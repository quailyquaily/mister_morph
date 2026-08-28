package mixin

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/agentpair"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	mixinbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/mixin"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/imagehistory"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/chatinfo"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/mixinapi"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/personautil"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
	"github.com/quailyquaily/mistermorph/internal/taskdomain"
	"github.com/quailyquaily/mistermorph/internal/textutil"
	"github.com/quailyquaily/mistermorph/internal/workspace"
)

const mixinTextMessageMaxBytes = 64 << 10

func runMixinLoop(ctx context.Context, d Dependencies, opts RunOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	logger, err := d.Logger()
	if err != nil {
		return err
	}
	deliveryReceipts := newMixinDeliveryReceipts()
	if logger == nil {
		logger = slog.Default()
	}
	allowedConversations, err := mixinAllowlist(opts.AllowedConversationIDs)
	if err != nil {
		return err
	}

	api, blaze, err := mixinProtocolClients(opts, logger)
	if err != nil {
		return err
	}
	bot, err := api.Me(ctx)
	if err != nil {
		return fmt.Errorf("load mixin bot profile: %w", err)
	}
	if strings.TrimSpace(bot.UserID) == "" {
		return fmt.Errorf("load mixin bot profile: user_id is required")
	}
	logger.Info("mixin_profile_loaded", "user_id", bot.UserID, "identity_number", bot.IdentityNumber, "name", bot.FullName)

	var untriggeredRecorder *runtimecore.UntriggeredRecorder
	if opts.RecordUntriggered {
		untriggeredRecorder, err = runtimecore.NewUntriggeredRecorder(d.RuntimePaths.JournalDir, d.TaskRotateMaxBytes)
		if err != nil {
			return fmt.Errorf("mixin untriggered journal: %w", err)
		}
		defer untriggeredRecorder.Close()
	}
	daemonStore := opts.TaskStore
	if daemonStore == nil {
		daemonStore, err = daemonruntime.NewTaskViewForTarget("mixin", opts.ServerMaxQueue, daemonruntime.TaskViewConfig{
			PersistenceTargets: d.TaskPersistenceTargets, TasksDir: d.RuntimePaths.TasksDir,
			JournalDir: d.RuntimePaths.JournalDir, RotateMaxBytes: d.TaskRotateMaxBytes,
		})
		if err != nil {
			return err
		}
	}
	bus, err := busruntime.StartInproc(busruntime.BootstrapOptions{MaxInFlight: opts.BusMaxInFlight, Logger: logger, Component: "mixin"})
	if err != nil {
		return err
	}
	busOwned := true
	defer func() {
		if busOwned {
			_ = bus.Close()
		}
	}()
	contactsStore := contacts.NewFileStore(d.RuntimePaths.ContactsDir)
	if err := contactsStore.Ensure(context.Background()); err != nil {
		return err
	}
	contactsService := contacts.NewService(contactsStore)
	chatInfoStore := chatinfo.NewStore(d.RuntimePaths.ContactsDir)
	var savedChatProfiles sync.Map
	adminValues := []string(nil)
	if d.AgentSettingsReader != nil {
		adminValues = d.AgentSettingsReader.GetStringSlice("admins")
	}
	admins, err := agentpair.ParseAdmins(adminValues)
	if err != nil {
		return err
	}
	pairManager, err := agentpair.New(agentpair.Options{
		Context: ctx, Self: mixinInboundAgentPeer(bot, ""), Admins: admins, Contacts: contactsService,
		JournalDir: d.RuntimePaths.JournalDir, JournalRotateMaxBytes: d.TaskRotateMaxBytes, Logger: logger,
		Send: func(sendCtx context.Context, target agentpair.Peer, body string) error {
			userID, targetErr := mixinPairSendUserID(target)
			if targetErr != nil {
				return targetErr
			}
			conversation, targetErr := api.CreateContactConversation(sendCtx, userID)
			if targetErr != nil {
				return targetErr
			}
			return sendMixinDirectText(sendCtx, api, conversation.ConversationID, userID, body, "")
		},
	})
	if err != nil {
		return err
	}
	workspaceStore := workspace.NewStore(d.RuntimePaths.WorkspaceAttachmentsPath)
	inboundAdapter, err := mixinbus.NewInboundAdapter(mixinbus.InboundAdapterOptions{Bus: bus, Store: contactsStore})
	if err != nil {
		return err
	}
	recentOutbound := newRecentMessageTracker(256)
	deliveryAdapter, err := mixinbus.NewDeliveryAdapter(mixinbus.DeliveryAdapterOptions{
		SendText: func(sendCtx context.Context, target mixinbus.DeliveryTarget, text string, sendOpts mixinbus.SendTextOptions) error {
			return sendMixinText(sendCtx, api, recentOutbound, target.ConversationID, target.RecipientID, text, sendOpts)
		},
	})
	if err != nil {
		return err
	}

	generations, err := runtimecore.BootstrapRuntimeGenerationManager(ctx, d.CommonDependencies, runtimecore.ChannelBootstrapOptions{
		Mode: "mixin", InspectRequest: opts.InspectRequest, InspectPrompt: opts.InspectPrompt,
		AgentConfig: opts.AgentLimits.ToConfig(), EngineToolsConfig: &opts.EngineToolsConfig, Logger: logger,
	})
	if err != nil {
		return err
	}
	defer generations.Close()
	runControl := runtimecontrol.New()
	workersCtx, stopWorkers := context.WithCancel(context.WithoutCancel(ctx))
	sem := make(chan struct{}, opts.MaxConcurrency)

	var daemonServer *http.Server
	var stopDaemon context.CancelFunc

	var stateMu sync.Mutex
	history := make(map[string][]chathistory.ChatHistoryItem)
	stickySkills := make(map[string][]string)
	approvals := newMixinApprovalManager(bus, daemonStore, generations, workersCtx, logger)
	var runner *runtimecore.ConversationRunner[string, mixinJob]
	runner = runtimecore.NewConversationRunner(workersCtx, sem, 16, func(workerCtx context.Context, conversationKey string, job mixinJob) {
		retainGeneration := false
		defer func() {
			if !retainGeneration {
				job.releaseGeneration()
			}
		}()
		bundle := job.runtimeBundle()
		if bundle == nil || bundle.TaskRuntime == nil {
			markMixinTaskFailed(logger, daemonStore, job.TaskID, "mixin runtime generation is unavailable", false)
			return
		}
		stateMu.Lock()
		prior := append([]chathistory.ChatHistoryItem(nil), history[conversationKey]...)
		skills := append([]string(nil), stickySkills[conversationKey]...)
		stateMu.Unlock()
		currentVersion := runner.CurrentVersion(conversationKey)
		if job.Version != currentVersion {
			prior = nil
		}
		if err := runtimecore.MarkTaskRunning(daemonStore, job.TaskID); err != nil {
			logger.Error("mixin_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskRunning, "error", err.Error())
			return
		}
		lease, err := runControl.StartLease(workerCtx, opts.TaskTimeout, runtimecontrol.ActiveRun{
			Runtime: "mixin", ConversationKey: conversationKey, TopicID: job.ConversationID, TaskID: job.TaskID, RunID: job.TaskID,
		})
		if err != nil {
			markMixinTaskFailed(logger, daemonStore, job.TaskID, err.Error(), false)
			return
		}
		runCtx := taskruntime.WithContextCompactionNotification(lease.Context, logger, func(notifyCtx context.Context, event agent.Event, text string) error {
			_, notifyErr := publishMixinBusOutbound(notifyCtx, bus, job.ConversationID, job.FromUserID, text, job.MessageID, fmt.Sprintf("mixin:context-compaction:%s:%d", job.TaskID, event.Step))
			return notifyErr
		})
		final, _, loadedSkills, runErr := runMixinTask(runCtx, bundle.TaskRuntime, api, job, prior, skills, lease.SteerQueue)
		userStopped := lease.UserStopped()
		lease.Finish()
		if runErr != nil {
			displayErr := depsutil.FormatRuntimeError(runErr)
			if userStopped {
				displayErr = "stopped by user"
			}
			markMixinTaskFailed(logger, daemonStore, job.TaskID, displayErr, userStopped)
			logger.Warn("mixin_task_error", "conversation_id", job.ConversationID, "message_id", job.MessageID, "error", displayErr)
			if !userStopped && workerCtx.Err() == nil {
				_, _ = publishMixinBusOutbound(workerCtx, bus, job.ConversationID, job.FromUserID, "error: "+displayErr, job.MessageID, "mixin:error:"+job.TaskID)
			}
			return
		}
		if pendingID, ok := runtimecore.PendingApprovalID(final); ok {
			pendingAt := time.Now().UTC()
			if err := daemonStore.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
				info.Status = daemonruntime.TaskPending
				info.PendingAt = &pendingAt
				info.ApprovalRequestID = pendingID
				info.Result = map[string]any{"source": "mixin", "final": final}
			}); err != nil {
				logger.Error("mixin_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskPending, "error", err.Error())
				return
			}
			if err := approvals.register(pendingID, job); err != nil {
				applied, stateErr := runtimecore.FailPendingApprovalTask(daemonStore, job.TaskID, pendingID, runtimecore.ApprovalRegistrationFailedTaskError)
				if stateErr != nil {
					err = errors.Join(err, stateErr)
				}
				logger.Error("mixin_approval_register_error", "approval_request_id", pendingID, "task_id", job.TaskID, "task_failed", applied, "error", err.Error())
				return
			}
			if err := approvals.notify(context.Background(), pendingID, job); err != nil {
				logger.Warn("mixin_approval_notify_error", "approval_request_id", pendingID, "conversation_id", job.ConversationID, "error", err.Error())
			}
			retainGeneration = true
			return
		}
		output := strings.TrimSpace(outputfmt.FormatFinalOutput(final))
		if output != "" {
			if err := workerCtx.Err(); err != nil {
				markMixinTaskFailed(logger, daemonStore, job.TaskID, err.Error(), true)
				return
			}
			if _, err := publishMixinBusOutboundAndWait(workerCtx, bus, deliveryReceipts, job.ConversationID, job.FromUserID, output, job.MessageID, "mixin:message:"+job.TaskID); err != nil {
				markMixinTaskFailed(logger, daemonStore, job.TaskID, "send mixin response: "+err.Error(), false)
				return
			}
		}
		if err := runtimecore.MarkTaskDone(daemonStore, job.TaskID, output); err != nil {
			logger.Error("mixin_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskDone, "error", err.Error())
			return
		}
		stateMu.Lock()
		latestVersion := runner.CurrentVersion(conversationKey)
		compactOnly := chatcommands.IsContextCompactCommand(job.Text)
		if latestVersion != currentVersion {
			history[conversationKey] = nil
			stickySkills[conversationKey] = nil
		} else if !compactOnly {
			if len(loadedSkills) > 0 {
				stickySkills[conversationKey] = capMixinSkills(loadedSkills)
			}
			items := append(history[conversationKey], newMixinInboundHistoryItem(job))
			if output != "" {
				items = append(items, newMixinOutboundHistoryItem(job, output, time.Now().UTC()))
			}
			history[conversationKey] = trimMixinHistory(items, mixinHistoryCap(opts.GroupTriggerMode))
		}
		stateMu.Unlock()
	}, runtimecore.ConversationRunnerOptions[string, mixinJob]{
		Logger: logger,
		OnDrop: func(_ string, job mixinJob) {
			markMixinTaskCanceled(logger, daemonStore, job.TaskID)
			job.releaseGeneration()
		},
		OnPanic: func(conversationKey string, job mixinJob) {
			defer job.releaseGeneration()
			runControl.Finish("mixin", conversationKey, job.TaskID)
			markMixinTaskFailed(logger, daemonStore, job.TaskID, "conversation worker panicked", false)
		},
	})
	approvals.runner = runner
	if opts.ServerListen != "" {
		daemonCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		stopDaemon = cancel
		daemonServer, err = daemonruntime.StartServer(daemonCtx, logger, daemonruntime.ServerOptions{
			Listen: opts.ServerListen,
			Routes: daemonruntime.RoutesOptions{
				Mode: "mixin", RuntimePaths: d.RuntimePaths, AuthToken: opts.ServerAuthToken,
				AgentNameFunc: func() string {
					if name := personautil.LoadAgentName(d.RuntimePaths.StateDir); name != "" {
						return name
					}
					return strings.TrimSpace(bot.FullName)
				},
				AgentAvatarURL: strings.TrimSpace(bot.AvatarURL),
				TaskTopic:      daemonruntime.TaskTopicRoutes{TaskReader: daemonStore},
				Approvals: daemonruntime.ApprovalRoutes{
					List: approvals.listApprovals, Get: approvals.getApproval,
					Approve: approvals.approve, Deny: approvals.deny,
				},
				Overview: func(context.Context) (map[string]any, error) {
					return map[string]any{"channel": map[string]any{
						"configured": true, "running": "mixin", "mixin_configured": true, "mixin_running": true,
					}}, nil
				},
				AgentSettingsEnabled: true, AgentSettingsOwner: d.AgentSettingsOwner, AgentSettingsReader: d.AgentSettingsReader, HealthEnabled: true,
			},
		})
		if err != nil {
			cancel()
			stopDaemon = nil
			daemonServer = nil
			logger.Warn("mixin_daemon_server_start_error", "addr", opts.ServerListen, "error", err.Error())
		}
	}
	defer func() {
		if daemonServer != nil {
			_ = daemonServer.Shutdown(context.Background())
		}
		if stopDaemon != nil {
			stopDaemon()
		}
		stopWorkers()
		_ = bus.Close()
		runner.WaitClosed()
		approvals.close()
	}()
	busOwned = false
	generations.Start(ctx)

	ingress := newMixinIngress(api, bot, opts.FileCacheDir, logger)
	ingress.authorize = func(messageCtx context.Context, inbound mixinbus.InboundMessage) (bool, error) {
		isGroup := isMixinGroup(inbound.ChatType)
		if mixinBypassesAllowlist(inbound.Text) {
			return true, nil
		}
		pairedAgent := false
		if inbound.FromIsAgent && !isGroup {
			peer := mixinInboundAgentPeer(mixinapi.User{
				UserID: inbound.FromUserID, IdentityNumber: inbound.IdentityNumber, FullName: inbound.DisplayName, AppID: "agent",
			}, inbound.ConversationID)
			var pairLookupErr error
			pairedAgent, pairLookupErr = pairManager.IsPaired(messageCtx, peer)
			if pairLookupErr != nil {
				logger.Warn("mixin_agent_pair_lookup_failed", "conversation_id", inbound.ConversationID, "message_id", inbound.MessageID, "peer_agent_id", peer.ID, "error", pairLookupErr.Error())
			}
		}
		authorized := mixinConversationAuthorized(allowedConversations, inbound.ConversationID, isGroup, inbound.FromIsAgent, pairedAgent)
		if !authorized {
			logger.Debug("mixin_unauthorized_conversation", "conversation_id", inbound.ConversationID, "message_id", inbound.MessageID, "from_user_id", inbound.FromUserID, "from_is_agent", inbound.FromIsAgent)
		}
		return authorized, nil
	}
	ingress.onConversationInvalidated = func(conversationID string) {
		savedChatProfiles.Delete(conversationID)
	}
	enqueueInbound := func(handlerCtx context.Context, message busruntime.BusMessage) error {
		inbound, err := mixinbus.InboundMessageFromBusMessage(message)
		if err != nil {
			return err
		}
		text := strings.TrimSpace(inbound.Text)
		conversationKey := message.ConversationKey
		stateMu.Lock()
		currentSkills := append([]string(nil), stickySkills[conversationKey]...)
		prior := append([]chathistory.ChatHistoryItem(nil), history[conversationKey]...)
		stateMu.Unlock()
		explicitReason, explicit := mixinExplicitTriggerReason(inbound, bot.UserID, recentOutbound)
		if isMixinGroup(inbound.ChatType) {
			if !mixinGroupCommandAllowed(text, explicit) {
				recordMixinUntriggered(logger, untriggeredRecorder, message, inbound)
				return nil
			}
			if !chatcommands.IsContextCompactCommand(text) {
				decisionCtx := llmstats.WithMetadata(context.Background(), mixinTaskID(inbound.ConversationID, inbound.MessageID), inbound.MessageID)
				generation, captureErr := generations.Capture()
				if captureErr != nil {
					return captureErr
				}
				bundle := generation.Bundle()
				if bundle == nil || bundle.TaskRuntime == nil {
					generation.Release()
					return fmt.Errorf("mixin runtime generation is unavailable")
				}
				addressingTimeout := bundle.AddressingRoute.ClientConfig.RequestTimeout
				if addressingTimeout <= 0 {
					addressingTimeout = opts.RequestTimeout
				}
				decision, accepted, decisionErr := decideMixinGroupTrigger(decisionCtx, bundle.AddressingClient, bundle.AddressingModel, inbound, opts.GroupTriggerMode, addressingTimeout, opts.AddressingConfidenceThreshold, opts.AddressingInterjectThreshold, prior, bot.UserID, recentOutbound, d.RuntimePaths.PersonaDir)
				generation.Release()
				if decisionErr != nil {
					logger.Warn("mixin_addressing_llm_error", "conversation_id", inbound.ConversationID, "error", decisionErr.Error())
					return nil
				}
				if !accepted {
					logger.Info("mixin_group_ignored", "conversation_id", inbound.ConversationID, "reason", decision.Reason, "llm_attempted", decision.AddressingLLMAttempted)
					if strings.EqualFold(opts.GroupTriggerMode, "talkative") {
						stateMu.Lock()
						history[conversationKey] = trimMixinHistory(append(history[conversationKey], newMixinInboundHistoryItem(mixinJobFromInbound(inbound))), mixinHistoryCap(opts.GroupTriggerMode))
						stateMu.Unlock()
					}
					recordMixinUntriggered(logger, untriggeredRecorder, message, inbound)
					return nil
				}
				logger.Info("mixin_group_trigger", "conversation_id", inbound.ConversationID, "reason", firstNonEmptyMixin(decision.Reason, explicitReason))
			}
		}
		if isMixinStopCommand(text) {
			result := runControl.Stop("mixin", conversationKey, "/stop")
			_, err := publishMixinBusOutbound(handlerCtx, bus, inbound.ConversationID, inbound.FromUserID, runtimecontrol.StopFeedback(result.Found), inbound.MessageID, "mixin:stop:"+inbound.MessageID)
			return err
		}
		if approvalID, approved, ok := parseMixinApprovalCommand(text); ok {
			_, _, decisionErr := approvals.apply(handlerCtx, approvalID, approved, "mixin:"+inbound.FromUserID, func(job mixinJob) bool {
				return job.ConversationID == inbound.ConversationID && job.FromUserID == inbound.FromUserID
			})
			response := mixinApprovalResultText(approved)
			if decisionErr != nil {
				response = "Approval failed: " + strings.TrimSpace(decisionErr.Error())
				logger.Warn("mixin_approval_decision_error", "approval_request_id", approvalID, "conversation_id", inbound.ConversationID, "user_id", inbound.FromUserID, "error", decisionErr.Error())
			}
			_, publishErr := publishMixinBusOutbound(handlerCtx, bus, inbound.ConversationID, inbound.FromUserID, response, inbound.MessageID, "mixin:approval-result:"+approvalID)
			return publishErr
		}
		if handled, commandErr := maybeHandleMixinCommand(handlerCtx, d, bus, workspaceStore, conversationKey, inbound, currentSkills, func(resetCtx context.Context) error {
			runControl.Stop("mixin", conversationKey, "/reset")
			generation, captureErr := generations.Capture()
			if captureErr != nil {
				return captureErr
			}
			bundle := generation.Bundle()
			if bundle == nil || bundle.TaskRuntime == nil {
				generation.Release()
				return fmt.Errorf("mixin runtime generation is unavailable")
			}
			resetErr := bundle.TaskRuntime.ResetContextHistory(resetCtx, conversationKey)
			generation.Release()
			if resetErr != nil {
				return resetErr
			}
			stateMu.Lock()
			delete(history, conversationKey)
			delete(stickySkills, conversationKey)
			runner.IncrementVersion(conversationKey)
			stateMu.Unlock()
			return nil
		}); handled {
			return commandErr
		}
		if !chatcommands.IsContextCompactCommand(text) {
			if result := runControl.Steer("mixin", conversationKey, text); result.Found {
				_, err := publishMixinBusOutbound(handlerCtx, bus, inbound.ConversationID, inbound.FromUserID, runtimecontrol.SteerFeedback(result.Found, result.Queued), inbound.MessageID, "mixin:steer:"+inbound.MessageID)
				return err
			}
		}
		resolution, err := workspace.Resolve(workspaceStore, conversationKey, d.DefaultWorkspaceDir)
		if err != nil {
			return err
		}
		imagePaths := busruntime.ImagePathsFromAttachments(inbound.ImageAttachments)
		images := imagehistory.BuildFromAttachments(inbound.ImageAttachments, pathroots.New(resolution.WorkspaceDir, opts.FileCacheDir, d.RuntimePaths.StateDir))
		taskID := mixinTaskID(inbound.ConversationID, inbound.MessageID)
		generation, err := generations.Capture()
		if err != nil {
			return err
		}
		bundle := generation.Bundle()
		if bundle == nil || bundle.TaskRuntime == nil {
			generation.Release()
			return fmt.Errorf("mixin runtime generation is unavailable")
		}
		route, err := bundle.TaskRuntime.ResolveTaskRouteForRun(llmstats.WithRunID(handlerCtx, taskID), text)
		if err != nil {
			generation.Release()
			return err
		}
		buildJob := func(version uint64) mixinJob {
			admittedRoute := route
			return mixinJob{
				TaskID: taskID, ConversationKey: conversationKey, ConversationID: inbound.ConversationID,
				ChatType: inbound.ChatType, MessageID: inbound.MessageID, QuoteMessageID: inbound.QuoteMessageID,
				FromUserID: inbound.FromUserID, IdentityNumber: inbound.IdentityNumber, DisplayName: inbound.DisplayName, FromIsAgent: inbound.FromIsAgent,
				Text: text, ImagePaths: imagePaths, Images: append([]chathistory.ChatHistoryImage(nil), images...),
				WorkspaceDir: resolution.WorkspaceDir, FileCacheDir: opts.FileCacheDir, Route: &admittedRoute, SentAt: inbound.SentAt,
				Version: version, MentionUsers: append([]string(nil), inbound.MentionUserIDs...), EventID: inbound.MessageID, Generation: generation,
			}
		}
		createdAt := inbound.SentAt.UTC()
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		if err := taskdomain.RecordTaskUpsert(daemonStore, daemonruntime.TaskInfo{
			ID: taskID, Status: daemonruntime.TaskQueued, Task: textutil.TruncateRunes(text, 2000), Model: route.ClientConfig.Model,
			Timeout: opts.TaskTimeout.String(), CreatedAt: createdAt, Conversation: mixinTaskConversation(buildJob(0)),
			Result: map[string]any{"source": "mixin", "mixin_conversation_id": inbound.ConversationID, "mixin_message_id": inbound.MessageID, "mixin_chat_type": inbound.ChatType, "mixin_from_user_id": inbound.FromUserID},
		}, daemonruntime.TaskTrigger{Source: "mixin", Event: "blaze_inbound", Ref: inbound.MessageID}); err != nil {
			generation.Release()
			return err
		}
		if err := runner.Enqueue(handlerCtx, conversationKey, buildJob); err != nil {
			generation.Release()
			markMixinTaskFailed(logger, daemonStore, taskID, err.Error(), taskdomain.EndedByCancellation(handlerCtx, err))
			return err
		}
		logger.Info("mixin_task_enqueued", "conversation_id", inbound.ConversationID, "message_id", inbound.MessageID, "conversation_key", conversationKey, "text_len", len(text))
		return nil
	}

	busHandler := func(handlerCtx context.Context, message busruntime.BusMessage) error {
		switch message.Direction {
		case busruntime.DirectionInbound:
			if message.Channel != busruntime.ChannelMixin {
				return fmt.Errorf("unsupported inbound channel: %s", message.Channel)
			}
			if err := contactsService.ObserveInboundBusMessage(context.Background(), message, time.Now().UTC()); err != nil {
				logger.Warn("contacts_observe_bus_error", "channel", message.Channel, "error", err.Error())
			}
			return enqueueInbound(handlerCtx, message)
		case busruntime.DirectionOutbound:
			if message.Channel != busruntime.ChannelMixin {
				return fmt.Errorf("unsupported outbound channel: %s", message.Channel)
			}
			_, _, err := deliveryAdapter.Deliver(handlerCtx, message)
			deliveryReceipts.complete(message.ID, err)
			return err
		default:
			return fmt.Errorf("unsupported direction: %s", message.Direction)
		}
	}
	for _, topic := range busruntime.AllTopics() {
		if err := bus.Subscribe(topic, busHandler); err != nil {
			return err
		}
	}

	logger.Info("mixin_runtime_start", "allowed_conversation_ids", len(opts.AllowedConversationIDs), "task_timeout", opts.TaskTimeout.String(), "max_concurrency", opts.MaxConcurrency, "group_trigger_mode", opts.GroupTriggerMode)
	err = blaze.Run(ctx, func(messageCtx context.Context, view mixinapi.MessageView) error {
		inbound, publish, normalizeErr := ingress.Normalize(messageCtx, view)
		if normalizeErr != nil {
			logger.Warn("mixin_message_unsupported", "conversation_id", view.ConversationID, "message_id", view.MessageID, "category", view.Category, "error", normalizeErr.Error())
			if errors.Is(normalizeErr, errMixinAttachmentDownload) {
				return normalizeErr
			}
			return nil
		}
		if !publish {
			return nil
		}
		isGroup := isMixinGroup(inbound.ChatType)
		if agentpair.IsControlMessage(inbound.Text) {
			if isGroup || !inbound.FromIsAgent {
				logger.Warn("agent_pair_failed", "channel", "mixin", "conversation_id", inbound.ConversationID, "message_id", inbound.MessageID, "reason", "invalid_control_sender_or_scope")
				return nil
			}
			peer := mixinInboundAgentPeer(mixinapi.User{
				UserID: inbound.FromUserID, IdentityNumber: inbound.IdentityNumber, FullName: inbound.DisplayName, AppID: "agent",
			}, inbound.ConversationID)
			_, handled, pairErr := pairManager.Handle(messageCtx, peer, inbound.Text)
			if pairErr != nil {
				logger.Warn("agent_pair_failed", "channel", "mixin", "conversation_id", inbound.ConversationID, "message_id", inbound.MessageID, "peer_agent_id", peer.ID, "reason", "offer_rejected", "error", pairErr.Error())
			}
			if handled {
				return nil
			}
		}
		command, args := chatcommands.ParseCommand(inbound.Text)
		if chatcommands.NormalizeCommand(command) == "/pair" {
			if isGroup || inbound.FromIsAgent {
				logger.Warn("agent_pair_failed", "channel", "mixin", "conversation_id", inbound.ConversationID, "message_id", inbound.MessageID, "reason", "pair_command_requires_private_human_sender")
				return nil
			}
			target, pairErr := mixinPairTarget(messageCtx, contactsService, args)
			var status agentpair.Status
			if pairErr == nil {
				adminReference := ""
				if inbound.IdentityNumber != "" {
					adminReference = "mixin:@" + inbound.IdentityNumber
				}
				status, pairErr = pairManager.Start(messageCtx, "mixin:"+inbound.FromUserID, target, adminReference)
			}
			if sendErr := sendMixinDirectText(messageCtx, api, inbound.ConversationID, inbound.FromUserID, mixinPairReplyText(status, pairErr), inbound.MessageID); sendErr != nil {
				return sendErr
			}
			return nil
		}
		if _, loaded := savedChatProfiles.LoadOrStore(inbound.ConversationID, true); !loaded {
			profileType := strings.ToLower(strings.TrimSpace(inbound.ChatType))
			if profileErr := chatInfoStore.Put(messageCtx, chatinfo.Info{
				ChatID: "mixin:" + inbound.ConversationID, Platform: "mixin", Type: profileType,
				Name: inbound.ConversationName, FetchedAt: time.Now().UTC(),
			}); profileErr != nil {
				savedChatProfiles.Delete(inbound.ConversationID)
				logger.Warn("mixin_chat_profile_write_failed", "conversation_id", inbound.ConversationID, "error", profileErr.Error())
			}
		}
		accepted, publishErr := inboundAdapter.HandleInboundMessage(messageCtx, inbound)
		if publishErr != nil {
			logger.Warn("mixin_bus_publish_error", "conversation_id", inbound.ConversationID, "message_id", inbound.MessageID, "error", publishErr.Error())
			return publishErr
		}
		if accepted {
			logger.Debug("mixin_message_received", "conversation_id", inbound.ConversationID, "message_id", inbound.MessageID, "chat_type", inbound.ChatType, "text_len", len(inbound.Text))
		} else {
			logger.Debug("mixin_message_deduped", "conversation_id", inbound.ConversationID, "message_id", inbound.MessageID)
		}
		return nil
	})
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) && ctx.Err() != nil {
		logger.Info("mixin_runtime_stop", "reason", "context_canceled")
		return nil
	}
	if err != nil {
		return fmt.Errorf("mixin blaze: %w", err)
	}
	return nil
}

