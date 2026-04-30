package line

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	linebus "github.com/quailyquaily/mistermorph/internal/bus/adapters/line"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llminspect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/personautil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/internal/telegramutil"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/quailyquaily/mistermorph/llm"
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
	WorkspaceDir    string
	SentAt          time.Time
	Version         uint64
	MentionUsers    []string
	EventID         string
}

const lineImageDownloadTimeout = 20 * time.Second

func runLineLoop(ctx context.Context, d Dependencies, opts runtimeLoopOptions) error {
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

	logger, err := depsutil.LoggerFromCommon(d.CommonDependencies)
	if err != nil {
		return err
	}
	slog.SetDefault(logger)

	daemonStore, err := daemonruntime.NewTaskViewForTarget("line", opts.ServerMaxQueue)
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
	defer inprocBus.Close()

	contactsStore := contacts.NewFileStore(statepaths.ContactsDir())
	if err := contactsStore.Ensure(context.Background()); err != nil {
		return err
	}
	workspaceStore := workspace.NewStore(statepaths.WorkspaceAttachmentsPath())
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
	var requestInspector *llminspect.RequestInspector
	if opts.InspectRequest {
		requestInspector, err = llminspect.NewRequestInspector(llminspect.Options{
			Mode:            "line",
			Task:            "line",
			TimestampFormat: "20060102_150405",
		})
		if err != nil {
			return err
		}
		defer func() { _ = requestInspector.Close() }()
	}
	var promptInspector *llminspect.PromptInspector
	if opts.InspectPrompt {
		promptInspector, err = llminspect.NewPromptInspector(llminspect.Options{
			Mode:            "line",
			Task:            "line",
			TimestampFormat: "20060102_150405",
		})
		if err != nil {
			return err
		}
		defer func() { _ = promptInspector.Close() }()
	}
	decorateRuntimeClient := func(client llm.Client, route llmutil.ResolvedRoute) llm.Client {
		return llminspect.WrapClient(client, llminspect.ClientOptions{
			PromptInspector:  promptInspector,
			RequestInspector: requestInspector,
			APIBase:          route.ClientConfig.Endpoint,
			Model:            strings.TrimSpace(route.ClientConfig.Model),
		})
	}
	execRuntime, err := taskruntime.Bootstrap(d.CommonDependencies, taskruntime.BootstrapOptions{
		AgentConfig:       opts.AgentLimits.ToConfig(),
		EngineToolsConfig: &opts.EngineToolsConfig,
		ClientDecorator:   decorateRuntimeClient,
	})
	if err != nil {
		return err
	}
	mainRoute := execRuntime.BootstrapMainRoute
	model := execRuntime.BootstrapMainModel
	addressingRoute, err := depsutil.ResolveLLMRouteFromCommon(d.CommonDependencies, llmutil.RoutePurposeAddressing)
	if err != nil {
		return err
	}
	addressingModel := strings.TrimSpace(addressingRoute.ClientConfig.Model)
	addressingClient := execRuntime.BootstrapMainClient
	if !addressingRoute.SameProfile(mainRoute) {
		addressingClient, err = depsutil.CreateClient(d.CreateLLMClient, addressingRoute)
		if err != nil {
			return err
		}
		addressingClient = decorateRuntimeClient(addressingClient, addressingRoute)
	}
	memRuntime, err := runtimecore.NewMemoryRuntime(d.CommonDependencies, runtimecore.MemoryRuntimeOptions{
		Enabled:       opts.MemoryEnabled,
		ShortTermDays: opts.MemoryShortTermDays,
		Logger:        logger,
		Decorate:      decorateRuntimeClient,
	})
	if err != nil {
		return err
	}
	if memRuntime.ProjectionWorker != nil {
		memRuntime.ProjectionWorker.Start(ctx)
	}
	defer memRuntime.Cleanup()
	groupTriggerMode := strings.ToLower(strings.TrimSpace(opts.GroupTriggerMode))
	taskRuntimeOpts := runtimeTaskOptions{
		ImageRecognitionEnabled: opts.ImageRecognitionEnabled,
		MemoryEnabled:           opts.MemoryEnabled,
		MemoryInjectionEnabled:  opts.MemoryInjectionEnabled,
		MemoryInjectionMaxItems: opts.MemoryInjectionMaxItems,
		MemoryOrchestrator:      memRuntime.Orchestrator,
		MemoryProjectionWorker:  memRuntime.ProjectionWorker,
	}
	lineImageCacheDir := ""
	if opts.ImageRecognitionEnabled {
		fileCacheDir := pathutil.ExpandHomePath(strings.TrimSpace(opts.FileCacheDir))
		if fileCacheDir == "" {
			return fmt.Errorf("line file cache dir is required for image recognition")
		}
		if err := telegramutil.EnsureSecureCacheDir(fileCacheDir); err != nil {
			return fmt.Errorf("line file cache dir: %w", err)
		}
		lineImageCacheDir = filepath.Join(fileCacheDir, "line")
		if err := ensureLineSecureChildDir(fileCacheDir, lineImageCacheDir); err != nil {
			return fmt.Errorf("line cache subdir: %w", err)
		}
	}
	addressingLLMTimeout := addressingRoute.ClientConfig.RequestTimeout
	if addressingLLMTimeout <= 0 {
		addressingLLMTimeout = requestTimeout
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
	workersCtx, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()
	allowedGroups := toAllowlist(opts.AllowedGroupIDs)

	serverListen := strings.TrimSpace(opts.ServerListen)
	if serverListen != "" {
		if strings.TrimSpace(opts.ServerAuthToken) == "" {
			logger.Warn("line_daemon_server_auth_empty", "hint", "set server.auth_token so console can read /tasks")
		}
		_, err := daemonruntime.StartServer(ctx, logger, daemonruntime.ServerOptions{
			Listen: serverListen,
			Routes: daemonruntime.RoutesOptions{
				Mode:          "line",
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
				HealthEnabled: true,
			},
		})
		if err != nil {
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
			mu.Lock()
			h := append([]chathistory.ChatHistoryItem(nil), history[conversationKey]...)
			sticky := append([]string(nil), stickySkillsByConv[conversationKey]...)
			mu.Unlock()
			curVersion := runner.CurrentVersion(conversationKey)
			if job.Version != curVersion {
				h = nil
			}
			runtimecore.MarkTaskRunning(daemonStore, job.TaskID)
			runCtx, cancel := context.WithTimeout(workerCtx, taskTimeout)
			final, _, loadedSkills, runErr := runLineTask(
				runCtx,
				execRuntime,
				job,
				h,
				sticky,
				taskRuntimeOpts,
			)
			cancel()
			if runErr != nil {
				if workerCtx.Err() != nil {
					return
				}
				displayErr := depsutil.FormatRuntimeError(runErr)
				runtimecore.MarkTaskFailed(daemonStore, job.TaskID, displayErr, false)
				logger.Warn("line_task_error",
					"chat_id", job.ChatID,
					"message_id", job.MessageID,
					"error", displayErr,
				)
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
				outText = strings.TrimSpace(depsutil.FormatFinalOutput(final))
			}
			runtimecore.MarkTaskDone(daemonStore, job.TaskID, outText)
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
			if latestVersion != curVersion {
				history[conversationKey] = nil
				stickySkillsByConv[conversationKey] = nil
			}
			if latestVersion == curVersion && len(loadedSkills) > 0 {
				stickySkillsByConv[conversationKey] = capUniqueStrings(loadedSkills, lineStickySkillsCap)
			}
			cur := history[conversationKey]
			cur = append(cur, newLineInboundHistoryItem(job))
			if outText != "" {
				cur = append(cur, newLineOutboundAgentHistoryItem(job, outText, time.Now().UTC()))
			}
			history[conversationKey] = trimChatHistoryItems(cur, lineHistoryCapForMode(groupTriggerMode))
			mu.Unlock()
		},
	)
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
		if handledCommand, cmdErr := maybeHandleLineCommand(ctx, d, inprocBus, workspaceStore, msg.ConversationKey, inbound); handledCommand {
			return cmdErr
		}
		if strings.EqualFold(strings.TrimSpace(inbound.ChatType), "group") {
			mu.Lock()
			historySnapshot := append([]chathistory.ChatHistoryItem(nil), history[msg.ConversationKey]...)
			mu.Unlock()
			decisionCtx := llmstats.WithMetadata(context.Background(), lineTaskID(inbound.ChatID, inbound.MessageID), inbound.EventID)
			dec, accepted, decErr := decideLineGroupTrigger(
				decisionCtx,
				addressingClient,
				addressingModel,
				inbound,
				botUserID,
				groupTriggerMode,
				addressingLLMTimeout,
				addressingConfidenceThreshold,
				addressingInterjectThreshold,
				historySnapshot,
			)
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
		if inbound.ImagePending && opts.ImageRecognitionEnabled {
			if api == nil {
				logger.Warn("line_image_download_skip", "chat_id", inbound.ChatID, "message_id", inbound.MessageID, "reason", "api_not_initialized")
				return nil
			}
			imageCtx := ctx
			if imageCtx == nil {
				imageCtx = workersCtx
			}
			imageCtx, cancelImage := context.WithTimeout(imageCtx, lineImageDownloadTimeout)
			path, imageErr := downloadLineImageToCache(imageCtx, api, lineImageCacheDir, inbound.MessageID, lineLLMMaxImageBytes)
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
			inbound.ImagePaths = []string{path}
			inbound.ImagePending = false
		}
		workspaceDir, err := workspace.LookupWorkspaceDir(workspaceStore, msg.ConversationKey)
		if err != nil {
			return err
		}
		jobTaskID := lineTaskID(inbound.ChatID, inbound.MessageID)
		if err := runner.Enqueue(ctx, msg.ConversationKey, func(version uint64) lineJob {
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
				ImagePaths:      append([]string(nil), inbound.ImagePaths...),
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
			})
		}
		logger.Info("line_task_enqueued",
			"channel", msg.Channel,
			"topic", msg.Topic,
			"chat_id", inbound.ChatID,
			"chat_type", inbound.ChatType,
			"idempotency_key", msg.IdempotencyKey,
			"conversation_key", msg.ConversationKey,
			"text_len", len(text),
			"image_count", len(inbound.ImagePaths),
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
		ChannelSecret:           channelSecret,
		Inbound:                 lineInboundAdapter,
		AllowedGroups:           allowedGroups,
		Logger:                  logger,
		ImageRecognitionEnabled: opts.ImageRecognitionEnabled,
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
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = webhookServer.Shutdown(shutdownCtx)
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
		"image_recognition_enabled", opts.ImageRecognitionEnabled,
	)

	select {
	case err := <-webhookErrCh:
		if err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
		logger.Info("line_stop", "reason", "context_canceled")
		return nil
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
