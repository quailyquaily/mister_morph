package line

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/contacts"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	linebus "github.com/quailyquaily/mistermorph/internal/bus/adapters/line"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/imagehistory"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/personautil"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
	"github.com/quailyquaily/mistermorph/internal/taskdomain"
	"github.com/quailyquaily/mistermorph/internal/telegramutil"
	"github.com/quailyquaily/mistermorph/internal/textutil"
	"github.com/quailyquaily/mistermorph/internal/workspace"
)

type lineJob struct {
	TaskID          string
	ConversationKey string
	ChatID          string
	ChatType        string
	MessageID       string
	ReplyToken      string
	FromUserID      string
	FromUsername    string
	DisplayName     string
	Text            string
	ImagePaths      []string
	Images          []chathistory.ChatHistoryImage
	WorkspaceDir    string
	Route           *llmutil.ResolvedRoute
	SentAt          time.Time
	Version         uint64
	MentionUsers    []string
	EventID         string
	Generation      *runtimecore.RuntimeGenerationLease
}

func (j lineJob) runtimeBundle() *runtimecore.ChannelRuntimeBundle {
	if j.Generation == nil {
		return nil
	}
	return j.Generation.Bundle()
}

func (j lineJob) releaseGeneration() {
	if j.Generation != nil {
		j.Generation.Release()
	}
}

const lineImageDownloadTimeout = 20 * time.Second

