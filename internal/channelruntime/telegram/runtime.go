package telegram

import (
	"context"
	"errors"
	"fmt"
	htmlstd "html"
	"log/slog"
	randv2 "math/rand/v2"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/guard"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	telegrambus "github.com/quailyquaily/mistermorph/internal/bus/adapters/telegram"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llminspect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/personautil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/internal/telegramutil"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/quailyquaily/mistermorph/llm"
	telegramtools "github.com/quailyquaily/mistermorph/tools/telegram"
)

type telegramJob struct {
	TaskID           string
	ConversationKey  string
	ChatID           int64
	MessageID        int64
	ReplyToMessageID int64
	SentAt           time.Time
	ChatType         string
	FromUserID       int64
	FromUsername     string
	FromFirstName    string
	FromLastName     string
	FromDisplayName  string
	Text             string
	ImagePaths       []string
	WorkspaceDir     string
	Version          uint64
	Meta             map[string]any
	MentionUsers     []string
}

type telegramPlanProgressLine struct {
	Text  string
	Emoji string
}

type telegramPlanProgressEditState struct {
	CorrelationID string
	MessageID     int64
	Lines         []telegramPlanProgressLine
}

func shouldRunInitFlow(initRequired bool, normalizedCmd string) bool {
	if !initRequired {
		return false
	}
	return strings.TrimSpace(normalizedCmd) == ""
}

func sendTelegramUnauthorizedMessage(api *telegramAPI, chatID int64, chatType string) {
	chatType = strings.TrimSpace(chatType)
	if chatType == "" {
		chatType = "unknown"
	}
	msg := fmt.Sprintf("You don't have permission to use this bot. Please contact the admin.\nchat_id: `%d`, type: `%s`", chatID, chatType)
	_ = api.sendMessageHTML(context.Background(), chatID, msg, true)
}

func shouldPublishTelegramText(final *agent.Final) bool {
	if final == nil {
		return true
	}
	return !final.IsLightweight
}

