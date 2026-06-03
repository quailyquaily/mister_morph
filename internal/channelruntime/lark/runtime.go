package lark

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	larkbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/lark"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/imagehistory"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/personautil"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/internal/workspace"
)

func runLarkLoop(ctx context.Context, d Dependencies, opts runtimeLoopOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(opts.AppID) == "" {
		return fmt.Errorf("missing lark.app_id")
	}
	if strings.TrimSpace(opts.AppSecret) == "" {
		return fmt.Errorf("missing lark.app_secret")
	}

	logger, err := depsutil.LoggerFromCommon(d.CommonDependencies)
	if err != nil {
		return err
	}
	slog.SetDefault(logger)

	daemonStore, err := daemonruntime.NewTaskViewForTarget("lark", opts.ServerMaxQueue)
	if err != nil {
		return err
	}
	inprocBus, err := busruntime.StartInproc(busruntime.BootstrapOptions{
		MaxInFlight: opts.BusMaxInFlight,
		Logger:      logger,
		Component:   "lark",
	})
	if err != nil {
		return err
	}
	defer inprocBus.Close()

	contactsStore := contacts.NewFileStore(statepaths.ContactsDir())
	if err := contactsStore.Ensure(context.Background()); err != nil {
		return err
	}
	workspaceStore := workspace.NewStore(statepaths.WorkspaceAttachmentsPath())
	contactsSvc := contacts.NewService(contactsStore)
	larkInboundAdapter, err := larkbus.NewInboundAdapter(larkbus.InboundAdapterOptions{
		Bus:   inprocBus,
		Store: contactsStore,
	})
	if err != nil {
		return err
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	tokenClient := NewTenantTokenClient(httpClient, strings.TrimSpace(opts.BaseURL), opts.AppID, opts.AppSecret)
	api := newLarkAPI(httpClient, strings.TrimSpace(opts.BaseURL), tokenClient)
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
	sharedRuntime, err := runtimecore.BootstrapChannelRuntime(ctx, d.CommonDependencies, runtimecore.ChannelBootstrapOptions{
		Mode:                "lark",
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
	defer sharedRuntime.Cleanup()
	execRuntime := sharedRuntime.TaskRuntime
	mainRoute := execRuntime.BootstrapMainRoute
	model := execRuntime.BootstrapMainModel
	addressingRoute := sharedRuntime.AddressingRoute
	addressingModel := sharedRuntime.AddressingModel
	addressingClient := sharedRuntime.AddressingClient
	memRuntime := sharedRuntime.Memory
	groupTriggerMode := strings.ToLower(strings.TrimSpace(opts.GroupTriggerMode))
	taskRuntimeOpts := runtimeTaskOptions{
		MemoryEnabled:           opts.MemoryEnabled,
		MemoryInjectionEnabled:  opts.MemoryInjectionEnabled,
		MemoryInjectionMaxItems: opts.MemoryInjectionMaxItems,
		FileCacheDir:            opts.FileCacheDir,
		ToolAPI:                 toolAPI,
		ToolFileMaxBytes:        larkToolFileMaxBytes,
		MemoryOrchestrator:      memRuntime.Orchestrator,
		MemoryProjectionWorker:  memRuntime.ProjectionWorker,
	}
	runControl := runtimecontrol.New()
	addressingLLMTimeout := addressingRoute.ClientConfig.RequestTimeout
	if addressingLLMTimeout <= 0 {
		addressingLLMTimeout = requestTimeout
	}
	addressingConfidenceThreshold := opts.AddressingConfidenceThreshold
	addressingInterjectThreshold := opts.AddressingInterjectThreshold

	taskTimeout := opts.TaskTimeout
	maxConcurrency := opts.MaxConcurrency
	sem := make(chan struct{}, maxConcurrency)
	workersCtx, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()
	allowedChats := toAllowlist(opts.AllowedChatIDs)

	serverListen := strings.TrimSpace(opts.ServerListen)
	if serverListen != "" {
		if strings.TrimSpace(opts.ServerAuthToken) == "" {
			logger.Warn("lark_daemon_server_auth_empty", "hint", "set server.auth_token so console can read /tasks")
		}
		_, err := daemonruntime.StartServer(ctx, logger, daemonruntime.ServerOptions{
			Listen: serverListen,
			Routes: daemonruntime.RoutesOptions{
				Mode:          "lark",
				AgentNameFunc: func() string { return personautil.LoadAgentName(statepaths.FileStateDir()) },
				AuthToken:     strings.TrimSpace(opts.ServerAuthToken),
				TaskReader:    daemonStore,
				Overview: func(ctx context.Context) (map[string]any, error) {
					return map[string]any{
						"llm": map[string]any{
							"provider": strings.TrimSpace(mainRoute.ClientConfig.Provider),
							"model":    model,
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
				HealthEnabled:        true,
			},
		})
		if err != nil {
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
			mu.Lock()
			h := append([]chathistory.ChatHistoryItem(nil), history[conversationKey]...)
			sticky := append([]string(nil), stickySkillsByConv[conversationKey]...)
			mu.Unlock()
			curVersion := runner.CurrentVersion(conversationKey)
			if job.Version != curVersion {
				h = nil
			}
			runtimecore.MarkTaskRunning(daemonStore, job.TaskID)
			lease, err := runControl.StartLease(workerCtx, taskTimeout, runtimecontrol.ActiveRun{
				Runtime:         "lark",
				ConversationKey: conversationKey,
				TopicID:         job.ChatID,
				TaskID:          job.TaskID,
				RunID:           job.TaskID,
				Snapshot: func() string {
					return "任务正在运行中，等待当前模型或工具调用返回。"
				},
			})
			if err != nil {
				runtimecore.MarkTaskFailed(daemonStore, job.TaskID, strings.TrimSpace(err.Error()), false)
				return
			}
			final, _, loadedSkills, runErr := runLarkTask(
				lease.Context,
				execRuntime,
				job,
				h,
				sticky,
				taskRuntimeOpts,
				lease.SteerQueue,
			)
			userStopped := lease.UserStopped()
			lease.Finish()
			if runErr != nil {
				if workerCtx.Err() != nil {
					return
				}
				displayErr := depsutil.FormatRuntimeError(runErr)
				if userStopped {
					displayErr = "stopped by user"
				}
				runtimecore.MarkTaskFailed(daemonStore, job.TaskID, displayErr, userStopped)
				logger.Warn("lark_task_error",
					"chat_id", job.ChatID,
					"message_id", job.MessageID,
					"error", displayErr,
				)
				errorText := "error: " + displayErr
				if userStopped {
					errorText = "已停止当前任务。"
				}
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
			outText := ""
			if shouldPublishLarkText(final) {
				outText = strings.TrimSpace(depsutil.FormatFinalOutput(final))
			}
			runtimecore.MarkTaskDone(daemonStore, job.TaskID, outText)
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
			if latestVersion != curVersion {
				history[conversationKey] = nil
				stickySkillsByConv[conversationKey] = nil
			}
			if latestVersion == curVersion && len(loadedSkills) > 0 {
				stickySkillsByConv[conversationKey] = capUniqueStrings(loadedSkills, larkStickySkillsCap)
			}
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
			mu.Unlock()
		},
	)

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
		mu.Lock()
		currentSkills := append([]string(nil), stickySkillsByConv[msg.ConversationKey]...)
		mu.Unlock()
		if isLarkStopCommand(inbound.Text) {
			result := runControl.Stop("lark", msg.ConversationKey, "/stop")
			correlationID := fmt.Sprintf("lark:stop:%s:%s", inbound.ChatID, inbound.MessageID)
			_, publishErr := publishLarkBusOutbound(ctx, inprocBus, inbound.ChatID, runtimecontrol.StopFeedback(result.Found, result.Progress), inbound.MessageID, correlationID)
			return publishErr
		}
		if handledCommand, cmdErr := maybeHandleLarkCommand(ctx, d, inprocBus, workspaceStore, msg.ConversationKey, inbound, currentSkills); handledCommand {
			return cmdErr
		}
		if strings.EqualFold(strings.TrimSpace(inbound.ChatType), "group") {
			mu.Lock()
			historySnapshot := append([]chathistory.ChatHistoryItem(nil), history[msg.ConversationKey]...)
			mu.Unlock()
			decisionCtx := llmstats.WithMetadata(context.Background(), larkTaskID(inbound.ChatID, inbound.MessageID), inbound.EventID)
			dec, accepted, decErr := decideLarkGroupTrigger(
				decisionCtx,
				addressingClient,
				addressingModel,
				inbound,
				groupTriggerMode,
				addressingLLMTimeout,
				addressingConfidenceThreshold,
				addressingInterjectThreshold,
				historySnapshot,
			)
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
					cur = append(cur, newLarkInboundHistoryItemFromInbound(inbound))
					history[msg.ConversationKey] = trimChatHistoryItems(cur, larkHistoryCapForMode(groupTriggerMode))
					mu.Unlock()
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
		if len(inbound.ImageKeys) == 0 && len(inbound.ImageAttachments) == 0 {
			if result := runControl.Steer("lark", msg.ConversationKey, text); result.Found {
				correlationID := fmt.Sprintf("lark:steer:%s:%s", inbound.ChatID, inbound.MessageID)
				_, publishErr := publishLarkBusOutbound(ctx, inprocBus, inbound.ChatID, runtimecontrol.SteerFeedback(result.Found, result.Queued), inbound.MessageID, correlationID)
				return publishErr
			}
		}
		workspaceDir, err := workspace.LookupWorkspaceDir(workspaceStore, msg.ConversationKey)
		if err != nil {
			return err
		}
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
		if err := runner.Enqueue(ctx, msg.ConversationKey, func(version uint64) larkJob {
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
				SentAt:          inbound.SentAt,
				Version:         version,
				MentionUsers:    append([]string(nil), inbound.MentionUsers...),
				EventID:         inbound.EventID,
			}
		}); err != nil {
			return err
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
			_ = daemonruntime.RecordTaskUpsert(daemonStore, daemonruntime.TaskInfo{
				ID:        jobTaskID,
				Status:    daemonruntime.TaskQueued,
				Task:      daemonruntime.TruncateUTF8(text, 2000),
				Model:     model,
				Timeout:   taskTimeout.String(),
				CreatedAt: createdAt,
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
			})
		}
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