func sendMixinDirectText(ctx context.Context, api mixinAPI, conversationID, recipientID, text, quoteMessageID string) error {
	conversationID = strings.TrimSpace(conversationID)
	text = strings.TrimSpace(text)
	if conversationID == "" || text == "" {
		return fmt.Errorf("mixin conversation and text are required")
	}
	return api.SendMessages(ctx, []mixinapi.MessageRequest{{
		ConversationID: conversationID, RecipientID: strings.TrimSpace(recipientID), MessageID: uuid.NewString(), Category: mixinapi.MessageCategoryPlainText,
		DataBase64: base64.RawURLEncoding.EncodeToString([]byte(text)), QuoteMessageID: strings.TrimSpace(quoteMessageID),
	}})
}

func mixinProtocolClients(opts RunOptions, logger *slog.Logger) (mixinAPI, mixinBlaze, error) {
	api := opts.api
	blaze := opts.blaze
	if api != nil && blaze != nil {
		return api, blaze, nil
	}
	credentials := opts.Credentials
	var err error
	if opts.KeystoreFile != "" {
		credentials, err = mixinapi.LoadKeystore(opts.KeystoreFile)
		if err != nil {
			return nil, nil, err
		}
	}
	if api == nil {
		api, err = mixinapi.NewClient(credentials, mixinapi.ClientOptions{})
		if err != nil {
			return nil, nil, err
		}
	}
	if blaze == nil {
		blaze, err = mixinapi.NewBlazeClient(credentials, mixinapi.BlazeOptions{
			OnConnectionChange: func(connected bool) {
				if connected {
					logger.Info("mixin_blaze_connected")
				} else {
					logger.Warn("mixin_blaze_disconnected")
				}
			},
			OnReconnect: func(reconnectErr error, delay time.Duration) {
				logger.Warn("mixin_blaze_reconnect_scheduled", "delay", delay.String(), "error", reconnectErr)
			},
		})
		if err != nil {
			return nil, nil, err
		}
	}
	return api, blaze, nil
}