func runLineLoop(ctx context.Context, d Dependencies, opts RunOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(opts.ChannelAccessToken) == "" {
		return fmt.Errorf("missing line.channel_access_token (set via --line-channel-access-token or MISTER_MORPH_LINE_CHANNEL_ACCESS_TOKEN)")
	}
	channelSecret := strings.TrimSpace(opts.ChannelSecret)
	if channelSecret == "" {
		return fmt.Errorf("missing line.channel_secret (set via --line-channel-secret or MISTER_MORPH_LINE_CHANNEL_SECRET)")
	}

	logger, err := d.Logger()
	if err != nil {
		return err
	}
	var untriggeredRecorder *runtimecore.UntriggeredRecorder
	if opts.RecordUntriggered {
		untriggeredRecorder, err = runtimecore.NewUntriggeredRecorder(d.RuntimePaths.JournalDir, d.TaskRotateMaxBytes)
		if err != nil {
			return fmt.Errorf("line untriggered journal: %w", err)
		}
		defer untriggeredRecorder.Close()
	}
	daemonStore, err := daemonruntime.NewTaskViewForTarget("line", opts.ServerMaxQueue, daemonruntime.TaskViewConfig{
		PersistenceTargets: d.TaskPersistenceTargets,
		TasksDir:           d.RuntimePaths.TasksDir,
		JournalDir:         d.RuntimePaths.JournalDir,
		RotateMaxBytes:     d.TaskRotateMaxBytes,
	})
	if err != nil {
		return err
	}
	inprocBus, err := busruntime.StartInproc(busruntime.BootstrapOptions{
		MaxInFlight: opts.BusMaxInFlight,
		Logger:      logger,
		Component:   "line",
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
	lineInboundAdapter, err := linebus.NewInboundAdapter(linebus.InboundAdapterOptions{
		Bus:   inprocBus,
		Store: contactsStore,
	})
	if err != nil {
		return err
	}
	baseURL := strings.TrimSpace(opts.BaseURL)
	httpClient := &http.Client{Timeout: 30 * time.Second}
	api := newLineAPI(httpClient, baseURL, opts.ChannelAccessToken)
	lineDeliveryAdapter, err := linebus.NewDeliveryAdapter(linebus.DeliveryAdapterOptions{
		SendText: func(ctx context.Context, target any, text string, opts linebus.SendTextOptions) error {
			deliverTarget, ok := target.(linebus.DeliveryTarget)
			if !ok {
				return fmt.Errorf("line target is invalid")
			}
			return sendLineText(ctx, api, logger, deliverTarget.ChatID, text, opts.ReplyToken)
		},
	})
	if err != nil {
		return err
	}
	requestTimeout := opts.RequestTimeout
	runtimeGenerations, err := runtimecore.BootstrapRuntimeGenerationManager(ctx, d.CommonDependencies, runtimecore.ChannelBootstrapOptions{
		Mode:                "line",
		InspectRequest:      opts.InspectRequest,
		InspectPrompt:       opts.InspectPrompt,
		AgentConfig:         opts.AgentLimits.ToConfig(),
		EngineToolsConfig:   &opts.EngineToolsConfig,
		MemoryEnabled:       opts.MemoryEnabled,
		MemoryShortTermDays: opts.MemoryShortTermDays,
		Logger:              logger,
	})
	if err != nil {
		return err
	}
	defer runtimeGenerations.Close()
	groupTriggerMode := strings.ToLower(strings.TrimSpace(opts.GroupTriggerMode))
	runControl := runtimecontrol.New()
	fileCacheDir := pathutil.ExpandHomePath(strings.TrimSpace(opts.FileCacheDir))
	if fileCacheDir == "" {
		return fmt.Errorf("line file cache dir is required for image recognition")
	}
	if err := telegramutil.EnsureSecureCacheDir(fileCacheDir); err != nil {
		return fmt.Errorf("line file cache dir: %w", err)
	}
	addressingConfidenceThreshold := opts.AddressingConfidenceThreshold
	addressingInterjectThreshold := opts.AddressingInterjectThreshold
	botUserID := ""
	botInfoCtx, cancelBotInfo := context.WithTimeout(ctx, 8*time.Second)
	resolvedBotUserID, botInfoErr := api.botUserID(botInfoCtx)
	cancelBotInfo()
	if botInfoErr != nil {
		logger.Warn("line_bot_info_load_failed", "error", botInfoErr.Error())
	} else {
		botUserID = strings.TrimSpace(resolvedBotUserID)
	}

	taskTimeout := opts.TaskTimeout
	maxConcurrency := opts.MaxConcurrency
	sem := make(chan struct{}, maxConcurrency)
	workersCtx, stopWorkers := newLineOwnedContext(ctx)
	allowedGroups := toAllowlist(opts.AllowedGroupIDs)

	serverListen := strings.TrimSpace(opts.ServerListen)
	var daemonServer *http.Server
	var stopDaemonServer context.CancelFunc
	if serverListen != "" {
		if strings.TrimSpace(opts.ServerAuthToken) == "" {
			logger.Warn("line_daemon_server_auth_empty", "hint", "set server.auth_token so console can read /tasks")
		}
		daemonServerCtx, cancelDaemonServer := newLineOwnedContext(ctx)
		stopDaemonServer = cancelDaemonServer
		daemonServer, err = daemonruntime.StartServer(daemonServerCtx, logger, daemonruntime.ServerOptions{
			Listen: serverListen,
			Routes: daemonruntime.RoutesOptions{
				Mode:          "line",
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
						return nil, fmt.Errorf("line runtime generation is unavailable")
					}
					mainRoute := runtimeBundle.TaskRuntime.BootstrapMainRoute
					return map[string]any{
						"llm": map[string]any{
							"provider": strings.TrimSpace(mainRoute.ClientConfig.Provider),
							"model":    runtimeBundle.TaskRuntime.BootstrapMainModel,
						},
						"channel": map[string]any{
							"configured":          true,
							"telegram_configured": false,
							"slack_configured":    false,
							"line_configured":     true,
							"running":             "line",
							"telegram_running":    false,
							"slack_running":       false,
							"line_running":        true,
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
			logger.Warn("line_daemon_server_start_error", "addr", serverListen, "error", err.Error())
		}
	}

	var (
		mu                 sync.Mutex
		history            = make(map[string][]chathistory.ChatHistoryItem)
		stickySkillsByConv = make(map[string][]string)
		enqueueLineInbound func(context.Context, busruntime.BusMessage) error
	)
	var runner *runtimecore.ConversationRunner[string, lineJob]
	runner = runtimecore.NewConversationRunner[string, lineJob](
		workersCtx,
		sem,
		16,
		func(workerCtx context.Context, conversationKey string, job lineJob) {
			defer job.releaseGeneration()
			runtimeBundle := job.runtimeBundle()
			if runtimeBundle == nil || runtimeBundle.TaskRuntime == nil {
				runtimecore.MarkTaskFailed(daemonStore, job.TaskID, "line runtime generation is unavailable", false)
				return
			}
			memRuntime := runtimeBundle.Memory
			taskRuntimeOpts := runtimeTaskOptions{
				FileCacheDir:            opts.FileCacheDir,
				MemoryEnabled:           opts.MemoryEnabled,
				MemoryInjectionEnabled:  opts.MemoryInjectionEnabled,
				MemoryInjectionMaxItems: opts.MemoryInjectionMaxItems,
				MemoryOrchestrator:      memRuntime.Orchestrator,
				MemoryProjectionWorker:  memRuntime.ProjectionWorker,
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
				logger.Error("line_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskRunning, "error", err.Error())
				return
			}
			lease, err := runControl.StartLease(workerCtx, taskTimeout, runtimecontrol.ActiveRun{
				Runtime:         "line",
				ConversationKey: conversationKey,
				TopicID:         job.ChatID,
				TaskID:          job.TaskID,
				RunID:           job.TaskID,
			})
			if err != nil {
				if cancelLineTaskOnWorkerShutdown(workerCtx, logger, daemonStore, job) {
					return
				}
				if stateErr := runtimecore.MarkTaskFailed(daemonStore, job.TaskID, strings.TrimSpace(err.Error()), false); stateErr != nil {
					logger.Error("line_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskFailed, "error", stateErr.Error())
				}
				return
			}
			runCtx := taskruntime.WithContextCompactionNotification(lease.Context, logger, func(notifyCtx context.Context, event agent.Event, text string) error {
				correlationID := fmt.Sprintf("line:context-compaction:%s:%d", job.TaskID, event.Step)
				_, notifyErr := publishLineBusOutbound(notifyCtx, inprocBus, job.ChatID, text, "", correlationID)
				return notifyErr
			})
			final, _, loadedSkills, runErr := runLineTask(
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
				if cancelLineTaskOnWorkerShutdown(workerCtx, logger, daemonStore, job) {
					return
				}
				displayErr := depsutil.FormatRuntimeError(runErr)
				if userStopped {
					displayErr = "stopped by user"
				}
				if stateErr := runtimecore.MarkTaskFailed(daemonStore, job.TaskID, displayErr, userStopped); stateErr != nil {
					logger.Error("line_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskFailed, "error", stateErr.Error())
				}
				logger.Warn("line_task_error",
					"chat_id", job.ChatID,
					"message_id", job.MessageID,
					"error", displayErr,
				)
				if userStopped {
					return
				}
				errorText := "error: " + displayErr
				errorCorrelationID := fmt.Sprintf("line:error:%s:%s", job.ChatID, job.MessageID)
				_, err := publishLineBusOutbound(workerCtx, inprocBus, job.ChatID, errorText, job.ReplyToken, errorCorrelationID)
				if err != nil {
					logger.Warn("line_bus_publish_error",
						"channel", busruntime.ChannelLine,
						"chat_id", job.ChatID,
						"bus_error_code", string(busruntime.ErrorCodeOf(err)),
						"error", err.Error(),
					)
				}
				return
			}
			outText := ""
			if shouldPublishLineText(final) {
				outText = strings.TrimSpace(outputfmt.FormatFinalOutput(final))
			}
			if err := runtimecore.MarkTaskDone(daemonStore, job.TaskID, outText); err != nil {
				logger.Error("line_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskDone, "error", err.Error())
				return
			}
			if outText != "" {
				if workerCtx.Err() != nil {
					return
				}
				outCorrelationID := fmt.Sprintf("line:message:%s:%s", job.ChatID, job.MessageID)
				_, err := publishLineBusOutbound(workerCtx, inprocBus, job.ChatID, outText, job.ReplyToken, outCorrelationID)
				if err != nil {
					logger.Warn("line_bus_publish_error",
						"channel", busruntime.ChannelLine,
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
				stickySkillsByConv[conversationKey] = capUniqueStrings(loadedSkills, lineStickySkillsCap)
			}
			if !contextCompactionOnly {
				cur := history[conversationKey]
				inboundHistory := newLineInboundHistoryItem(job)
				if outText != "" {
					inboundHistory.Images = imagehistory.WithDescription(inboundHistory.Images, outText, "agent_final")
				}
				cur = append(cur, inboundHistory)
				if outText != "" {
					cur = append(cur, newLineOutboundAgentHistoryItem(job, outText, time.Now().UTC()))
				}
				history[conversationKey] = trimChatHistoryItems(cur, lineHistoryCapForMode(groupTriggerMode))
			}
			mu.Unlock()
		},
		lineConversationRunnerOptions(logger, daemonStore, runControl),
	)
	// Stop daemon admission, drain bus handlers, then join tasks before shared cleanup.
	defer shutdownLineRuntime(daemonServer, stopDaemonServer, inprocBus, stopWorkers, runner)
	busShutdownTransferred = true
	runtimeGenerations.Start(ctx)
	enqueueLineInbound = func(ctx context.Context, msg busruntime.BusMessage) error {
		if ctx == nil {
			ctx = workersCtx
		}
		inbound, err := linebus.InboundMessageFromBusMessage(msg)
		if err != nil {
			return err
		}
		text := strings.TrimSpace(inbound.Text)
		if text == "" {
			return fmt.Errorf("line inbound text is required")
		}
		contextCompactionOnly := chatcommands.IsContextCompactCommand(text)
		mu.Lock()
		currentSkills := append([]string(nil), stickySkillsByConv[msg.ConversationKey]...)
		mu.Unlock()
		if isLineStopCommand(inbound.Text) {
			result := runControl.Stop("line", msg.ConversationKey, "/stop")
			correlationID := fmt.Sprintf("line:stop:%s:%s", inbound.ChatID, inbound.MessageID)
			_, publishErr := publishLineBusOutbound(ctx, inprocBus, inbound.ChatID, runtimecontrol.StopFeedback(result.Found), inbound.ReplyToken, correlationID)
			return publishErr
		}
		if handledCommand, cmdErr := maybeHandleLineCommand(ctx, d, inprocBus, workspaceStore, msg.ConversationKey, inbound, currentSkills); handledCommand {
			return cmdErr
		}
		if !contextCompactionOnly && strings.EqualFold(strings.TrimSpace(inbound.ChatType), "group") {
			mu.Lock()
			historySnapshot := append([]chathistory.ChatHistoryItem(nil), history[msg.ConversationKey]...)
			mu.Unlock()
			decisionCtx := llmstats.WithMetadata(context.Background(), lineTaskID(inbound.ChatID, inbound.MessageID), inbound.EventID)
			addressingLease, captureErr := runtimeGenerations.Capture()
			if captureErr != nil {
				logger.Warn("line_runtime_generation_unavailable", "error", captureErr.Error())
				return nil
			}
			addressingBundle := addressingLease.Bundle()
			if addressingBundle == nil || addressingBundle.TaskRuntime == nil {
				addressingLease.Release()
				logger.Warn("line_runtime_generation_unavailable", "error", "runtime bundle is unavailable")
				return nil
			}
			addressingLLMTimeout := addressingBundle.AddressingRoute.ClientConfig.RequestTimeout
			if addressingLLMTimeout <= 0 {
				addressingLLMTimeout = requestTimeout
			}
			dec, accepted, decErr := decideLineGroupTrigger(
				decisionCtx,
				addressingBundle.AddressingClient,
				addressingBundle.AddressingModel,
				inbound,
				botUserID,
				groupTriggerMode,
				addressingLLMTimeout,
				addressingConfidenceThreshold,
				addressingInterjectThreshold,
				historySnapshot,
				d.RuntimePaths.PersonaDir,
			)
			addressingLease.Release()
			if decErr != nil {
				logger.Warn("line_addressing_llm_error",
					"chat_id", inbound.ChatID,
					"error", decErr.Error(),
				)
				return nil
			}
			if !accepted {
				logger.Info("line_group_ignored",
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
					cur = append(cur, newLineInboundHistoryItemFromInbound(inbound))
					history[msg.ConversationKey] = trimChatHistoryItems(cur, lineHistoryCapForMode(groupTriggerMode))
					mu.Unlock()
				}
				if untriggeredRecorder != nil {
					untriggeredText := inbound.Text
					if inbound.ImagePending {
						untriggeredText = ""
					}
					if recordErr := untriggeredRecorder.Record(runtimecore.UntriggeredMessage{
						Channel:         string(busruntime.ChannelLine),
						ConversationKey: msg.ConversationKey,
						MessageID:       inbound.MessageID,
						SenderID:        inbound.FromUserID,
						SentAt:          inbound.SentAt,
						Text:            untriggeredText,
						HasAttachment:   inbound.ImagePending || len(inbound.ImagePaths) > 0 || len(inbound.ImageAttachments) > 0,
					}); recordErr != nil {
						logger.Error("line_untriggered_journal_append_error", "chat_id", inbound.ChatID, "message_id", inbound.MessageID, "error", recordErr.Error())
					}
				}
				return nil
			}
			logger.Info("line_group_trigger",
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
		if !contextCompactionOnly && !inbound.ImagePending && len(inbound.ImageAttachments) == 0 {
			if result := runControl.Steer("line", msg.ConversationKey, text); result.Found {
				correlationID := fmt.Sprintf("line:steer:%s:%s", inbound.ChatID, inbound.MessageID)
				_, publishErr := publishLineBusOutbound(ctx, inprocBus, inbound.ChatID, runtimecontrol.SteerFeedback(result.Found, result.Queued), inbound.ReplyToken, correlationID)
				return publishErr
			}
		}
		workspaceResolution, err := workspace.Resolve(workspaceStore, msg.ConversationKey, d.DefaultWorkspaceDir)
		if err != nil {
			return err
		}
		workspaceDir := workspaceResolution.WorkspaceDir
		if inbound.ImagePending {
			if api == nil {
				logger.Warn("line_image_download_skip", "chat_id", inbound.ChatID, "message_id", inbound.MessageID, "reason", "api_not_initialized")
				return nil
			}
			imageCacheDir, dirErr := imagehistory.DownloadDir(fileCacheDir, workspaceDir, chathistory.ChannelLine)
			if dirErr != nil {
				return dirErr
			}
			imageCtx := ctx
			if imageCtx == nil {
				imageCtx = workersCtx
			}
			imageCtx, cancelImage := context.WithTimeout(imageCtx, lineImageDownloadTimeout)
			path, imageErr := downloadLineImageToCache(imageCtx, api, imageCacheDir, inbound.MessageID, lineLLMMaxImageBytes)
			cancelImage()
			if imageErr != nil {
				logger.Warn("line_image_download_error",
					"chat_id", inbound.ChatID,
					"message_id", inbound.MessageID,
					"error", imageErr.Error(),
				)
				errorText := "error: failed to fetch image content"
				errorCorrelationID := fmt.Sprintf("line:image_error:%s:%s", inbound.ChatID, inbound.MessageID)
				_, publishErr := publishLineBusOutbound(workersCtx, inprocBus, inbound.ChatID, errorText, inbound.ReplyToken, errorCorrelationID)
				if publishErr != nil {
					logger.Warn("line_bus_publish_error",
						"channel", busruntime.ChannelLine,
						"chat_id", inbound.ChatID,
						"bus_error_code", string(busruntime.ErrorCodeOf(publishErr)),
						"error", publishErr.Error(),
					)
				}
				return nil
			}
			inbound.ImageAttachments = []busruntime.ImageAttachment{{
				Path:               path,
				SourceMessageID:    strings.TrimSpace(inbound.MessageID),
				SourceAttachmentID: "image",
			}}
			inbound.ImagePending = false
		}
		imagePaths := busruntime.ImagePathsFromAttachments(inbound.ImageAttachments)
		images := imagehistory.BuildFromAttachments(inbound.ImageAttachments, pathroots.New(workspaceDir, fileCacheDir, ""))
		jobTaskID := lineTaskID(inbound.ChatID, inbound.MessageID)
		generationLease, captureErr := runtimeGenerations.Capture()
		if captureErr != nil {
			return captureErr
		}
		runtimeBundle := generationLease.Bundle()
		if runtimeBundle == nil || runtimeBundle.TaskRuntime == nil {
			generationLease.Release()
			return fmt.Errorf("line runtime generation is unavailable")
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
		buildJob := func(version uint64) lineJob {
			admittedRoute := taskRoute
			return lineJob{
				TaskID:          jobTaskID,
				ConversationKey: msg.ConversationKey,
				ChatID:          inbound.ChatID,
				ChatType:        inbound.ChatType,
				MessageID:       inbound.MessageID,
				ReplyToken:      inbound.ReplyToken,
				FromUserID:      inbound.FromUserID,
				FromUsername:    inbound.FromUsername,
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
				ID:        jobTaskID,
				Status:    daemonruntime.TaskQueued,
				Task:      textutil.TruncateRunes(text, 2000),
				Model:     strings.TrimSpace(taskRoute.ClientConfig.Model),
				Timeout:   taskTimeout.String(),
				CreatedAt: createdAt,
				Result: map[string]any{
					"source":            "line",
					"line_chat_id":      inbound.ChatID,
					"line_message_id":   inbound.MessageID,
					"line_chat_type":    inbound.ChatType,
					"line_from_user_id": inbound.FromUserID,
				},
			}, daemonruntime.TaskTrigger{
				Source: "webhook",
				Event:  "webhook_inbound",
				Ref:    triggerRef,
			}); err != nil {
				return err
			}
		}
		if err := runner.Enqueue(ctx, msg.ConversationKey, buildJob); err != nil {
			if stateErr := runtimecore.MarkTaskFailed(daemonStore, jobTaskID, strings.TrimSpace(err.Error()), taskdomain.EndedByCancellation(ctx, err)); stateErr != nil {
				return fmt.Errorf("enqueue line task: %v; persist failed state: %w", err, stateErr)
			}
			return err
		}
		transferredGeneration = true
		logger.Info("line_task_enqueued",
			"channel", msg.Channel,
			"topic", msg.Topic,
			"chat_id", inbound.ChatID,
			"chat_type", inbound.ChatType,
			"idempotency_key", msg.IdempotencyKey,
			"conversation_key", msg.ConversationKey,
			"text_len", len(text),
			"image_count", len(inbound.ImageAttachments),
		)
		return nil
	}

	busHandler := func(ctx context.Context, msg busruntime.BusMessage) error {
		switch msg.Direction {
		case busruntime.DirectionInbound:
			if msg.Channel != busruntime.ChannelLine {
				return fmt.Errorf("unsupported inbound channel: %s", msg.Channel)
			}
			if err := contactsSvc.ObserveInboundBusMessage(context.Background(), msg, time.Now().UTC()); err != nil {
				logger.Warn("contacts_observe_bus_error", "channel", msg.Channel, "idempotency_key", msg.IdempotencyKey, "error", err.Error())
			}
			if enqueueLineInbound == nil {
				return fmt.Errorf("line inbound handler is not initialized")
			}
			return enqueueLineInbound(ctx, msg)
		case busruntime.DirectionOutbound:
			if msg.Channel != busruntime.ChannelLine {
				return fmt.Errorf("unsupported outbound channel: %s", msg.Channel)
			}
			if lineDeliveryAdapter == nil {
				return fmt.Errorf("line delivery adapter is not initialized")
			}
			_, _, err := lineDeliveryAdapter.Deliver(ctx, msg)
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

	webhookPath := normalizeWebhookPath(opts.WebhookPath)
	webhookMux := http.NewServeMux()
	webhookMux.Handle(webhookPath, newLineWebhookHandler(lineWebhookHandlerOptions{
		ChannelSecret: channelSecret,
		Inbound:       lineInboundAdapter,
		AllowedGroups: allowedGroups,
		Logger:        logger,
	}))
	webhookServer := &http.Server{
		Addr:              strings.TrimSpace(opts.WebhookListen),
		Handler:           webhookMux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	webhookErrCh := make(chan error, 1)
	go func() {
		err := webhookServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			webhookErrCh <- err
			return
		}
		webhookErrCh <- nil
	}()
	logger.Info("line_start",
		"base_url", strings.TrimSpace(opts.BaseURL),
		"webhook_listen", strings.TrimSpace(opts.WebhookListen),
		"webhook_path", webhookPath,
		"bot_user_id_present", strings.TrimSpace(botUserID) != "",
		"allowed_group_ids", len(allowedGroups),
		"task_timeout", taskTimeout.String(),
		"max_concurrency", maxConcurrency,
		"group_trigger_mode", strings.TrimSpace(opts.GroupTriggerMode),
		"addressing_confidence_threshold", opts.AddressingConfidenceThreshold,
		"addressing_interject_threshold", opts.AddressingInterjectThreshold,
	)

	select {
	case err := <-webhookErrCh:
		if err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
		logger.Info("line_stop", "reason", "context_canceled")
		return stopAndWaitLineWebhook(webhookServer, webhookErrCh, stopWorkers)
	}
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

func lineTaskID(chatID, messageID string) string {
	return daemonruntime.BuildTaskID("li", chatID, messageID)
}