func runTelegramLoop(ctx context.Context, d Dependencies, opts runtimeLoopOptions) error {
	token := strings.TrimSpace(opts.BotToken)
	if token == "" {
		return fmt.Errorf("missing telegram.bot_token (set via --telegram-bot-token or MISTER_MORPH_TELEGRAM_BOT_TOKEN)")
	}

	baseURL := "https://api.telegram.org"

	allowed := make(map[int64]bool)
	for _, id := range normalizeAllowedChatIDs(opts.AllowedChatIDs) {
		if id == 0 {
			continue
		}
		allowed[id] = true
	}

	logger, err := depsutil.LoggerFromCommon(d.CommonDependencies)
	if err != nil {
		return err
	}
	hooks := opts.Hooks
	pollCtx := ctx
	if pollCtx == nil {
		pollCtx = context.Background()
	}
	slog.SetDefault(logger)

	daemonStore := opts.TaskStore
	if daemonStore == nil {
		daemonStore, err = daemonruntime.NewTaskViewForTarget("telegram", opts.Server.MaxQueue)
		if err != nil {
			return err
		}
	}

	inprocBus, err := busruntime.StartInproc(busruntime.BootstrapOptions{
		MaxInFlight: opts.BusMaxInFlight,
		Logger:      logger,
		Component:   "telegram",
	})
	if err != nil {
		return err
	}
	defer inprocBus.Close()

	contactsStore := contacts.NewFileStore(statepaths.ContactsDir())
	workspaceStore := workspace.NewStore(statepaths.WorkspaceAttachmentsPath())
	contactsSvc := contacts.NewService(contactsStore)

	var telegramInboundAdapter *telegrambus.InboundAdapter
	var telegramDeliveryAdapter *telegrambus.DeliveryAdapter
	var enqueueTelegramInbound func(context.Context, busruntime.BusMessage) error
	telegramInboundAdapter, err = telegrambus.NewInboundAdapter(telegrambus.InboundAdapterOptions{
		Bus:   inprocBus,
		Store: contactsStore,
	})
	if err != nil {
		return err
	}

	busHandler := func(ctx context.Context, msg busruntime.BusMessage) error {
		switch msg.Direction {
		case busruntime.DirectionInbound:
			if msg.Channel == busruntime.ChannelTelegram {
				if err := contactsSvc.ObserveInboundBusMessage(context.Background(), msg, time.Now().UTC()); err != nil {
					logger.Warn("contacts_observe_bus_error", "channel", msg.Channel, "idempotency_key", msg.IdempotencyKey, "error", err.Error())
				}
			}
			switch msg.Channel {
			case busruntime.ChannelTelegram:
				if enqueueTelegramInbound == nil {
					return fmt.Errorf("telegram inbound handler is not initialized")
				}
				return enqueueTelegramInbound(ctx, msg)
			default:
				return fmt.Errorf("unsupported inbound channel: %s", msg.Channel)
			}
		case busruntime.DirectionOutbound:
			switch msg.Channel {
			case busruntime.ChannelTelegram:
				if telegramDeliveryAdapter == nil {
					return fmt.Errorf("telegram delivery adapter is not initialized")
				}
				_, _, err := telegramDeliveryAdapter.Deliver(ctx, msg)
				if err != nil {
					chatID, _ := telegramChatIDFromConversationKey(msg.ConversationKey)
					callErrorHook(ctx, logger, hooks, ErrorEvent{
						Stage:  ErrorStageDeliverOutbound,
						ChatID: chatID,
						Err:    err,
					})
					return err
				}
				event, eventErr := telegramOutboundEventFromBusMessage(msg)
				if eventErr != nil {
					callErrorHook(ctx, logger, hooks, ErrorEvent{
						Stage:  ErrorStageDeliverOutbound,
						ChatID: event.ChatID,
						Err:    eventErr,
					})
				} else {
					callOutboundHook(ctx, logger, hooks, event)
				}
				return nil
			default:
				return fmt.Errorf("unsupported outbound channel: %s", msg.Channel)
			}
		default:
			return fmt.Errorf("unsupported direction: %s", msg.Direction)
		}
	}
	for _, topic := range busruntime.AllTopics() {
		if err := inprocBus.Subscribe(topic, busHandler); err != nil {
			return err
		}
	}

	requestTimeout := opts.RequestTimeout
	var requestInspector *llminspect.RequestInspector
	if opts.InspectRequest {
		requestInspector, err = llminspect.NewRequestInspector(llminspect.Options{
			Mode:            "telegram",
			Task:            "telegram",
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
			Mode:            "telegram",
			Task:            "telegram",
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
		memRuntime.ProjectionWorker.Start(pollCtx)
	}
	defer memRuntime.Cleanup()
	taskRuntimeOpts := runtimeTaskOptions{
		MemoryEnabled:           opts.MemoryEnabled,
		MemoryInjectionEnabled:  opts.MemoryInjectionEnabled,
		MemoryInjectionMaxItems: opts.MemoryInjectionMaxItems,
		ImageRecognitionEnabled: opts.ImageRecognitionEnabled,
		MemoryOrchestrator:      memRuntime.Orchestrator,
		MemoryProjectionWorker:  memRuntime.ProjectionWorker,
	}
	pollTimeout := opts.PollTimeout
	taskTimeout := opts.TaskTimeout
	maxConc := opts.MaxConcurrency
	sem := make(chan struct{}, maxConc)
	workersCtx, stopWorkers := context.WithCancel(pollCtx)
	defer stopWorkers()
	serverListen := strings.TrimSpace(opts.Server.Listen)
	if serverListen != "" {
		if strings.TrimSpace(opts.Server.AuthToken) == "" {
			logger.Warn("telegram_daemon_server_auth_empty", "hint", "set server.auth_token so console can read /tasks")
		}
		_, err := daemonruntime.StartServer(pollCtx, logger, daemonruntime.ServerOptions{
			Listen: serverListen,
			Routes: daemonruntime.RoutesOptions{
				Mode:          "telegram",
				AgentNameFunc: func() string { return personautil.LoadAgentName(statepaths.FileStateDir()) },
				AuthToken:     strings.TrimSpace(opts.Server.AuthToken),
				TaskReader:    daemonStore,
				Overview: func(ctx context.Context) (map[string]any, error) {
					return map[string]any{
						"llm": map[string]any{
							"provider": strings.TrimSpace(mainRoute.ClientConfig.Provider),
							"model":    model,
						},
						"channel": map[string]any{
							"configured":          true,
							"telegram_configured": true,
							"slack_configured":    false,
							"running":             "telegram",
							"telegram_running":    true,
							"slack_running":       false,
						},
					}, nil
				},
				Poke:          opts.Server.Poke,
				HealthEnabled: true,
			},
		})
		if err != nil {
			logger.Warn("telegram_daemon_server_start_error", "addr", serverListen, "error", err.Error())
		}
	}

	httpClient := &http.Client{Timeout: 60 * time.Second}
	api := newTelegramAPI(httpClient, baseURL, token)
	var (
		planProgressEditMu    sync.Mutex
		planProgressStateByID = make(map[int64]telegramPlanProgressEditState)
	)
	parseSendTextInput := func(target any, opts telegrambus.SendTextOptions) (int64, int64, string, error) {
		chatID, ok := target.(int64)
		if !ok {
			return 0, 0, "", fmt.Errorf("telegram target is invalid")
		}
		replyToMessageID := int64(0)
		replyToRaw := strings.TrimSpace(opts.ReplyTo)
		if replyToRaw != "" {
			parsed, parseErr := strconv.ParseInt(replyToRaw, 10, 64)
			if parseErr != nil || parsed <= 0 {
				return 0, 0, "", fmt.Errorf("telegram reply_to is invalid")
			}
			replyToMessageID = parsed
		}
		return chatID, replyToMessageID, strings.TrimSpace(opts.CorrelationID), nil
	}
	sendPlanProgress := func(ctx context.Context, chatID int64, text string, replyToMessageID int64, correlationID string) error {
		line := strings.TrimSpace(text)
		if line == "" {
			return nil
		}
		var state telegramPlanProgressEditState
		planProgressEditMu.Lock()
		state = planProgressStateByID[chatID]
		planProgressEditMu.Unlock()

		nextState, rendered := nextTelegramPlanProgressState(state, correlationID, line)
		if rendered == "" {
			return nil
		}
		if nextState.MessageID > 0 && strings.EqualFold(nextState.CorrelationID, correlationID) {
			if err := api.editMessageHTML(ctx, chatID, nextState.MessageID, rendered, true); err == nil || isTelegramMessageNotModified(err) {
				planProgressEditMu.Lock()
				planProgressStateByID[chatID] = nextState
				planProgressEditMu.Unlock()
				return nil
			} else {
				logger.Warn("telegram_plan_progress_edit_failed", "chat_id", chatID, "message_id", nextState.MessageID, "correlation_id", correlationID, "error", err.Error())
			}
		}
		messageID, err := api.sendMessageChunkedReplyWithFirstMessageID(ctx, chatID, rendered, replyToMessageID)
		if err != nil {
			return err
		}
		if messageID > 0 && correlationID != "" {
			nextState.MessageID = messageID
			planProgressEditMu.Lock()
			planProgressStateByID[chatID] = nextState
			planProgressEditMu.Unlock()
		}
		return nil
	}
	telegramDeliveryAdapter, err = telegrambus.NewDeliveryAdapter(telegrambus.DeliveryAdapterOptions{
		SendText: func(ctx context.Context, target any, text string, opts telegrambus.SendTextOptions) error {
			chatID, replyToMessageID, correlationID, err := parseSendTextInput(target, opts)
			if err != nil {
				return err
			}
			kind := telegramOutboundKind(correlationID)
			if kind == "plan_progress" {
				return sendPlanProgress(ctx, chatID, text, replyToMessageID, correlationID)
			}
			return api.sendMessageChunkedReply(ctx, chatID, text, replyToMessageID)
		},
	})
	if err != nil {
		return err
	}
	publishTelegramText := func(ctx context.Context, chatID int64, text string, correlationID string) error {
		replyTo := ""
		_, err := publishTelegramBusOutbound(ctx, inprocBus, chatID, text, replyTo, correlationID)
		if err != nil {
			callErrorHook(ctx, logger, hooks, ErrorEvent{
				Stage:  ErrorStagePublishOutbound,
				ChatID: chatID,
				Err:    err,
			})
			return err
		}
		return nil
	}

	fileCacheDir := strings.TrimSpace(opts.FileCacheDir)
	const filesMaxBytes = int64(20 * 1024 * 1024)
	if err := telegramutil.EnsureSecureCacheDir(fileCacheDir); err != nil {
		return fmt.Errorf("telegram file cache dir: %w", err)
	}
	telegramCacheDir := filepath.Join(fileCacheDir, "telegram")
	if err := ensureSecureChildDir(fileCacheDir, telegramCacheDir); err != nil {
		return fmt.Errorf("telegram cache subdir: %w", err)
	}
	maxAge := opts.FileCacheMaxAge
	maxFiles := opts.FileCacheMaxFiles
	maxTotalBytes := opts.FileCacheMaxTotalBytes
	if err := telegramutil.CleanupFileCacheDir(telegramCacheDir, maxAge, maxFiles, maxTotalBytes); err != nil {
		logger.Warn("file_cache_cleanup_error", "error", err.Error())
	}

	var me *telegramUser
	for {
		me, err = api.getMe(pollCtx)
		if err == nil {
			break
		}
		if errors.Is(err, context.Canceled) || pollCtx.Err() != nil {
			logger.Info("telegram_stop", "reason", "context_canceled")
			return nil
		}
		logger.Warn("telegram_get_me_error", "error", err.Error())
		select {
		case <-pollCtx.Done():
			logger.Info("telegram_stop", "reason", "context_canceled")
			return nil
		case <-time.After(2 * time.Second):
		}
	}

	botUser := me.Username
	botID := me.ID
	groupTriggerMode := strings.ToLower(strings.TrimSpace(opts.GroupTriggerMode))
	telegramHistoryCap := telegramHistoryCapForMode(groupTriggerMode)
	addressingLLMTimeout := addressingRoute.ClientConfig.RequestTimeout
	if addressingLLMTimeout <= 0 {
		addressingLLMTimeout = requestTimeout
	}
	addressingConfidenceThreshold := opts.AddressingConfidenceThreshold
	addressingInterjectThreshold := opts.AddressingInterjectThreshold

	var (
		mu                 sync.Mutex
		history            = make(map[int64][]chathistory.ChatHistoryItem)
		initSessions       = make(map[int64]telegramInitSession)
		stickySkillsByChat = make(map[int64][]string)
		lastActivity       = make(map[int64]time.Time)
		lastFromUser       = make(map[int64]int64)
		lastFromUsername   = make(map[int64]string)
		lastFromName       = make(map[int64]string)
		lastFromFirst      = make(map[int64]string)
		lastFromLast       = make(map[int64]string)
		lastChatType       = make(map[int64]string)
		knownMentions      = make(map[int64]map[string]string)
		offset             int64
	)
	initRequired := false
	if _, err := loadInitProfileDraft(); err == nil {
		initRequired = true
		logger.Info("telegram_init_pending", "reason", "IDENTITY.md and SOUL.md are draft")
	} else if !errors.Is(err, errInitProfilesNotDraft) {
		logger.Warn("telegram_init_check_error", "error", err.Error())
	}
	var sharedGuard *guard.Guard

	var (
		warningsMu                sync.Mutex
		systemWarnings            []string
		systemWarningsSeen        = make(map[string]bool)
		systemWarningsVersion     int
		systemWarningsSentVersion = make(map[int64]int)
	)

	logger.Info("telegram_start",
		"base_url", baseURL,
		"bot_username", botUser,
		"bot_id", botID,
		"poll_timeout", pollTimeout.String(),
		"task_timeout", taskTimeout.String(),
		"max_concurrency", maxConc,
		"telegram_history_mode_cap_talkative", 16,
		"telegram_history_mode_cap_others", 8,
		"reactions_enabled", true,
		"group_trigger_mode", groupTriggerMode,
		"group_reply_policy", "humanlike",
		"addressing_confidence_threshold", addressingConfidenceThreshold,
		"addressing_interject_threshold", addressingInterjectThreshold,
		"telegram_history_cap", telegramHistoryCap,
	)

	enqueueSystemWarning := func(msg string) int {
		msg = strings.TrimSpace(msg)
		if msg == "" {
			return systemWarningsVersion
		}
		warningsMu.Lock()
		defer warningsMu.Unlock()
		key := strings.ToLower(msg)
		if systemWarningsSeen[key] {
			return systemWarningsVersion
		}
		systemWarningsSeen[key] = true
		systemWarnings = append(systemWarnings, msg)
		systemWarningsVersion++
		return systemWarningsVersion
	}

	systemWarningsSnapshot := func() (string, int) {
		warningsMu.Lock()
		defer warningsMu.Unlock()
		if len(systemWarnings) == 0 {
			return "", 0
		}
		return strings.Join(systemWarnings, "\n"), systemWarningsVersion
	}

	markSystemWarningsSent := func(chatID int64, version int) {
		warningsMu.Lock()
		defer warningsMu.Unlock()
		if systemWarningsSentVersion[chatID] < version {
			systemWarningsSentVersion[chatID] = version
		}
	}

	sendSystemWarnings := func(chatID int64) {
		if len(allowed) > 0 && !allowed[chatID] {
			return
		}
		msg, version := systemWarningsSnapshot()
		if version == 0 {
			return
		}
		warningsMu.Lock()
		sentVersion := systemWarningsSentVersion[chatID]
		warningsMu.Unlock()
		if sentVersion >= version {
			return
		}
		_ = api.sendMessageHTML(context.Background(), chatID, msg, true)
		markSystemWarningsSent(chatID, version)
	}

	broadcastSystemWarnings := func() {
		msg, version := systemWarningsSnapshot()
		if version == 0 {
			return
		}
		mu.Lock()
		chatIDs := make([]int64, 0, len(lastActivity))
		for chatID := range lastActivity {
			chatIDs = append(chatIDs, chatID)
		}
		mu.Unlock()
		for _, chatID := range chatIDs {
			if len(allowed) > 0 && !allowed[chatID] {
				continue
			}
			warningsMu.Lock()
			sentVersion := systemWarningsSentVersion[chatID]
			warningsMu.Unlock()
			if sentVersion >= version {
				continue
			}
			_ = api.sendMessageHTML(context.Background(), chatID, msg, true)
			markSystemWarningsSent(chatID, version)
		}
	}

	sharedGuard = depsutil.GuardFromCommon(d.CommonDependencies, logger)
	if sharedGuard != nil {
		for _, warn := range sharedGuard.Warnings() {
			enqueueSystemWarning(warn)
		}
		broadcastSystemWarnings()
	}

	var runner *runtimecore.ConversationRunner[int64, telegramJob]
	runner = runtimecore.NewConversationRunner[int64, telegramJob](
		workersCtx,
		sem,
		16,
		func(workerCtx context.Context, chatID int64, job telegramJob) {
			mu.Lock()
			h := append([]chathistory.ChatHistoryItem(nil), history[chatID]...)
			sticky := append([]string(nil), stickySkillsByChat[chatID]...)
			mu.Unlock()
			curVersion := runner.CurrentVersion(chatID)

			// If there was a /reset after this job was queued, drop history for this run.
			if job.Version != curVersion {
				h = nil
			}

			typingStop := startTypingTicker(workerCtx, api, chatID, "typing", 4*time.Second)
			defer typingStop()
			runtimecore.MarkTaskRunning(daemonStore, job.TaskID)

			runCtx, cancel := context.WithTimeout(workerCtx, taskTimeout)
			final, _, loadedSkills, reaction, runErr := runTelegramTask(runCtx, execRuntime, api, fileCacheDir, filesMaxBytes, allowed, job, botUser, h, telegramHistoryCap, sticky, requestTimeout, taskRuntimeOpts, publishTelegramText)
			cancel()

			if runErr != nil {
				if workerCtx.Err() != nil {
					return
				}
				displayErr := depsutil.FormatRuntimeError(runErr)
				runtimecore.MarkTaskFailed(daemonStore, job.TaskID, displayErr, isTaskContextCanceled(runErr))
				callErrorHook(workerCtx, logger, hooks, ErrorEvent{
					Stage:     ErrorStageRunTask,
					ChatID:    chatID,
					MessageID: job.MessageID,
					Err:       runErr,
				})
				errorCorrelationID := fmt.Sprintf("telegram:error:%d:%d", chatID, job.MessageID)
				if _, err := publishTelegramBusOutbound(workerCtx, inprocBus, chatID, "error: "+displayErr, "", errorCorrelationID); err != nil {
					logger.Warn("telegram_bus_publish_error", "channel", busruntime.ChannelTelegram, "chat_id", chatID, "bus_error_code", busErrorCodeString(err), "error", err.Error())
					callErrorHook(workerCtx, logger, hooks, ErrorEvent{
						Stage:     ErrorStagePublishErrorReply,
						ChatID:    chatID,
						MessageID: job.MessageID,
						Err:       err,
					})
				}
				return
			}

			outText := depsutil.FormatFinalOutput(final)
			publishText := shouldPublishTelegramText(final)
			runtimecore.MarkTaskDone(daemonStore, job.TaskID, outText)
			if publishText {
				outCorrelationID := fmt.Sprintf("telegram:message:%d:%d", chatID, job.MessageID)
				if workerCtx.Err() != nil {
					return
				}
				replyTo := ""
				if job.ReplyToMessageID > 0 {
					replyTo = strconv.FormatInt(job.ReplyToMessageID, 10)
				}
				if _, err := publishTelegramBusOutbound(workerCtx, inprocBus, chatID, outText, replyTo, outCorrelationID); err != nil {
					logger.Warn("telegram_bus_publish_error", "channel", busruntime.ChannelTelegram, "chat_id", chatID, "bus_error_code", busErrorCodeString(err), "error", err.Error())
					callErrorHook(workerCtx, logger, hooks, ErrorEvent{
						Stage:     ErrorStagePublishOutbound,
						ChatID:    chatID,
						MessageID: job.MessageID,
						Err:       err,
					})
				}
			}
			mu.Lock()
			// Respect resets that happened while the task was running.
			latestVersion := runner.CurrentVersion(chatID)
			if latestVersion != curVersion {
				history[chatID] = nil
				stickySkillsByChat[chatID] = nil
			}
			if latestVersion == curVersion && len(loadedSkills) > 0 {
				stickySkillsByChat[chatID] = capUniqueStrings(loadedSkills, telegramStickySkillsCap)
			}
			cur := history[chatID]
			cur = append(cur, newTelegramInboundHistoryItem(job))
			if reaction != nil {
				note := "[reacted]"
				if emoji := strings.TrimSpace(reaction.Emoji); emoji != "" {
					note = "[reacted: " + emoji + "]"
				}
				cur = append(cur, newTelegramOutboundReactionHistoryItem(chatID, job.ChatType, note, reaction.Emoji, time.Now().UTC(), botUser))
			}
			if publishText {
				cur = append(cur, newTelegramOutboundAgentHistoryItem(chatID, job.ChatType, outText, time.Now().UTC(), botUser))
			}
			history[chatID] = trimChatHistoryItems(cur, telegramHistoryCap)
			mu.Unlock()
		},
	)

	enqueueTelegramInbound = func(ctx context.Context, msg busruntime.BusMessage) error {
		if ctx == nil {
			ctx = workersCtx
		}
		inbound, err := telegrambus.InboundMessageFromBusMessage(msg)
		if err != nil {
			return err
		}
		text := strings.TrimSpace(inbound.Text)
		if text == "" {
			return fmt.Errorf("telegram inbound text is required")
		}
		mu.Lock()
		lastActivity[inbound.ChatID] = time.Now()
		if inbound.FromUserID > 0 {
			lastFromUser[inbound.ChatID] = inbound.FromUserID
			if inbound.FromUsername != "" {
				lastFromUsername[inbound.ChatID] = inbound.FromUsername
			}
			if inbound.FromDisplayName != "" {
				lastFromName[inbound.ChatID] = inbound.FromDisplayName
			}
			if inbound.FromFirstName != "" {
				lastFromFirst[inbound.ChatID] = inbound.FromFirstName
			}
			if inbound.FromLastName != "" {
				lastFromLast[inbound.ChatID] = inbound.FromLastName
			}
		}
		if inbound.ChatType != "" {
			lastChatType[inbound.ChatID] = inbound.ChatType
		}
		mu.Unlock()

		logger.Info("telegram_task_enqueued",
			"channel", msg.Channel,
			"topic", msg.Topic,
			"chat_id", inbound.ChatID,
			"type", inbound.ChatType,
			"idempotency_key", msg.IdempotencyKey,
			"conversation_key", msg.ConversationKey,
			"text_len", len(text),
			"image_count", len(inbound.ImagePaths),
		)
		workspaceDir, err := workspace.LookupWorkspaceDir(workspaceStore, msg.ConversationKey)
		if err != nil {
			return err
		}
		jobTaskID := telegramTaskID(inbound.ChatID, inbound.MessageID)
		if err := runner.Enqueue(ctx, inbound.ChatID, func(version uint64) telegramJob {
			return telegramJob{
				TaskID:           jobTaskID,
				ConversationKey:  msg.ConversationKey,
				ChatID:           inbound.ChatID,
				MessageID:        inbound.MessageID,
				ReplyToMessageID: inbound.ReplyToMessageID,
				SentAt:           inbound.SentAt,
				ChatType:         inbound.ChatType,
				FromUserID:       inbound.FromUserID,
				FromUsername:     inbound.FromUsername,
				FromFirstName:    inbound.FromFirstName,
				FromLastName:     inbound.FromLastName,
				FromDisplayName:  inbound.FromDisplayName,
				Text:             text,
				ImagePaths:       append([]string(nil), inbound.ImagePaths...),
				WorkspaceDir:     workspaceDir,
				Version:          version,
				MentionUsers:     append([]string(nil), inbound.MentionUsers...),
			}
		}); err != nil {
			return err
		}
		if daemonStore != nil {
			createdAt := inbound.SentAt.UTC()
			if createdAt.IsZero() {
				createdAt = time.Now().UTC()
			}
			topicID, topicTitle := telegramManagedTopicInfo(inbound.ChatID, inbound.ChatType, inbound.FromDisplayName, inbound.FromUsername)
			recordTelegramQueuedTask(daemonStore, daemonruntime.TaskInfo{
				ID:        jobTaskID,
				Status:    daemonruntime.TaskQueued,
				Task:      daemonruntime.TruncateUTF8(text, 2000),
				Model:     strings.TrimSpace(model),
				Timeout:   taskTimeout.String(),
				CreatedAt: createdAt,
				TopicID:   topicID,
				Result: map[string]any{
					"source":                "telegram",
					"telegram_chat_id":      inbound.ChatID,
					"telegram_message_id":   inbound.MessageID,
					"telegram_reply_to":     inbound.ReplyToMessageID,
					"telegram_chat_type":    strings.TrimSpace(inbound.ChatType),
					"telegram_from_user_id": inbound.FromUserID,
					"telegram_from_name":    strings.TrimSpace(inbound.FromDisplayName),
					"mention_users":         append([]string(nil), inbound.MentionUsers...),
				},
			}, daemonruntime.TaskTrigger{
				Source: "system",
				Event:  "poll_inbound",
				Ref:    fmt.Sprintf("telegram/%d/%d", inbound.ChatID, inbound.MessageID),
			}, topicTitle)
		}
		callInboundHook(ctx, logger, hooks, InboundEvent{
			ChatID:       inbound.ChatID,
			MessageID:    inbound.MessageID,
			ChatType:     inbound.ChatType,
			FromUserID:   inbound.FromUserID,
			Text:         text,
			MentionUsers: append([]string(nil), inbound.MentionUsers...),
		})
		return nil
	}

	for {
		updates, nextOffset, err := api.getUpdates(pollCtx, offset, pollTimeout)
		if err != nil {
			if errors.Is(err, context.Canceled) || pollCtx.Err() != nil {
				logger.Info("telegram_stop", "reason", "context_canceled")
				return nil
			}
			if isTelegramPollTimeoutError(err) {
				logger.Debug("telegram_get_updates_timeout", "error", err.Error())
			} else {
				logger.Warn("telegram_get_updates_error", "error", err.Error())
			}
			time.Sleep(1 * time.Second)
			continue
		}
		offset = nextOffset

		for _, u := range updates {
			msg := u.Message
			if msg == nil {
				msg = u.EditedMessage
			}
			if msg == nil {
				msg = u.ChannelPost
			}
			if msg == nil {
				msg = u.EditedChannelPost
			}
			if msg == nil || msg.Chat == nil {
				continue
			}
			chatID := msg.Chat.ID
			text := strings.TrimSpace(messageTextOrCaption(msg))
			rawText := text

			fromUserID := int64(0)
			fromUsername := ""
			fromFirst := ""
			fromLast := ""
			fromDisplay := ""
			if msg.From != nil && !msg.From.IsBot {
				fromUserID = msg.From.ID
				fromUsername = strings.TrimSpace(msg.From.Username)
				fromFirst = strings.TrimSpace(msg.From.FirstName)
				fromLast = strings.TrimSpace(msg.From.LastName)
				fromDisplay = telegramDisplayName(msg.From)
			}

			chatType := strings.ToLower(strings.TrimSpace(msg.Chat.Type))
			isGroup := chatType == "group" || chatType == "supergroup"
			messageSentAt := telegramMessageSentAt(msg)
			sendSystemWarnings(chatID)

			var mentionCandidates []string
			if isGroup {
				mentionCandidates = collectMentionCandidates(msg, botUser)
				if len(mentionCandidates) > 0 {
					mu.Lock()
					addKnownUsernames(knownMentions, chatID, mentionCandidates)
					mu.Unlock()
				}
			}
			appendIgnoredInboundHistory := func(ignoredText string) {
				ignoredText = strings.TrimSpace(ignoredText)
				if ignoredText == "" && messageHasDownloadableFile(msg) {
					ignoredText = "[attachment]"
				}
				if msg.ReplyTo != nil {
					if quoted := buildReplyContext(msg.ReplyTo); quoted != "" {
						if ignoredText == "" {
							ignoredText = "(empty)"
						}
						ignoredText = "Quoted message:\n> " + quoted + "\n\nUser request:\n" + ignoredText
					}
				}
				mu.Lock()
				cur := history[chatID]
				cur = append(cur, newTelegramInboundHistoryItem(telegramJob{
					ChatID:          chatID,
					MessageID:       msg.MessageID,
					SentAt:          messageSentAt,
					ChatType:        chatType,
					FromUserID:      fromUserID,
					FromUsername:    fromUsername,
					FromFirstName:   fromFirst,
					FromLastName:    fromLast,
					FromDisplayName: fromDisplay,
					Text:            ignoredText,
				}))
				history[chatID] = trimChatHistoryItems(cur, telegramHistoryCap)
				mu.Unlock()
			}

			cmdWord, cmdArgs := chatcommands.ParseCommand(text)
			normalizedCmd := chatcommands.NormalizeCommand(cmdWord)
			messageRunID := ""
			if msg != nil && msg.MessageID > 0 {
				messageRunID = telegramTaskID(chatID, msg.MessageID)
			}
			withMessageRunID := func(runCtx context.Context) context.Context {
				return llmstats.WithRunID(runCtx, messageRunID)
			}
			newMessageTimeoutCtx := func() (context.Context, context.CancelFunc) {
				runCtx, cancel := context.WithTimeout(context.Background(), initFlowTimeout(requestTimeout))
				return withMessageRunID(runCtx), cancel
			}
			if shouldRunInitFlow(initRequired, normalizedCmd) {
				if len(allowed) > 0 && !allowed[chatID] {
					logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
					sendTelegramUnauthorizedMessage(api, chatID, chatType)
					continue
				}
				if strings.ToLower(strings.TrimSpace(chatType)) != "private" {
					_ = api.sendMessageHTML(context.Background(), chatID, "initialization is pending; please DM me first to finish setup", true)
					continue
				}
				mu.Lock()
				initSession, hasInitSession := initSessions[chatID]
				mu.Unlock()
				if !hasInitSession {
					draft, err := loadInitProfileDraft()
					if err != nil {
						if errors.Is(err, errInitProfilesNotDraft) {
							initRequired = false
						} else {
							_ = api.sendMessageHTML(context.Background(), chatID, "init failed: "+err.Error(), true)
							continue
						}
					} else {
						typingStop := startTypingTicker(context.Background(), api, chatID, "typing", 4*time.Second)
						initCtx, cancel := newMessageTimeoutCtx()
						mainClient, mainModel, cleanupMain, mainErr := resolveTelegramMainForUse(execRuntime)
						if mainErr != nil {
							cancel()
							typingStop()
							_ = api.sendMessageHTML(context.Background(), chatID, "init failed: "+mainErr.Error(), true)
							continue
						}
						questions, questionMsg, err := buildInitQuestions(initCtx, mainClient, mainModel, draft, text)
						cleanupMain()
						cancel()
						typingStop()
						if err != nil {
							logger.Warn("telegram_init_question_error", "error", err.Error())
						}
						if len(questions) == 0 {
							questions = defaultInitQuestions(text)
						}
						if strings.TrimSpace(questionMsg) == "" {
							questionMsg = fallbackInitQuestionMessage(questions, text)
						}
						mu.Lock()
						initSessions[chatID] = telegramInitSession{
							Questions: questions,
							StartedAt: time.Now().UTC(),
						}
						mu.Unlock()
						_ = api.sendMessageHTML(context.Background(), chatID, questionMsg, true)
						continue
					}
				}
				if hasInitSession {
					if strings.TrimSpace(text) == "" {
						_ = api.sendMessageHTML(context.Background(), chatID, "please answer the init questions in one message", true)
						continue
					}
					draft, err := loadInitProfileDraft()
					if err != nil {
						if errors.Is(err, errInitProfilesNotDraft) {
							initRequired = false
							mu.Lock()
							for k := range initSessions {
								delete(initSessions, k)
							}
							mu.Unlock()
						} else {
							_ = api.sendMessageHTML(context.Background(), chatID, "init failed: "+err.Error(), true)
							continue
						}
					} else {
						typingStop := startTypingTicker(context.Background(), api, chatID, "typing", 4*time.Second)
						initCtx, cancel := newMessageTimeoutCtx()
						mainClient, mainModel, cleanupMain, mainErr := resolveTelegramMainForUse(execRuntime)
						if mainErr != nil {
							cancel()
							typingStop()
							_ = api.sendMessageHTML(context.Background(), chatID, "init failed: "+mainErr.Error(), true)
							continue
						}
						applyResult, err := applyInitFromAnswer(initCtx, mainClient, mainModel, draft, initSession, text, fromUsername, fromDisplay)
						cleanupMain()
						cancel()
						typingStop()
						if err != nil {
							_ = api.sendMessageHTML(context.Background(), chatID, "init failed: "+err.Error(), true)
							continue
						}
						mu.Lock()
						initRequired = false
						for k := range initSessions {
							delete(initSessions, k)
						}
						mu.Unlock()
						typingStop2 := startTypingTicker(context.Background(), api, chatID, "typing", 4*time.Second)
						greetCtx, greetCancel := newMessageTimeoutCtx()
						mainClient, mainModel, cleanupMain, mainErr = resolveTelegramMainForUse(execRuntime)
						if mainErr != nil {
							greetCancel()
							typingStop2()
							_ = api.sendMessageHTML(context.Background(), chatID, "init applied, but greeting failed: "+mainErr.Error(), true)
							continue
						}
						greeting, greetErr := generatePostInitGreeting(greetCtx, mainClient, mainModel, draft, initSession, text, applyResult)
						cleanupMain()
						greetCancel()
						typingStop2()
						if greetErr != nil {
							logger.Warn("telegram_init_greeting_error", "error", greetErr.Error())
						}
						_ = api.sendMessageHTML(context.Background(), chatID, greeting, true)
						continue
					}
				}
			}
			replyToMessageID := int64(0)
			switch normalizedCmd {
			case "/start", "/help":
				help := "Send a message and I will run it as an agent task.\n" +
					"Commands: /echo <msg>, /humanize, /model, /workspace, /reset, /id\n\n" +
					"Group chats: reply to me, or mention @" + botUser + ".\n" +
					"You can also send a file (document/photo). It will be downloaded under file_cache_dir/telegram/ and the agent can process it.\n" +
					"Note: if Bot Privacy Mode is enabled, I may not receive normal group messages."
				_ = api.sendMessageHTML(context.Background(), chatID, help, true)
				continue
			case "/model":
				if len(allowed) > 0 && !allowed[chatID] {
					logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
					sendTelegramUnauthorizedMessage(api, chatID, chatType)
					continue
				}
				if executeTelegramProfileCommand(d, api, chatID, text) {
					continue
				}
				_ = api.sendMessageHTML(context.Background(), chatID, "error: "+htmlstd.EscapeString("missing llm profile command handler"), true)
				continue
			case "/id":
				_ = api.sendMessageHTML(context.Background(), chatID, fmt.Sprintf("chat_id=%d type=%s", chatID, chatType), true)
				continue
			case "/workspace":
				if len(allowed) > 0 && !allowed[chatID] {
					logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
					sendTelegramUnauthorizedMessage(api, chatID, chatType)
					continue
				}
				conversationKey, convErr := busruntime.BuildTelegramChatConversationKey(strconv.FormatInt(chatID, 10))
				if convErr != nil {
					_ = api.sendMessageHTML(context.Background(), chatID, "error: "+htmlstd.EscapeString(convErr.Error()), true)
					continue
				}
				result, cmdErr := workspace.ExecuteStoreCommand(workspaceStore, conversationKey, cmdArgs, nil)
				reply := result.Reply
				if cmdErr != nil {
					reply = "error: " + strings.TrimSpace(cmdErr.Error())
				}
				_ = api.sendMessageHTML(context.Background(), chatID, htmlstd.EscapeString(reply), true)
				continue
			case "/humanize":
				if len(allowed) > 0 && !allowed[chatID] {
					logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
					sendTelegramUnauthorizedMessage(api, chatID, chatType)
					continue
				}
				if strings.ToLower(strings.TrimSpace(chatType)) != "private" {
					_ = api.sendMessageHTML(context.Background(), chatID, "please use /humanize in the private chat", true)
					continue
				}
				typingStop := startTypingTicker(context.Background(), api, chatID, "typing", 4*time.Second)
				humanizeCtx, cancel := newMessageTimeoutCtx()
				mainClient, mainModel, cleanupMain, mainErr := resolveTelegramMainForUse(execRuntime)
				if mainErr != nil {
					cancel()
					typingStop()
					_ = api.sendMessageHTML(context.Background(), chatID, "humanize failed: "+mainErr.Error(), true)
					continue
				}
				updated, err := humanizeSoulProfile(humanizeCtx, mainClient, mainModel)
				cleanupMain()
				cancel()
				typingStop()
				if err != nil {
					_ = api.sendMessageHTML(context.Background(), chatID, "humanize failed: "+err.Error(), true)
					continue
				}
				if updated {
					_ = api.sendMessageHTML(context.Background(), chatID, "ok (SOUL.md humanized)", true)
				} else {
					_ = api.sendMessageHTML(context.Background(), chatID, "ok (SOUL.md unchanged)", true)
				}
				continue
			case "/reset":
				if len(allowed) > 0 && !allowed[chatID] {
					logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
					sendTelegramUnauthorizedMessage(api, chatID, chatType)
					continue
				}
				mu.Lock()
				delete(history, chatID)
				delete(stickySkillsByChat, chatID)
				delete(knownMentions, chatID)
				delete(initSessions, chatID)
				runner.IncrementVersion(chatID)
				mu.Unlock()
				planProgressEditMu.Lock()
				delete(planProgressStateByID, chatID)
				planProgressEditMu.Unlock()
				_ = api.sendMessageHTML(context.Background(), chatID, "ok (reset)", true)
				continue
			case "/echo":
				if len(allowed) > 0 && !allowed[chatID] {
					logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
					sendTelegramUnauthorizedMessage(api, chatID, chatType)
					continue
				}
				msg := strings.TrimSpace(cmdArgs)
				if msg == "" {
					_ = api.sendMessageHTML(context.Background(), chatID, "usage: /echo <msg>", true)
					continue
				}
				_ = api.sendMessageHTML(context.Background(), chatID, msg, true)
				continue
			default:
				if len(allowed) > 0 && !allowed[chatID] {
					logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
					sendTelegramUnauthorizedMessage(api, chatID, chatType)
					continue
				}
				if isGroup {
					if shouldSkipGroupReplyWithoutBodyMention(msg, text, botUser, botID) {
						logger.Info("telegram_group_ignored_reply_without_at_mention",
							"chat_id", chatID,
							"type", chatType,
							"text_len", len(text),
						)
						appendIgnoredInboundHistory(rawText)
						continue
					}
					mu.Lock()
					historySnapshot := append([]chathistory.ChatHistoryItem(nil), history[chatID]...)
					mu.Unlock()
					var addressingReactionTool *telegramtools.ReactTool
					if api != nil && msg != nil && msg.MessageID > 0 {
						addressingReactionTool = telegramtools.NewReactTool(newTelegramToolAPI(api), chatID, msg.MessageID, allowed)
					}
					decisionCtx := withMessageRunID(context.Background())
					dec, ok, decErr := groupTriggerDecision(decisionCtx, addressingClient, addressingModel, msg, botUser, botID, groupTriggerMode, addressingLLMTimeout, addressingConfidenceThreshold, addressingInterjectThreshold, historySnapshot, addressingReactionTool)
					if addressingReactionTool != nil {
						if reaction := addressingReactionTool.LastReaction(); reaction != nil {
							logger.Info("telegram_group_addressing_reaction_applied",
								"chat_id", reaction.ChatID,
								"message_id", reaction.MessageID,
								"emoji", reaction.Emoji,
								"source", reaction.Source,
							)
						}
					}
					if decErr != nil {
						logger.Warn("telegram_addressing_llm_error",
							"chat_id", chatID,
							"type", chatType,
							"error", decErr.Error(),
						)
						continue
					}
					if !ok {
						logger.Info("telegram_group_ignored",
							"chat_id", chatID,
							"type", chatType,
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
							appendIgnoredInboundHistory(rawText)
						}
						continue
					}
					replyToMessageID = quoteReplyMessageIDForGroupTrigger(msg, dec)
					quoteReply := replyToMessageID > 0
					logger.Info("telegram_group_trigger",
						"chat_id", chatID,
						"type", chatType,
						"reason", dec.Reason,
						"llm_addressed", dec.Addressing.Addressed,
						"confidence", dec.Addressing.Confidence,
						"wanna_interject", dec.Addressing.WannaInterject,
						"interject", dec.Addressing.Interject,
						"impulse", dec.Addressing.Impulse,
						"is_lightweight", dec.Addressing.IsLightweight,
						"quote_reply", quoteReply,
					)
					text = strings.TrimSpace(rawText)
					if strings.TrimSpace(text) == "" && !messageHasDownloadableFile(msg) && msg.ReplyTo == nil {
						// just ignore the unknown message
						continue
					}
				} else {
					if strings.TrimSpace(text) == "" && !messageHasDownloadableFile(msg) {
						continue
					}
				}
			}

			var downloaded []telegramDownloadedFile
			if messageHasDownloadableFile(msg) || (msg.ReplyTo != nil && messageHasDownloadableFile(msg.ReplyTo)) {
				telegramCacheDir := filepath.Join(fileCacheDir, "telegram")
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				downloaded, err = downloadTelegramMessageFiles(ctx, api, telegramCacheDir, filesMaxBytes, msg, chatID)
				cancel()
				if err != nil {
					correlationID := fmt.Sprintf("telegram:file_download_error:%d:%d", chatID, msg.MessageID)
					if _, publishErr := publishTelegramBusOutbound(context.Background(), inprocBus, chatID, "file download error: "+err.Error(), "", correlationID); publishErr != nil {
						logger.Warn("telegram_bus_publish_error", "channel", busruntime.ChannelTelegram, "chat_id", chatID, "message_id", msg.MessageID, "bus_error_code", busErrorCodeString(publishErr), "error", publishErr.Error())
						callErrorHook(context.Background(), logger, hooks, ErrorEvent{
							Stage:     ErrorStagePublishFileDownloadError,
							ChatID:    chatID,
							MessageID: msg.MessageID,
							Err:       publishErr,
						})
					}
					continue
				}
			}
			if strings.TrimSpace(text) == "" && len(downloaded) > 0 {
				text = "Please process the uploaded file(s)."
			}
			if len(downloaded) > 0 {
				text = appendDownloadedFilesToTask(text, downloaded)
			}
			imagePaths := collectDownloadedImagePaths(downloaded, 3)
			if msg.ReplyTo != nil {
				quoted := buildReplyContext(msg.ReplyTo)
				if quoted != "" {
					if strings.TrimSpace(text) == "" {
						text = "Please read the quoted message, and proceed according to the previous context, or your understanding, in the same langauge."
					}
					text = "Quoted message:\n> " + quoted + "\n\nUser request:\n" + strings.TrimSpace(text)
				}
			}
			if fromUserID > 0 {
				observedAt := time.Now().UTC()
				if err := applyTelegramInboundFeedback(context.Background(), contactsSvc, chatID, chatType, fromUserID, fromUsername, observedAt); err != nil {
					logger.Warn("contacts_feedback_telegram_error", "chat_id", chatID, "user_id", fromUserID, "error", err.Error())
				}
			}

			mentionUsers := dedupeNonEmptyStrings(mentionCandidates)
			if isGroup && mentionUserSnapshotLimit > 0 && len(mentionUsers) > mentionUserSnapshotLimit {
				mentionUsers = mentionUsers[:mentionUserSnapshotLimit]
			}
			accepted, publishErr := telegramInboundAdapter.HandleInboundMessage(context.Background(), telegrambus.InboundMessage{
				ChatID:           chatID,
				MessageID:        msg.MessageID,
				ReplyToMessageID: replyToMessageID,
				SentAt:           messageSentAt,
				ChatType:         chatType,
				FromUserID:       fromUserID,
				FromUsername:     fromUsername,
				FromFirstName:    fromFirst,
				FromLastName:     fromLast,
				FromDisplayName:  fromDisplay,
				Text:             text,
				MentionUsers:     mentionUsers,
				ImagePaths:       imagePaths,
			})
			if publishErr != nil {
				logger.Warn("telegram_bus_publish_error", "channel", busruntime.ChannelTelegram, "chat_id", chatID, "message_id", msg.MessageID, "bus_error_code", busErrorCodeString(publishErr), "error", publishErr.Error())
				callErrorHook(context.Background(), logger, hooks, ErrorEvent{
					Stage:     ErrorStagePublishInbound,
					ChatID:    chatID,
					MessageID: msg.MessageID,
					Err:       publishErr,
				})
				continue
			}
			if !accepted {
				logger.Debug("telegram_bus_inbound_deduped", "chat_id", chatID, "message_id", msg.MessageID)
				continue
			}
		}

	}
}

func telegramOutboundEventFromBusMessage(msg busruntime.BusMessage) (OutboundEvent, error) {
	chatID, err := telegramChatIDFromConversationKey(msg.ConversationKey)
	if err != nil {
		return OutboundEvent{}, err
	}
	env, err := msg.Envelope()
	if err != nil {
		return OutboundEvent{}, err
	}
	replyToRaw := strings.TrimSpace(msg.Extensions.ReplyTo)
	if replyToRaw == "" {
		replyToRaw = strings.TrimSpace(env.ReplyTo)
	}
	replyToMessageID := int64(0)
	if replyToRaw != "" {
		if parsed, parseErr := strconv.ParseInt(replyToRaw, 10, 64); parseErr == nil && parsed > 0 {
			replyToMessageID = parsed
		}
	}
	return OutboundEvent{
		ChatID:           chatID,
		ReplyToMessageID: replyToMessageID,
		Text:             strings.TrimSpace(env.Text),
		CorrelationID:    strings.TrimSpace(msg.CorrelationID),
		Kind:             telegramOutboundKind(msg.CorrelationID),
	}, nil
}

func telegramChatIDFromConversationKey(conversationKey string) (int64, error) {
	const prefix = "tg:"
	if !strings.HasPrefix(conversationKey, prefix) {
		return 0, fmt.Errorf("telegram conversation key is invalid")
	}
	raw := strings.TrimSpace(strings.TrimPrefix(conversationKey, prefix))
	if raw == "" {
		return 0, fmt.Errorf("telegram conversation key is invalid")
	}
	chatID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("telegram conversation key is invalid: %w", err)
	}
	return chatID, nil
}

func telegramOutboundKind(correlationID string) string {
	id := strings.ToLower(strings.TrimSpace(correlationID))
	switch {
	case strings.Contains(id, ":plan:"):
		return "plan_progress"
	case strings.Contains(id, ":error:") || strings.Contains(id, "file_download_error"):
		return "error"
	default:
		return "message"
	}
}

func nextTelegramPlanProgressState(state telegramPlanProgressEditState, correlationID string, line string) (telegramPlanProgressEditState, string) {
	correlationID = strings.TrimSpace(correlationID)
	line = strings.TrimSpace(line)
	next := telegramPlanProgressEditState{
		CorrelationID: correlationID,
	}
	if line == "" {
		return next, ""
	}

	if state.MessageID > 0 && strings.EqualFold(strings.TrimSpace(state.CorrelationID), correlationID) {
		next.MessageID = state.MessageID
		next.Lines = append(next.Lines, state.Lines...)
	}

	next.Lines = append(next.Lines, telegramPlanProgressLine{
		Text:  line,
		Emoji: emojiForTelegramPlanStep(line),
	})
	return next, renderTelegramPlanProgressExpandable(next.Lines)
}

func renderTelegramPlanProgressExpandable(lines []telegramPlanProgressLine) string {
	reversed := make([]string, 0, len(lines))
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i].Text)
		if line == "" {
			continue
		}
		emoji := strings.TrimSpace(lines[i].Emoji)
		if emoji == "" {
			emoji = emojiForTelegramPlanStep(line)
		}
		reversed = append(reversed, fmt.Sprintf("%s %d. %s", emoji, i+1, htmlstd.EscapeString(line)))
	}
	if len(reversed) == 0 {
		return ""
	}
	return "<blockquote expandable>" + strings.Join(reversed, "<br>") + "</blockquote>"
}