func sendMixinText(ctx context.Context, api mixinAPI, recent *recentMessageTracker, conversationID, recipientID, text string, opts mixinbus.SendTextOptions) error {
	baseID, err := uuid.Parse(strings.TrimSpace(opts.MessageID))
	if err != nil || baseID == uuid.Nil {
		return fmt.Errorf("mixin outbound message_id is invalid")
	}
	parts := splitMixinText(text, mixinTextMessageMaxBytes)
	for index, part := range parts {
		messageID := baseID
		if index > 0 {
			messageID = uuid.NewSHA1(baseID, []byte(strconv.Itoa(index)))
		}
		quote := ""
		if index == 0 {
			quote = strings.TrimSpace(opts.QuoteMessageID)
		}
		request := mixinapi.MessageRequest{
			ConversationID: conversationID, RecipientID: strings.TrimSpace(recipientID), MessageID: messageID.String(), Category: mixinapi.MessageCategoryPlainText,
			DataBase64: base64.RawURLEncoding.EncodeToString([]byte(part)), QuoteMessageID: quote,
		}
		if err := api.SendMessages(ctx, []mixinapi.MessageRequest{request}); err != nil {
			return err
		}
		recent.Add(conversationID, messageID.String())
	}
	return nil
}

func mixinAllowlist(items []string) (map[string]bool, error) {
	allowed := make(map[string]bool, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		id, err := uuid.Parse(item)
		if err != nil || id == uuid.Nil {
			return nil, fmt.Errorf("invalid mixin allowed conversation id %q", item)
		}
		allowed[id.String()] = true
	}
	return allowed, nil
}

