package lark

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/contacts"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	larkbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/lark"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/imagehistory"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/larkapi"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/personautil"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
	"github.com/quailyquaily/mistermorph/internal/taskdomain"
	"github.com/quailyquaily/mistermorph/internal/textutil"
	"github.com/quailyquaily/mistermorph/internal/workspace"
)

func runLarkLoop(ctx context.Context, d Dependencies, opts RunOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(opts.AppID) == "" {
		return fmt.Errorf("missing lark.app_id")
	}
	if strings.TrimSpace(opts.AppSecret) == "" {
		return fmt.Errorf("missing lark.app_secret")
	}

	logger, err := d.Logger()
	if err != nil {
		return err
	}
	var untriggeredRecorder *runtimecore.UntriggeredRecorder
	if opts.RecordUntriggered {
		untriggeredRecorder, err = runtimecore.NewUntriggeredRecorder(d.RuntimePaths.JournalDir, d.TaskRotateMaxBytes)
		if err != nil {
			return fmt.Errorf("lark untriggered journal: %w", err)
		}
		defer untriggeredRecorder.Close()
	}
	daemonStore := opts.TaskStore
	if daemonStore == nil {
		daemonStore, err = daemonruntime.NewTaskViewForTarget("lark", opts.ServerMaxQueue, daemonruntime.TaskViewConfig{
			PersistenceTargets: d.TaskPersistenceTargets,
			TasksDir:           d.RuntimePaths.TasksDir,
			JournalDir:         d.RuntimePaths.JournalDir,
			RotateMaxBytes:     d.TaskRotateMaxBytes,
		})
		if err != nil {
			return err
		}
	}
	inprocBus, err := busruntime.StartInproc(busruntime.BootstrapOptions{
		MaxInFlight: opts.BusMaxInFlight,
		Logger:      logger,
		Component:   "lark",
	})
	if err != nil {
		return err
	}
	busShutdownTransferred := false
	defer func() {
		if !busShutdownTransferred {
			_ = inprocBus.Close()
		}
	}()

	contactsStore := contacts.NewFileStore(d.RuntimePaths.ContactsDir)
	if err := contactsStore.Ensure(context.Background()); err != nil {
		return err
	}
	workspaceStore := workspace.NewStore(d.RuntimePaths.WorkspaceAttachmentsPath)
	contactsSvc := contacts.NewService(contactsStore)
	avatarRefresher, err := contacts.NewContactAvatarRefresher(ctx, contactsStore, logger.With("channel", "lark"))
	if err != nil {
		return err
	}
	defer avatarRefresher.Close()
	larkInboundAdapter, err := larkbus.NewInboundAdapter(larkbus.InboundAdapterOptions{
		Bus:   inprocBus,
		Store: contactsStore,
	})
	if err != nil {
		return err
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	tokenClient := larkapi.NewTenantTokenClient(httpClient, strings.TrimSpace(opts.BaseURL), opts.AppID, opts.AppSecret)
	api := newLarkAPI(httpClient, strings.TrimSpace(opts.BaseURL), tokenClient)
	if err := avatarRefresher.Prewarm(contacts.ChannelLark, func(contact contacts.Contact) contacts.ContactAvatarFetchFunc {
		openID := strings.TrimSpace(contact.LarkOpenID)
		if openID == "" {
			return nil
		}
		return func(ctx context.Context) ([]byte, bool, error) {
			avatarURL, err := api.userAvatarURL(ctx, openID)
			if err != nil {
				return nil, false, err
			}
			return contacts.FetchContactAvatarURL(ctx, api.http, avatarURL)
		}
	}); err != nil {
		logger.Warn("contact_avatar_prewarm_failed", "channel", "lark", "error", err.Error())
	}
	toolAPI := newLarkToolAPI(api)
	larkDeliveryAdapter, err := larkbus.NewDeliveryAdapter(larkbus.DeliveryAdapterOptions{
		SendText: func(ctx context.Context, target any, text string, opts larkbus.SendTextOptions) error {
			deliverTarget, ok := target.(larkbus.DeliveryTarget)
			if !ok {
				return fmt.Errorf("lark target is invalid")
			}
			return sendLarkText(ctx, api, logger, deliverTarget.ChatID, text, opts.ReplyToMessageID)
		},
	})
	if err != nil {
		return err
	}

	requestTimeout := opts.RequestTimeout
	runtimeGenerations, err := runtimecore.BootstrapRuntimeGenerationManager(ctx, d.CommonDependencies, runtimecore.ChannelBootstrapOptions{
		Mode:              "lark",
		InspectRequest:    opts.InspectRequest,
		InspectPrompt:     opts.InspectPrompt,
		AgentConfig:       opts.AgentLimits.ToConfig(),
		EngineToolsConfig: &opts.EngineToolsConfig,
		Logger:            logger,
	})
	if err != nil {
		return err
	}
	defer runtimeGenerations.Close()
	groupTriggerMode := strings.ToLower(strings.TrimSpace(opts.GroupTriggerMode))
	runControl := runtimecontrol.New()
	addressingConfidenceThreshold := opts.AddressingConfidenceThreshold
	addressingInterjectThreshold := opts.AddressingInterjectThreshold

	taskTimeout := opts.TaskTimeout
	maxConcurrency := opts.MaxConcurrency
	sem := make(chan struct{}, maxConcurrency)
	workersCtx, stopWorkers := newLarkOwnedContext(ctx)
	allowedChats := toAllowlist(opts.AllowedChatIDs)

	serverListen := strings.TrimSpace(opts.ServerListen)
	var daemonServer *http.Server
	var stopDaemonServer context.CancelFunc
	if serverListen != "" {
		if strings.TrimSpace(opts.ServerAuthToken) == "" {
			logger.Warn("lark_daemon_server_auth_empty", "hint", "set server.auth_token so console can read /runtime/tasks")
		}
		daemonServerCtx, cancelDaemonServer := newLarkOwnedContext(ctx)
		stopDaemonServer = cancelDaemonServer
		daemonServer, err = daemonruntime.StartServer(daemonServerCtx, logger, daemonruntime.ServerOptions{
			Listen: serverListen,
			Routes: daemonruntime.RoutesOptions{
				Mode:          "lark",
				AgentNameFunc: func() string { return personautil.LoadAgentName(d.RuntimePaths.StateDir) },
				RuntimePaths:  d.RuntimePaths,
				AuthToken:     strings.TrimSpace(opts.ServerAuthToken), TaskTopic: daemonruntime.TaskTopicRoutes{TaskReader: daemonStore}, Overview: func(ctx context.Context) (map[string]any, error) {
					generationLease, captureErr := runtimeGenerations.Capture()
					if captureErr != nil {
						return nil, captureErr
					}
					defer generationLease.Release()
					runtimeBundle := generationLease.Bundle()
					if runtimeBundle == nil || runtimeBundle.TaskRuntime == nil {
						return nil, fmt.Errorf("lark runtime generation is unavailable")
					}
					mainRoute := runtimeBundle.TaskRuntime.BootstrapMainRoute
					return map[string]any{
						"llm": map[string]any{
							"provider": strings.TrimSpace(mainRoute.ClientConfig.Provider),
							"model":    runtimeBundle.TaskRuntime.BootstrapMainModel,
						},
						"channel": map[string]any{
							"configured":       true,
							"telegram_running": false,
							"slack_running":    false,
							"line_running":     false,
							"lark_running":     true,
							"running":          "lark",
						},
					}, nil
				},
				AgentSettingsEnabled: true,
				AgentSettingsOwner:   d.AgentSettingsOwner,
				AgentSettingsReader:  d.AgentSettingsReader,
				HealthEnabled:        true,
			},
		})
		if err != nil {
			stopDaemonServer()
			stopDaemonServer = nil
			daemonServer = nil
			logger.Warn("lark_daemon_server_start_error", "addr", serverListen, "error", err.Error())
		}
	}

	var (
		mu                 sync.Mutex
		history            = make(map[string][]chathistory.ChatHistoryItem)
		stickySkillsByConv = make(map[string][]string)
		enqueueLarkInbound func(context.Context, busruntime.BusMessage) error
	)
	var runner *runtimecore.ConversationRunner[string, larkJob]
	runner = runtimecore.NewConversationRunner[string, larkJob](
		workersCtx,
		sem,
		16,
		func(workerCtx context.Context, conversationKey string, job larkJob) {
			defer job.releaseGeneration()
			runtimeBundle := job.runtimeBundle()
			if runtimeBundle == nil || runtimeBundle.TaskRuntime == nil {
				if stateErr := runtimecore.MarkTaskFailed(daemonStore, job.TaskID, "lark runtime generation is unavailable", false); stateErr != nil {
					logger.Error("lark_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskFailed, "error", stateErr.Error())
				}
				return
			}
			taskRuntimeOpts := runtimeTaskOptions{
				FileCacheDir:     opts.FileCacheDir,
				ToolAPI:          toolAPI,
				ToolFileMaxBytes: larkToolFileMaxBytes,
			}
			mu.Lock()
			h := append([]chathistory.ChatHistoryItem(nil), history[conversationKey]...)
			sticky := append([]string(nil), stickySkillsByConv[conversationKey]...)
			mu.Unlock()
			curVersion := runner.CurrentVersion(conversationKey)
			if job.Version != curVersion {
				h = nil
			}
			if err := runtimecore.MarkTaskRunning(daemonStore, job.TaskID); err != nil {
				logger.Error("lark_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskRunning, "error", err.Error())
				return
			}
			lease, err := runControl.StartLease(workerCtx, taskTimeout, runtimecontrol.ActiveRun{
				Runtime:         "lark",
				ConversationKey: conversationKey,
				TopicID:         job.ChatID,
				TaskID:          job.TaskID,
				RunID:           job.TaskID,
			})
			if err != nil {
				if cancelLarkTaskOnWorkerShutdown(workerCtx, logger, daemonStore, job) {
					return
				}
				if stateErr := runtimecore.MarkTaskFailed(daemonStore, job.TaskID, strings.TrimSpace(err.Error()), false); stateErr != nil {
					logger.Error("lark_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskFailed, "error", stateErr.Error())
				}
				return
			}
			runCtx := taskruntime.WithContextCompactionNotification(lease.Context, logger, func(notifyCtx context.Context, event agent.Event, text string) error {
				correlationID := fmt.Sprintf("lark:context-compaction:%s:%d", job.TaskID, event.Step)
				_, notifyErr := publishLarkBusOutbound(notifyCtx, inprocBus, job.ChatID, text, job.MessageID, correlationID)
				return notifyErr
			})
			final, _, loadedSkills, runErr := runLarkTask(
				runCtx,
				runtimeBundle.TaskRuntime,
				job,
				h,
				sticky,
				taskRuntimeOpts,
				lease.SteerQueue,
			)
			userStopped := lease.UserStopped()
			lease.Finish()
			if runErr != nil {
				if cancelLarkTaskOnWorkerShutdown(workerCtx, logger, daemonStore, job) {
					return
				}
				displayErr := depsutil.FormatRuntimeError(runErr)
				if userStopped {
					displayErr = "stopped by user"
				}
				if stateErr := runtimecore.MarkTaskFailed(daemonStore, job.TaskID, displayErr, userStopped); stateErr != nil {
					logger.Error("lark_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskFailed, "error", stateErr.Error())
				}
				logger.Warn("lark_task_error",
					"chat_id", job.ChatID,
					"message_id", job.MessageID,
					"error", displayErr,
				)
				if userStopped {
					return
				}
				errorText := "error: " + displayErr
				errorCorrelationID := fmt.Sprintf("lark:error:%s:%s", job.ChatID, job.MessageID)
				_, err := publishLarkBusOutbound(workerCtx, inprocBus, job.ChatID, errorText, job.MessageID, errorCorrelationID)
				if err != nil {
					logger.Warn("lark_bus_publish_error",
						"channel", busruntime.ChannelLark,
						"chat_id", job.ChatID,
						"bus_error_code", string(busruntime.ErrorCodeOf(err)),
						"error", err.Error(),
					)
				}
				return
			}
			outText := strings.TrimSpace(outputfmt.FormatFinalOutput(final))
			if err := runtimecore.MarkTaskDone(daemonStore, job.TaskID, outText); err != nil {
				logger.Error("lark_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskDone, "error", err.Error())
				return
			}
			if outText != "" {
				if workerCtx.Err() != nil {
					return
				}
				outCorrelationID := fmt.Sprintf("lark:message:%s:%s", job.ChatID, job.MessageID)
				_, err := publishLarkBusOutbound(workerCtx, inprocBus, job.ChatID, outText, job.MessageID, outCorrelationID)
				if err != nil {
					logger.Warn("lark_bus_publish_error",
						"channel", busruntime.ChannelLark,
						"chat_id", job.ChatID,
						"bus_error_code", string(busruntime.ErrorCodeOf(err)),
						"error", err.Error(),
					)
				}
			}
			mu.Lock()
			latestVersion := runner.CurrentVersion(conversationKey)
			contextCompactionOnly := chatcommands.IsContextCompactCommand(job.Text)
			if latestVersion != curVersion {
				history[conversationKey] = nil
				stickySkillsByConv[conversationKey] = nil
			}
			if !contextCompactionOnly && latestVersion == curVersion && len(loadedSkills) > 0 {
				stickySkillsByConv[conversationKey] = capUniqueStrings(loadedSkills, larkStickySkillsCap)
			}
			if !contextCompactionOnly {
				cur := history[conversationKey]
				inboundHistory := newLarkInboundHistoryItem(job)
				if outText != "" {
					inboundHistory.Images = imagehistory.WithDescription(inboundHistory.Images, outText, "agent_final")
				}
				cur = append(cur, inboundHistory)
				if outText != "" {
					cur = append(cur, newLarkOutboundAgentHistoryItem(job, outText, time.Now().UTC()))
				}
				history[conversationKey] = trimChatHistoryItems(cur, larkHistoryCapForMode(groupTriggerMode))
			}
			mu.Unlock()
		},
		larkConversationRunnerOptions(logger, daemonStore, runControl),
	)
	// Stop daemon admission, drain bus handlers, then join tasks before shared cleanup.
	defer shutdownLarkRuntime(daemonServer, stopDaemonServer, inprocBus, stopWorkers, runner)
	busShutdownTransferred = true
	runtimeGenerations.Start(ctx)

	enqueueLarkInbound = func(ctx context.Context, msg busruntime.BusMessage) error {
		if ctx == nil {
			ctx = workersCtx
		}
		inbound, err := larkbus.InboundMessageFromBusMessage(msg)
		if err != nil {
			return err
		}
		text := strings.TrimSpace(inbound.Text)
		if text == "" {
			return fmt.Errorf("lark inbound text is required")
		}
		contextCompactionOnly := chatcommands.IsContextCompactCommand(text)
		mu.Lock()
		currentSkills := append([]string(nil), stickySkillsByConv[msg.ConversationKey]...)
		mu.Unlock()
		if isLarkStopCommand(inbound.Text) {
			result := runControl.Stop("lark", msg.ConversationKey, "/stop")
			correlationID := fmt.Sprintf("lark:stop:%s:%s", inbound.ChatID, inbound.MessageID)
			_, publishErr := publishLarkBusOutbound(ctx, inprocBus, inbound.ChatID, runtimecontrol.StopFeedback(result.Found), inbound.MessageID, correlationID)
			return publishErr
		}
		if handledCommand, cmdErr := maybeHandleLarkCommand(ctx, d, inprocBus, workspaceStore, msg.ConversationKey, inbound, currentSkills); handledCommand {
			return cmdErr
		}
		if !contextCompactionOnly && strings.EqualFold(strings.TrimSpace(inbound.ChatType), "group") {
			mu.Lock()
			historySnapshot := append([]chathistory.ChatHistoryItem(nil), history[msg.ConversationKey]...)
			mu.Unlock()
			decisionCtx := llmstats.WithMetadata(context.Background(), larkTaskID(inbound.ChatID, inbound.MessageID), inbound.EventID)
			addressingLease, captureErr := runtimeGenerations.Capture()
			if captureErr != nil {
				logger.Warn("lark_runtime_generation_unavailable", "error", captureErr.Error())
				return nil
			}
			addressingBundle := addressingLease.Bundle()
			if addressingBundle == nil || addressingBundle.TaskRuntime == nil {
				addressingLease.Release()
				logger.Warn("lark_runtime_generation_unavailable", "error", "runtime bundle is unavailable")
				return nil
			}
			addressingLLMTimeout := addressingBundle.AddressingRoute.ClientConfig.RequestTimeout
			if addressingLLMTimeout <= 0 {
				addressingLLMTimeout = requestTimeout
			}
			dec, accepted, decErr := decideLarkGroupTrigger(
				decisionCtx,
				addressingBundle.AddressingClient,
				addressingBundle.AddressingModel,
				inbound,
				groupTriggerMode,
				addressingLLMTimeout,
				addressingConfidenceThreshold,
				addressingInterjectThreshold,
				historySnapshot,
				d.RuntimePaths.PersonaDir,
			)
			addressingLease.Release()
			if decErr != nil {
				logger.Warn("lark_addressing_llm_error", "chat_id", inbound.ChatID, "error", decErr.Error())
				return nil
			}
			if !accepted {
				logger.Info("lark_group_ignored",
					"chat_id", inbound.ChatID,
					"text_len", len(text),
					"llm_attempted", dec.AddressingLLMAttempted,
					"llm_ok", dec.AddressingLLMOK,
					"llm_addressed", dec.Addressing.Addressed,
					"confidence", dec.Addressing.Confidence,
					"wanna_interject", dec.Addressing.WannaInterject,
					"interject", dec.Addressing.Interject,
					"impulse", dec.Addressing.Impulse,
					"is_lightweight", dec.Addressing.IsLightweight,
					"reason", dec.Reason,
				)
				if strings.EqualFold(groupTriggerMode, "talkative") {
					mu.Lock()
					cur := history[msg.ConversationKey]
					cur = append(cur, newLarkInboundHistoryItem(larkJobFromInbound(inbound)))
					history[msg.ConversationKey] = trimChatHistoryItems(cur, larkHistoryCapForMode(groupTriggerMode))
					mu.Unlock()
				}
				if untriggeredRecorder != nil {
					hasAttachment := len(inbound.ImageKeys) > 0 || len(inbound.ImagePaths) > 0 || len(inbound.ImageAttachments) > 0
					untriggeredText := inbound.Text
					if hasAttachment && strings.TrimSpace(untriggeredText) == "User sent an image." {
						untriggeredText = ""
					}
					if recordErr := untriggeredRecorder.Record(runtimecore.UntriggeredMessage{
						Channel:         string(busruntime.ChannelLark),
						ConversationKey: msg.ConversationKey,
						MessageID:       inbound.MessageID,
						SenderID:        inbound.FromUserID,
						SentAt:          inbound.SentAt,
						Text:            untriggeredText,
						HasAttachment:   hasAttachment,
					}); recordErr != nil {
						logger.Error("lark_untriggered_journal_append_error", "chat_id", inbound.ChatID, "message_id", inbound.MessageID, "error", recordErr.Error())
					}
				}
				return nil
			}
			logger.Info("lark_group_trigger",
				"chat_id", inbound.ChatID,
				"reason", dec.Reason,
				"llm_addressed", dec.Addressing.Addressed,
				"confidence", dec.Addressing.Confidence,
				"wanna_interject", dec.Addressing.WannaInterject,
				"interject", dec.Addressing.Interject,
				"impulse", dec.Addressing.Impulse,
				"is_lightweight", dec.Addressing.IsLightweight,
			)
		}
		if !contextCompactionOnly && len(inbound.ImageKeys) == 0 && len(inbound.ImageAttachments) == 0 {
			if result := runControl.Steer("lark", msg.ConversationKey, text); result.Found {
				correlationID := fmt.Sprintf("lark:steer:%s:%s", inbound.ChatID, inbound.MessageID)
				_, publishErr := publishLarkBusOutbound(ctx, inprocBus, inbound.ChatID, runtimecontrol.SteerFeedback(result.Found, result.Queued), inbound.MessageID, correlationID)
				return publishErr
			}
		}
		workspaceResolution, err := workspace.Resolve(workspaceStore, msg.ConversationKey, d.DefaultWorkspaceDir)
		if err != nil {
			return err
		}
		workspaceDir := workspaceResolution.WorkspaceDir
		if len(inbound.ImageKeys) > 0 {
			imageCacheDir, dirErr := imagehistory.DownloadDir(opts.FileCacheDir, workspaceDir, chathistory.ChannelLark)
			if dirErr != nil {
				return dirErr
			}
			inbound = downloadLarkInboundImages(ctx, api, imageCacheDir, inbound, logger)
			text = strings.TrimSpace(inbound.Text)
		}
		imagePaths := busruntime.ImagePathsFromAttachments(inbound.ImageAttachments)
		images := imagehistory.BuildFromAttachments(inbound.ImageAttachments, pathroots.New(workspaceDir, opts.FileCacheDir, ""))
		jobTaskID := larkTaskID(inbound.ChatID, inbound.MessageID)
		generationLease, captureErr := runtimeGenerations.Capture()
		if captureErr != nil {
			return captureErr
		}
		runtimeBundle := generationLease.Bundle()
		if runtimeBundle == nil || runtimeBundle.TaskRuntime == nil {
			generationLease.Release()
			return fmt.Errorf("lark runtime generation is unavailable")
		}
		transferredGeneration := false
		defer func() {
			if !transferredGeneration {
				generationLease.Release()
			}
		}()
		taskRoute, err := runtimeBundle.TaskRuntime.ResolveTaskRouteForRun(llmstats.WithRunID(ctx, jobTaskID), text)
		if err != nil {
			return err
		}
		buildJob := func(version uint64) larkJob {
			admittedRoute := taskRoute
			return larkJob{
				TaskID:          jobTaskID,
				ConversationKey: msg.ConversationKey,
				ChatID:          inbound.ChatID,
				ChatType:        inbound.ChatType,
				MessageID:       inbound.MessageID,
				FromUserID:      inbound.FromUserID,
				DisplayName:     inbound.DisplayName,
				Text:            text,
				ImagePaths:      imagePaths,
				Images:          append([]chathistory.ChatHistoryImage(nil), images...),
				WorkspaceDir:    workspaceDir,
				Route:           &admittedRoute,
				SentAt:          inbound.SentAt,
				Version:         version,
				MentionUsers:    append([]string(nil), inbound.MentionUsers...),
				EventID:         inbound.EventID,
				Generation:      generationLease,
			}
		}
		if daemonStore != nil {
			createdAt := inbound.SentAt.UTC()
			if createdAt.IsZero() {
				createdAt = time.Now().UTC()
			}
			triggerRef := strings.TrimSpace(inbound.EventID)
			if triggerRef == "" {
				triggerRef = strings.TrimSpace(inbound.MessageID)
			}
			if triggerRef == "" {
				triggerRef = strings.TrimSpace(inbound.ChatID)
			}
			if err := taskdomain.RecordTaskUpsert(daemonStore, daemonruntime.TaskInfo{
				ID:           jobTaskID,
				Status:       daemonruntime.TaskQueued,
				Task:         textutil.TruncateRunes(text, 2000),
				Model:        strings.TrimSpace(taskRoute.ClientConfig.Model),
				Timeout:      taskTimeout.String(),
				CreatedAt:    createdAt,
				Conversation: larkTaskConversation(buildJob(0)),
				Result: map[string]any{
					"source":            "lark",
					"lark_chat_id":      inbound.ChatID,
					"lark_message_id":   inbound.MessageID,
					"lark_chat_type":    inbound.ChatType,
					"lark_from_open_id": inbound.FromUserID,
				},
			}, daemonruntime.TaskTrigger{
				Source: "lark",
				Event:  "websocket_inbound",
				Ref:    triggerRef,
			}); err != nil {
				return err
			}
		}
		if err := runner.Enqueue(ctx, msg.ConversationKey, buildJob); err != nil {
			if stateErr := runtimecore.MarkTaskFailed(daemonStore, jobTaskID, strings.TrimSpace(err.Error()), taskdomain.EndedByCancellation(ctx, err)); stateErr != nil {
				return fmt.Errorf("enqueue lark task: %v; persist failed state: %w", err, stateErr)
			}
			return err
		}
		transferredGeneration = true
		logger.Info("lark_task_enqueued",
			"channel", msg.Channel,
			"topic", msg.Topic,
			"chat_id", inbound.ChatID,
			"chat_type", inbound.ChatType,
			"idempotency_key", msg.IdempotencyKey,
			"conversation_key", msg.ConversationKey,
			"text_len", len(text),
		)
		return nil
	}

	busHandler := func(ctx context.Context, msg busruntime.BusMessage) error {
		switch msg.Direction {
		case busruntime.DirectionInbound:
			if msg.Channel != busruntime.ChannelLark {
				return fmt.Errorf("unsupported inbound channel: %s", msg.Channel)
			}
			if err := contactsSvc.ObserveInboundBusMessage(context.Background(), msg, time.Now().UTC()); err != nil {
				logger.Warn("contacts_observe_bus_error", "channel", msg.Channel, "idempotency_key", msg.IdempotencyKey, "error", err.Error())
			}
			if inbound, err := larkbus.InboundMessageFromBusMessage(msg); err == nil {
				openID := inbound.FromUserID
				avatarRefresher.Enqueue("lark_user:"+openID, func(ctx context.Context) ([]byte, bool, error) {
					avatarURL, err := api.userAvatarURL(ctx, openID)
					if err != nil {
						return nil, false, err
					}
					return contacts.FetchContactAvatarURL(ctx, api.http, avatarURL)
				})
			}
			if enqueueLarkInbound == nil {
				return fmt.Errorf("lark inbound handler is not initialized")
			}
			return enqueueLarkInbound(ctx, msg)
		case busruntime.DirectionOutbound:
			if msg.Channel != busruntime.ChannelLark {
				return fmt.Errorf("unsupported outbound channel: %s", msg.Channel)
			}
			if larkDeliveryAdapter == nil {
				return fmt.Errorf("lark delivery adapter is not initialized")
			}
			_, _, err := larkDeliveryAdapter.Deliver(ctx, msg)
			return err
		default:
			return fmt.Errorf("unsupported direction: %s", msg.Direction)
		}
	}
	for _, topic := range busruntime.AllTopics() {
		if err := inprocBus.Subscribe(topic, busHandler); err != nil {
			return err
		}
	}

	wsDomain := larkWebSocketDomainFromBaseURL(opts.BaseURL)

	logger.Info("lark_start",
		"base_url", strings.TrimSpace(opts.BaseURL),
		"websocket_domain", wsDomain,
		"allowed_chat_ids", len(allowedChats),
		"task_timeout", taskTimeout.String(),
		"max_concurrency", maxConcurrency,
		"group_trigger_mode", strings.TrimSpace(opts.GroupTriggerMode),
		"addressing_confidence_threshold", opts.AddressingConfidenceThreshold,
		"addressing_interject_threshold", opts.AddressingInterjectThreshold,
	)

	if err := runLarkWebSocketIngress(ctx, larkWebSocketIngressOptions{
		AppID:        opts.AppID,
		AppSecret:    opts.AppSecret,
		Domain:       wsDomain,
		Inbound:      larkInboundAdapter,
		AllowedChats: allowedChats,
		Logger:       logger,
		StopWorkers:  stopWorkers,
	}); err != nil {
		return err
	}
	logger.Info("lark_stop", "reason", "context_canceled")
	return nil
}

func toAllowlist(items []string) map[string]bool {
	out := make(map[string]bool)
	for _, raw := range items {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		out[item] = true
	}
	return out
}

func larkTaskID(chatID, messageID string) string {
	return daemonruntime.BuildTaskID("lk", chatID, messageID)
}

func sendLarkText(ctx context.Context, api *larkAPI, logger *slog.Logger, chatID, text, replyToMessageID string) error {
	if api == nil {
		return fmt.Errorf("lark api is not initialized")
	}
	chatID = strings.TrimSpace(chatID)
	text = strings.TrimSpace(text)
	replyToMessageID = strings.TrimSpace(replyToMessageID)
	if replyToMessageID != "" {
		if err := api.replyText(ctx, replyToMessageID, text); err == nil {
			return nil
		} else if logger != nil {
			logger.Warn("lark_reply_fallback_to_send", "chat_id", chatID, "reply_to_message_id", replyToMessageID, "error", err.Error())
		}
	}
	return api.sendText(ctx, "chat_id", chatID, text)
}