func emojiForTelegramPlanStep(step string) string {
	lower := strings.ToLower(strings.TrimSpace(step))
	switch {
	case strings.Contains(lower, "web_search"):
		return "🔎"
	case strings.Contains(lower, "url_fetch"):
		return "🧭"
	case strings.Contains(lower, "read_file"):
		return "📖"
	case strings.Contains(lower, "write_file"):
		return "✍️"
	case strings.Contains(lower, "_send_file"):
		return "🗂️"
	case strings.Contains(lower, "_send_photo"):
		return "📷"
	case strings.Contains(lower, "_send_voice"):
		return "🎙️"
	case strings.Contains(lower, "bash"):
		return "🧑‍💻"
	case strings.Contains(lower, "todo_update"):
		return "🗓️"
	case strings.Contains(lower, "contacts_send"):
		return "✉️"
	default:
		if randv2.IntN(2) == 0 {
			return "💭"
		}
		return "🤔"
	}
}

func telegramTaskID(chatID int64, messageID int64) string {
	return daemonruntime.BuildTaskID("tg", chatID, messageID)
}

func telegramManagedTopicInfo(chatID int64, chatType string, displayName string, username string) (string, string) {
	topicID := fmt.Sprintf("telegram:%d", chatID)
	label := strings.TrimSpace(displayName)
	if label == "" {
		label = strings.TrimSpace(username)
	}
	if label != "" {
		return topicID, daemonruntime.TruncateUTF8("Telegram · "+label, 72)
	}
	chatType = strings.TrimSpace(strings.ToLower(chatType))
	if chatType != "" && chatType != "private" {
		return topicID, daemonruntime.TruncateUTF8("Telegram · "+chatType+" · "+strconv.FormatInt(chatID, 10), 72)
	}
	return topicID, daemonruntime.TruncateUTF8("Telegram · "+strconv.FormatInt(chatID, 10), 72)
}

func recordTelegramQueuedTask(store daemonruntime.TaskView, info daemonruntime.TaskInfo, trigger daemonruntime.TaskTrigger, topicTitle string) {
	if store == nil {
		return
	}
	if writer, ok := store.(interface {
		UpsertWithTrigger(daemonruntime.TaskInfo, daemonruntime.TaskTrigger, string) error
	}); ok {
		_ = writer.UpsertWithTrigger(info, trigger, topicTitle)
		return
	}
	_ = daemonruntime.RecordTaskUpsert(store, info, trigger)
}

func isTaskContextCanceled(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "context canceled") || strings.Contains(msg, "context deadline exceeded")
}