func mixinBypassesAllowlist(text string) bool {
	if agentpair.IsControlMessage(text) {
		return true
	}
	command, _ := chatcommands.ParseCommand(text)
	switch chatcommands.NormalizeCommand(command) {
	case "/id", "/pair":
		return true
	default:
		return false
	}
}

func mixinTaskID(conversationID, messageID string) string {
	return daemonruntime.BuildTaskID("mx", conversationID, messageID)
}

func recordMixinUntriggered(logger *slog.Logger, recorder *runtimecore.UntriggeredRecorder, message busruntime.BusMessage, inbound mixinbus.InboundMessage) {
	if recorder == nil {
		return
	}
	if err := recorder.Record(runtimecore.UntriggeredMessage{
		Channel: string(busruntime.ChannelMixin), ConversationKey: message.ConversationKey, MessageID: inbound.MessageID,
		SenderID: inbound.FromUserID, SentAt: inbound.SentAt, Text: inbound.Text,
	}); err != nil {
		logger.Error("mixin_untriggered_journal_append_error", "conversation_id", inbound.ConversationID, "message_id", inbound.MessageID, "error", err.Error())
	}
}

func markMixinTaskFailed(logger *slog.Logger, store daemonruntime.TaskUpdater, taskID, message string, stopped bool) {
	if err := runtimecore.MarkTaskFailed(store, taskID, strings.TrimSpace(message), stopped); err != nil {
		logger.Error("mixin_task_state_write_error", "task_id", taskID, "status", daemonruntime.TaskFailed, "error", err.Error())
	}
}

func markMixinTaskCanceled(logger *slog.Logger, store daemonruntime.TaskUpdater, taskID string) {
	if store == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	finishedAt := time.Now().UTC()
	if err := store.Update(taskID, func(info *daemonruntime.TaskInfo) {
		if info != nil && (info.Status == daemonruntime.TaskQueued || info.Status == daemonruntime.TaskRunning) {
			info.Status = daemonruntime.TaskCanceled
			info.Error = "mixin runtime closed"
			info.FinishedAt = &finishedAt
		}
	}); err != nil {
		logger.Error("mixin_task_state_write_error", "task_id", taskID, "status", daemonruntime.TaskCanceled, "error", err.Error())
	}
}

func firstNonEmptyMixin(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
