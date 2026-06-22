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
	"github.com/quailyquaily/mistermorph/internal/channelruntime/imagehistory"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/imagesession"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/personautil"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/internal/telegramutil"
	"github.com/quailyquaily/mistermorph/internal/topiccontext"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	telegramtools "github.com/quailyquaily/mistermorph/tools/telegram"
)

type telegramJob struct {
	TaskID           string
	ConversationKey  string
	ChatID           int64
	MessageThreadID  int64
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
	Images           []chathistory.ChatHistoryImage
	WorkspaceDir     string
	ResumeApprovalID string
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

func sendTelegramUnauthorizedMessage(api *telegramAPI, chatID int64, messageThreadID int64, chatType string) {
	chatType = strings.TrimSpace(chatType)
	if chatType == "" {
		chatType = "unknown"
	}
	msg := fmt.Sprintf("You don't have permission to use this bot. Please contact the admin.\nchat_id: `%d`, type: `%s`", chatID, chatType)
	_ = api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, msg, true)
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
	sharedRuntime, err := runtimecore.BootstrapChannelRuntime(pollCtx, d.CommonDependencies, runtimecore.ChannelBootstrapOptions{
		Mode:                "telegram",
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
	taskRuntimeOpts := runtimeTaskOptions{
		MemoryEnabled:           opts.MemoryEnabled,
		MemoryInjectionEnabled:  opts.MemoryInjectionEnabled,
		MemoryInjectionMaxItems: opts.MemoryInjectionMaxItems,
		FileCacheDir:            opts.FileCacheDir,
		MemoryOrchestrator:      memRuntime.Orchestrator,
		MemoryProjectionWorker:  memRuntime.ProjectionWorker,
	}
	runControl := runtimecontrol.New()
	pollTimeout := opts.PollTimeout
	taskTimeout := opts.TaskTimeout
	maxConc := opts.MaxConcurrency
	sem := make(chan struct{}, maxConc)
	workersCtx, stopWorkers := context.WithCancel(pollCtx)
	defer stopWorkers()
	serverListen := strings.TrimSpace(opts.Server.Listen)
	var approvalRoutesMu sync.RWMutex
	var approvalListRoute daemonruntime.ApprovalListFunc
	var approvalApproveRoute daemonruntime.ApprovalDecisionFunc
	var approvalDenyRoute daemonruntime.ApprovalDecisionFunc
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
						"poke_enabled":     opts.Server.Poke != nil,
						"cron_run_enabled": opts.Server.CronRun != nil,
					}, nil
				},
				Poke:    opts.Server.Poke,
				CronRun: opts.Server.CronRun,
				ApprovalList: func(ctx context.Context, req daemonruntime.ApprovalListRequest) (daemonruntime.ApprovalListResponse, error) {
					approvalRoutesMu.RLock()
					handler := approvalListRoute
					approvalRoutesMu.RUnlock()
					if handler == nil {
						return daemonruntime.ApprovalListResponse{}, fmt.Errorf("approvals are unavailable")
					}
					return handler(ctx, req)
				},
				ApprovalApprove: func(ctx context.Context, req daemonruntime.ApprovalDecisionRequest) (daemonruntime.ApprovalDecisionResponse, error) {
					approvalRoutesMu.RLock()
					handler := approvalApproveRoute
					approvalRoutesMu.RUnlock()
					if handler == nil {
						return daemonruntime.ApprovalDecisionResponse{}, fmt.Errorf("approvals are unavailable")
					}
					return handler(ctx, req)
				},
				ApprovalDeny: func(ctx context.Context, req daemonruntime.ApprovalDecisionRequest) (daemonruntime.ApprovalDecisionResponse, error) {
					approvalRoutesMu.RLock()
					handler := approvalDenyRoute
					approvalRoutesMu.RUnlock()
					if handler == nil {
						return daemonruntime.ApprovalDecisionResponse{}, fmt.Errorf("approvals are unavailable")
					}
					return handler(ctx, req)
				},
				AgentSettingsEnabled: true,
				HealthEnabled:        true,
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
		planProgressStateByID = make(map[string]telegramPlanProgressEditState)
	)
	parseSendTextInput := func(target any, opts telegrambus.SendTextOptions) (int64, int64, int64, string, error) {
		deliveryTarget, ok := target.(telegrambus.DeliveryTarget)
		if !ok {
			return 0, 0, 0, "", fmt.Errorf("telegram target is invalid")
		}
		chatID := deliveryTarget.ChatID
		messageThreadID := deliveryTarget.MessageThreadID
		replyToMessageID := int64(0)
		replyToRaw := strings.TrimSpace(opts.ReplyTo)
		if replyToRaw != "" {
			parsed, parseErr := strconv.ParseInt(replyToRaw, 10, 64)
			if parseErr != nil || parsed <= 0 {
				return 0, 0, 0, "", fmt.Errorf("telegram reply_to is invalid")
			}
			replyToMessageID = parsed
		}
		return chatID, messageThreadID, replyToMessageID, strings.TrimSpace(opts.CorrelationID), nil
	}
	sendPlanProgress := func(ctx context.Context, chatID int64, messageThreadID int64, text string, replyToMessageID int64, correlationID string) error {
		line := strings.TrimSpace(text)
		if line == "" {
			return nil
		}
		progressKey := telegramConversationMapKey(chatID, messageThreadID)
		var state telegramPlanProgressEditState
		planProgressEditMu.Lock()
		state = planProgressStateByID[progressKey]
		planProgressEditMu.Unlock()

		nextState, rendered := nextTelegramPlanProgressState(state, correlationID, line)
		if rendered == "" {
			return nil
		}
		if nextState.MessageID > 0 && strings.EqualFold(nextState.CorrelationID, correlationID) {
			if err := api.editMessageHTML(ctx, chatID, nextState.MessageID, rendered, true); err == nil || isTelegramMessageNotModified(err) {
				planProgressEditMu.Lock()
				planProgressStateByID[progressKey] = nextState
				planProgressEditMu.Unlock()
				return nil
			} else {
				logger.Warn("telegram_plan_progress_edit_failed", "chat_id", chatID, "message_id", nextState.MessageID, "correlation_id", correlationID, "error", err.Error())
			}
		}
		messageID, err := api.sendMessageChunkedReplyInThreadWithFirstMessageID(ctx, chatID, messageThreadID, rendered, replyToMessageID)
		if err != nil {
			return err
		}
		if messageID > 0 && correlationID != "" {
			nextState.MessageID = messageID
			planProgressEditMu.Lock()
			planProgressStateByID[progressKey] = nextState
			planProgressEditMu.Unlock()
		}
		return nil
	}
	telegramDeliveryAdapter, err = telegrambus.NewDeliveryAdapter(telegrambus.DeliveryAdapterOptions{
		SendText: func(ctx context.Context, target any, text string, opts telegrambus.SendTextOptions) error {
			chatID, messageThreadID, replyToMessageID, correlationID, err := parseSendTextInput(target, opts)
			if err != nil {
				return err
			}
			kind := telegramOutboundKind(correlationID)
			if kind == "plan_progress" {
				return sendPlanProgress(ctx, chatID, messageThreadID, text, replyToMessageID, correlationID)
			}
			return api.sendMessageChunkedReplyInThread(ctx, chatID, messageThreadID, text, replyToMessageID)
		},
	})
	if err != nil {
		return err
	}
	publishTelegramText := func(ctx context.Context, chatID int64, messageThreadID int64, text string, correlationID string) error {
		replyTo := ""
		_, err := publishTelegramBusOutbound(ctx, inprocBus, chatID, messageThreadID, text, replyTo, correlationID)
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
	protected, protectedErr := imagesession.NewStore(d.CommonDependencies.RuntimeToolsConfig.Image.FileStateDir).ProtectedPaths(fileCacheDir)
	if protectedErr != nil {
		logger.Warn("file_cache_protected_paths_error", "error", protectedErr.Error())
	}
	if err := telegramutil.CleanupFileCacheDirWithProtected(telegramCacheDir, maxAge, maxFiles, maxTotalBytes, protected); err != nil {
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
		history            = make(map[string][]chathistory.ChatHistoryItem)
		stickySkillsByChat = make(map[string][]string)
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
	var sharedGuard *guard.Guard

	var (
		warningsMu                sync.Mutex
		systemWarnings            []string
		systemWarningsSeen        = make(map[string]bool)
		systemWarningsVersion     int
		systemWarningsSentVersion = make(map[string]int)
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

	markSystemWarningsSent := func(chatID int64, messageThreadID int64, version int) {
		warningsMu.Lock()
		defer warningsMu.Unlock()
		key := telegramConversationMapKey(chatID, messageThreadID)
		if systemWarningsSentVersion[key] < version {
			systemWarningsSentVersion[key] = version
		}
	}

	sendSystemWarnings := func(chatID int64, messageThreadID int64) {
		if len(allowed) > 0 && !allowed[chatID] {
			return
		}
		msg, version := systemWarningsSnapshot()
		if version == 0 {
			return
		}
		key := telegramConversationMapKey(chatID, messageThreadID)
		warningsMu.Lock()
		sentVersion := systemWarningsSentVersion[key]
		warningsMu.Unlock()
		if sentVersion >= version {
			return
		}
		_ = api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, msg, true)
		markSystemWarningsSent(chatID, messageThreadID, version)
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
			key := telegramConversationMapKey(chatID, 0)
			warningsMu.Lock()
			sentVersion := systemWarningsSentVersion[key]
			warningsMu.Unlock()
			if sentVersion >= version {
				continue
			}
			_ = api.sendMessageHTML(context.Background(), chatID, msg, true)
			markSystemWarningsSent(chatID, 0, version)
		}
	}

	sharedGuard = depsutil.GuardFromCommon(d.CommonDependencies, logger)
	if sharedGuard != nil {
		for _, warn := range sharedGuard.Warnings() {
			enqueueSystemWarning(warn)
		}
		broadcastSystemWarnings()
	}

	var (
		pendingApprovalsMu sync.Mutex
		pendingApprovals   = make(map[string]telegramJob)
	)
	var runner *runtimecore.ConversationRunner[string, telegramJob]
	registerPendingApproval := func(approvalID string, job telegramJob) {
		approvalID = strings.TrimSpace(approvalID)
		if approvalID == "" {
			return
		}
		pendingApprovalsMu.Lock()
		pendingApprovals[approvalID] = job
		pendingApprovalsMu.Unlock()
	}
	takePendingApproval := func(approvalID string) (telegramJob, bool) {
		approvalID = strings.TrimSpace(approvalID)
		if approvalID == "" {
			return telegramJob{}, false
		}
		pendingApprovalsMu.Lock()
		job, ok := pendingApprovals[approvalID]
		if ok {
			delete(pendingApprovals, approvalID)
		}
		pendingApprovalsMu.Unlock()
		return job, ok
	}
	notifyPendingApproval := func(ctx context.Context, approvalID string, job telegramJob) {
		if sharedGuard == nil || api == nil {
			return
		}
		rec, ok, err := sharedGuard.GetApproval(ctx, approvalID)
		if err != nil {
			logger.Warn("telegram_approval_get_error", "approval_request_id", approvalID, "error", err.Error())
			return
		}
		if !ok {
			logger.Warn("telegram_approval_missing", "approval_request_id", approvalID)
			return
		}
		text := telegramApprovalRequestText(job, rec)
		replyMarkup := telegramApprovalReplyMarkup(approvalID)
		if replyMarkup == nil {
			return
		}
		if job.ChatID == 0 {
			return
		}
		if _, err := api.sendMessageHTMLReplyInThreadWithMessageIDAndMarkup(ctx, job.ChatID, job.MessageThreadID, text, true, 0, replyMarkup); err != nil {
			logger.Warn("telegram_approval_notify_error", "approval_request_id", approvalID, "chat_id", job.ChatID, "error", err.Error())
		}
	}
	applyApprovalDecision := func(ctx context.Context, approvalID string, approved bool, actor string) (string, bool, error) {
		approvalID = strings.TrimSpace(approvalID)
		if approvalID == "" {
			return "", false, daemonruntime.BadRequest("approval_request_id is required")
		}
		actor = strings.TrimSpace(actor)
		if actor == "" {
			actor = "telegram:console"
		}
		if sharedGuard == nil {
			return "", false, fmt.Errorf("approvals are unavailable")
		}
		rec, found, err := sharedGuard.GetApproval(ctx, approvalID)
		if err != nil {
			return "", false, err
		}
		if !found || rec.Status != guard.ApprovalPending || (!rec.ExpiresAt.IsZero() && time.Now().UTC().After(rec.ExpiresAt)) {
			return "", false, daemonruntime.BadRequest("approval is not pending")
		}
		status := guard.ApprovalDenied
		if approved {
			status = guard.ApprovalApproved
		}
		if err := sharedGuard.ResolveApproval(ctx, approvalID, status, actor, ""); err != nil {
			return "", false, telegramApprovalDecisionError(err)
		}
		job, ok := takePendingApproval(approvalID)
		if !ok {
			return markTelegramMissingApprovalHandle(daemonStore, approvalID, approved)
		}
		if !approved {
			finishedAt := time.Now().UTC()
			if daemonStore != nil && strings.TrimSpace(job.TaskID) != "" {
				daemonStore.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
					info.Status = daemonruntime.TaskCanceled
					info.Error = telegramApprovalResultText(false)
					info.FinishedAt = &finishedAt
					runtimecore.ClearTaskPendingApprovalFields(info)
				})
			}
			return job.TaskID, false, nil
		}
		if runner == nil {
			return job.TaskID, false, markTelegramApprovalResumeFailed(daemonStore, job.TaskID, "runner unavailable")
		}
		job.ResumeApprovalID = approvalID
		if err := runner.Enqueue(workersCtx, job.ConversationKey, func(version uint64) telegramJob {
			job.Version = version
			return job
		}); err != nil {
			return job.TaskID, false, markTelegramApprovalResumeFailed(daemonStore, job.TaskID, strings.TrimSpace(err.Error()))
		}
		resumedAt := time.Now().UTC()
		if daemonStore != nil && strings.TrimSpace(job.TaskID) != "" {
			daemonStore.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
				info.Status = daemonruntime.TaskQueued
				info.Error = ""
				info.ResumedAt = &resumedAt
				runtimecore.ClearTaskPendingApprovalFields(info)
			})
		}
		return job.TaskID, true, nil
	}
	handleApprovalCallback := func(ctx context.Context, query *telegramCallbackQuery) bool {
		if query == nil {
			return false
		}
		approvalID, approved, ok := parseTelegramApprovalCallbackData(query.Data)
		if !ok {
			return false
		}
		answer := func(text string, alert bool) {
			if api == nil || strings.TrimSpace(query.ID) == "" {
				return
			}
			if err := api.answerCallbackQuery(context.Background(), query.ID, text, alert); err != nil {
				logger.Warn("telegram_approval_callback_answer_error", "approval_request_id", approvalID, "error", err.Error())
			}
		}
		sendResultMessage := func(text string) {
			if api == nil || strings.TrimSpace(text) == "" {
				return
			}
			chatID, messageThreadID, ok := telegramApprovalCallbackMessageTarget(query)
			if !ok {
				return
			}
			if _, err := api.sendMessageHTMLReplyInThreadWithMessageID(ctx, chatID, messageThreadID, text, true, 0); err != nil {
				logger.Warn("telegram_approval_result_message_error", "approval_request_id", approvalID, "chat_id", chatID, "error", err.Error())
			}
		}
		answerText := telegramApprovalResultText(approved)
		if _, _, err := applyApprovalDecision(ctx, approvalID, approved, telegramApprovalActor(query.From)); err != nil {
			answer(strings.TrimSpace(err.Error()), true)
			logger.Warn("telegram_approval_decision_error", "approval_request_id", approvalID, "error", err.Error())
			return true
		}
		answer(answerText, false)
		sendResultMessage(answerText)
		return true
	}
	runner = runtimecore.NewConversationRunner[string, telegramJob](
		workersCtx,
		sem,
		16,
		func(workerCtx context.Context, conversationKey string, job telegramJob) {
			chatID := job.ChatID
			mu.Lock()
			h := append([]chathistory.ChatHistoryItem(nil), history[conversationKey]...)
			sticky := append([]string(nil), stickySkillsByChat[conversationKey]...)
			mu.Unlock()
			curVersion := runner.CurrentVersion(conversationKey)

			// If there was a /reset after this job was queued, drop history for this run.
			if job.Version != curVersion {
				h = nil
			}

			typingStop := startTypingTickerInThread(workerCtx, api, chatID, job.MessageThreadID, "typing", 4*time.Second)
			defer typingStop()
			runtimecore.MarkTaskRunning(daemonStore, job.TaskID)

			lease, err := runControl.StartLease(workerCtx, taskTimeout, runtimecontrol.ActiveRun{
				Runtime:         "telegram",
				ConversationKey: conversationKey,
				TopicID:         telegramContextTopicID(job),
				TaskID:          job.TaskID,
				RunID:           job.TaskID,
			})
			if err != nil {
				runtimecore.MarkTaskFailed(daemonStore, job.TaskID, strings.TrimSpace(err.Error()), false)
				return
			}
			final, _, loadedSkills, reaction, runErr := runTelegramTask(lease.Context, execRuntime, api, fileCacheDir, filesMaxBytes, allowed, job, botUser, h, telegramHistoryCap, sticky, requestTimeout, taskRuntimeOpts, lease.SteerQueue, publishTelegramText)
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
				runtimecore.MarkTaskFailed(daemonStore, job.TaskID, displayErr, isTaskContextCanceled(runErr) || userStopped)
				callErrorHook(workerCtx, logger, hooks, ErrorEvent{
					Stage:     ErrorStageRunTask,
					ChatID:    chatID,
					MessageID: job.MessageID,
					Err:       runErr,
				})
				if userStopped {
					return
				}
				errorCorrelationID := fmt.Sprintf("telegram:error:%d:%d", chatID, job.MessageID)
				errorText := "error: " + displayErr
				if _, err := publishTelegramBusOutbound(workerCtx, inprocBus, chatID, job.MessageThreadID, errorText, "", errorCorrelationID); err != nil {
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

			if pendingID, ok := runtimecore.PendingApprovalID(final); ok {
				registerPendingApproval(pendingID, job)
				pendingAt := time.Now().UTC()
				if daemonStore != nil {
					daemonStore.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
						info.Status = daemonruntime.TaskPending
						info.PendingAt = &pendingAt
						info.ApprovalRequestID = pendingID
						info.Result = map[string]any{
							"source": "telegram",
							"final":  final,
						}
					})
				}
				notifyPendingApproval(context.Background(), pendingID, job)
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
				if _, err := publishTelegramBusOutbound(workerCtx, inprocBus, chatID, job.MessageThreadID, outText, replyTo, outCorrelationID); err != nil {
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
			latestVersion := runner.CurrentVersion(conversationKey)
			if latestVersion != curVersion {
				history[conversationKey] = nil
				stickySkillsByChat[conversationKey] = nil
			}
			if latestVersion == curVersion && len(loadedSkills) > 0 {
				stickySkillsByChat[conversationKey] = capUniqueStrings(loadedSkills, telegramStickySkillsCap)
			}
			cur := history[conversationKey]
			inboundHistory := newTelegramInboundHistoryItem(job)
			if publishText {
				inboundHistory.Images = imagehistory.WithDescription(inboundHistory.Images, outText, "agent_final")
			}
			cur = append(cur, inboundHistory)
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
			history[conversationKey] = trimChatHistoryItems(cur, telegramHistoryCap)
			mu.Unlock()
		},
	)
	approvalRoutesMu.Lock()
	approvalListRoute = func(ctx context.Context, req daemonruntime.ApprovalListRequest) (daemonruntime.ApprovalListResponse, error) {
		return runtimecore.ListPendingApprovals(ctx, daemonStore, sharedGuard, req, "telegram")
	}
	approvalApproveRoute = func(ctx context.Context, req daemonruntime.ApprovalDecisionRequest) (daemonruntime.ApprovalDecisionResponse, error) {
		taskID, resumed, err := applyApprovalDecision(ctx, req.ApprovalRequestID, true, strings.TrimSpace(req.Actor))
		if err != nil {
			if strings.TrimSpace(taskID) != "" {
				return daemonruntime.ApprovalDecisionResponse{
					ApprovalRequestID: strings.TrimSpace(req.ApprovalRequestID),
					TaskID:            taskID,
					Status:            string(guard.ApprovalApproved),
					Resumed:           false,
					Error:             strings.TrimSpace(err.Error()),
				}, nil
			}
			return daemonruntime.ApprovalDecisionResponse{}, err
		}
		return daemonruntime.ApprovalDecisionResponse{
			ApprovalRequestID: strings.TrimSpace(req.ApprovalRequestID),
			TaskID:            taskID,
			Status:            string(guard.ApprovalApproved),
			Resumed:           resumed,
		}, nil
	}
	approvalDenyRoute = func(ctx context.Context, req daemonruntime.ApprovalDecisionRequest) (daemonruntime.ApprovalDecisionResponse, error) {
		taskID, resumed, err := applyApprovalDecision(ctx, req.ApprovalRequestID, false, strings.TrimSpace(req.Actor))
		if err != nil {
			return daemonruntime.ApprovalDecisionResponse{}, err
		}
		return daemonruntime.ApprovalDecisionResponse{
			ApprovalRequestID: strings.TrimSpace(req.ApprovalRequestID),
			TaskID:            taskID,
			Status:            string(guard.ApprovalDenied),
			Resumed:           resumed,
		}, nil
	}
	approvalRoutesMu.Unlock()

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

		imagePaths := busruntime.ImagePathsFromAttachments(inbound.ImageAttachments)
		logger.Info("telegram_task_enqueued",
			"channel", msg.Channel,
			"topic", msg.Topic,
			"chat_id", inbound.ChatID,
			"type", inbound.ChatType,
			"idempotency_key", msg.IdempotencyKey,
			"conversation_key", msg.ConversationKey,
			"text_len", len(text),
			"image_count", len(inbound.ImageAttachments),
		)
		workspaceDir, err := workspace.LookupWorkspaceDir(workspaceStore, msg.ConversationKey)
		if err != nil {
			return err
		}
		images := imagehistory.BuildFromAttachments(inbound.ImageAttachments, pathroots.New(workspaceDir, fileCacheDir, ""))
		jobTaskID := telegramTaskID(inbound.ChatID, inbound.MessageThreadID, inbound.MessageID)
		if err := runner.Enqueue(ctx, msg.ConversationKey, func(version uint64) telegramJob {
			return telegramJob{
				TaskID:           jobTaskID,
				ConversationKey:  msg.ConversationKey,
				ChatID:           inbound.ChatID,
				MessageThreadID:  inbound.MessageThreadID,
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
				ImagePaths:       imagePaths,
				Images:           append([]chathistory.ChatHistoryImage(nil), images...),
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
			topicID, topicTitle := telegramManagedTopicInfo(inbound.ChatID, inbound.MessageThreadID, inbound.ChatType, inbound.FromDisplayName, inbound.FromUsername)
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
					"telegram_thread_id":    inbound.MessageThreadID,
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
				Ref:    telegramTaskRef(inbound.ChatID, inbound.MessageThreadID, inbound.MessageID),
			}, topicTitle)
		}
		callInboundHook(ctx, logger, hooks, InboundEvent{
			ChatID:          inbound.ChatID,
			MessageThreadID: inbound.MessageThreadID,
			MessageID:       inbound.MessageID,
			ChatType:        inbound.ChatType,
			FromUserID:      inbound.FromUserID,
			Text:            text,
			MentionUsers:    append([]string(nil), inbound.MentionUsers...),
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
			if handleApprovalCallback(context.Background(), u.CallbackQuery) {
				continue
			}
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
			messageThreadID := msg.MessageThreadID
			conversationKey, convErr := busruntime.BuildTelegramTopicConversationKey(strconv.FormatInt(chatID, 10), messageThreadID)
			if convErr != nil {
				logger.Warn("telegram_conversation_key_error", "chat_id", chatID, "message_thread_id", messageThreadID, "error", convErr.Error())
				continue
			}
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
			sendSystemWarnings(chatID, messageThreadID)

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
				cur := history[conversationKey]
				cur = append(cur, newTelegramInboundHistoryItem(telegramJob{
					ChatID:          chatID,
					MessageThreadID: messageThreadID,
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
				history[conversationKey] = trimChatHistoryItems(cur, telegramHistoryCap)
				mu.Unlock()
			}

			cmdWord, cmdArgs := chatcommands.ParseCommand(text)
			normalizedCmd := chatcommands.NormalizeCommand(cmdWord)
			replyToMessageID := int64(0)
			switch normalizedCmd {
			case "/stop":
				if len(allowed) > 0 && !allowed[chatID] {
					logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
					sendTelegramUnauthorizedMessage(api, chatID, messageThreadID, chatType)
					continue
				}
				result := runControl.Stop("telegram", conversationKey, "/stop")
				_ = api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, htmlstd.EscapeString(runtimecontrol.StopFeedback(result.Found)), true)
				continue
			case "/help":
				help := "Send a message and I will run it as an agent task.\n" +
					"Commands: /think, /stop, /models, /skills, /ctx, /workspace, /reset, /id\n\n" +
					"Group chats: reply to me, or mention @" + botUser + ".\n" +
					"You can also send a file (document/photo). It will be downloaded under file_cache_dir/telegram/ and the agent can process it.\n" +
					"Note: if Bot Privacy Mode is enabled, I may not receive normal group messages."
				_ = api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, help, true)
				continue
			case "/models":
				if len(allowed) > 0 && !allowed[chatID] {
					logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
					sendTelegramUnauthorizedMessage(api, chatID, messageThreadID, chatType)
					continue
				}
				if executeTelegramProfileCommand(d, api, chatID, messageThreadID, text) {
					continue
				}
				_ = api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, "error: "+htmlstd.EscapeString("missing llm profile command handler"), true)
				continue
			case "/skills":
				if len(allowed) > 0 && !allowed[chatID] {
					logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
					sendTelegramUnauthorizedMessage(api, chatID, messageThreadID, chatType)
					continue
				}
				mu.Lock()
				currentSkills := append([]string(nil), stickySkillsByChat[conversationKey]...)
				mu.Unlock()
				if executeTelegramSkillCommand(d, api, chatID, messageThreadID, currentSkills) {
					continue
				}
				_ = api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, "error: "+htmlstd.EscapeString("missing skill command handler"), true)
				continue
			case "/ctx":
				if len(allowed) > 0 && !allowed[chatID] {
					logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
					sendTelegramUnauthorizedMessage(api, chatID, messageThreadID, chatType)
					continue
				}
				reply, cmdErr := topiccontext.RenderCommandText(conversationKey)
				if cmdErr != nil {
					reply = "error: " + strings.TrimSpace(cmdErr.Error())
				}
				_ = api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, htmlstd.EscapeString(reply), true)
				continue
			case "/id":
				idText := fmt.Sprintf("chat_id=%d type=%s", chatID, chatType)
				if messageThreadID > 0 {
					idText += fmt.Sprintf(" message_thread_id=%d", messageThreadID)
				}
				_ = api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, idText, true)
				continue
			case "/workspace":
				if len(allowed) > 0 && !allowed[chatID] {
					logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
					sendTelegramUnauthorizedMessage(api, chatID, messageThreadID, chatType)
					continue
				}
				result, cmdErr := workspace.ExecuteStoreCommand(workspaceStore, conversationKey, cmdArgs, nil)
				reply := result.Reply
				if cmdErr != nil {
					reply = "error: " + strings.TrimSpace(cmdErr.Error())
				}
				_ = api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, htmlstd.EscapeString(reply), true)
				continue
			case "/think":
				if len(allowed) > 0 && !allowed[chatID] {
					logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
					sendTelegramUnauthorizedMessage(api, chatID, messageThreadID, chatType)
					continue
				}
			case "/reset":
				if len(allowed) > 0 && !allowed[chatID] {
					logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
					sendTelegramUnauthorizedMessage(api, chatID, messageThreadID, chatType)
					continue
				}
				mu.Lock()
				delete(history, conversationKey)
				delete(stickySkillsByChat, conversationKey)
				delete(knownMentions, chatID)
				runner.IncrementVersion(conversationKey)
				mu.Unlock()
				planProgressEditMu.Lock()
				delete(planProgressStateByID, conversationKey)
				planProgressEditMu.Unlock()
				_ = api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, "ok (reset)", true)
				continue
			default:
				if len(allowed) > 0 && !allowed[chatID] {
					logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
					sendTelegramUnauthorizedMessage(api, chatID, messageThreadID, chatType)
					continue
				}
				if isGroup {
					if shouldSkipGroupReplyWithoutBodyMention(msg, text, botUser, botID) {
						logFields := []any{
							"chat_id", chatID,
							"message_id", msg.MessageID,
							"message_thread_id", messageThreadID,
							"is_topic_message", msg.IsTopicMessage,
							"type", chatType,
							"text_len", len(text),
							"entities_count", len(msg.Entities),
							"caption_entities_count", len(msg.CaptionEntities),
						}
						logFields = append(logFields, telegramReplyToMessageLogFields(msg.ReplyTo)...)
						logger.Info("telegram_group_ignored_reply_without_at_mention", logFields...)
						appendIgnoredInboundHistory(rawText)
						continue
					}
					mu.Lock()
					historySnapshot := append([]chathistory.ChatHistoryItem(nil), history[conversationKey]...)
					mu.Unlock()
					var addressingReactionTool *telegramtools.ReactTool
					if api != nil && msg != nil && msg.MessageID > 0 {
						addressingReactionTool = telegramtools.NewReactTool(newTelegramToolAPI(api), chatID, msg.MessageID, allowed)
					}
					decisionCtx := context.Background()
					if msg != nil && msg.MessageID > 0 {
						decisionCtx = llmstats.WithRunID(decisionCtx, telegramTaskID(chatID, messageThreadID, msg.MessageID))
					}
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
			downloadRoots := pathroots.New("", fileCacheDir, "")
			if messageHasDownloadableFile(msg) || (msg.ReplyTo != nil && messageHasDownloadableFile(msg.ReplyTo)) {
				downloadDir := filepath.Join(fileCacheDir, "telegram")
				if conversationKey, keyErr := busruntime.BuildTelegramTopicConversationKey(strconv.FormatInt(chatID, 10), messageThreadID); keyErr == nil {
					if workspaceDir, workspaceErr := workspace.LookupWorkspaceDir(workspaceStore, conversationKey); workspaceErr == nil {
						downloadRoots = pathroots.New(workspaceDir, fileCacheDir, "")
						if dir, dirErr := imagehistory.DownloadDir(fileCacheDir, workspaceDir, chathistory.ChannelTelegram); dirErr == nil {
							downloadDir = dir
						} else {
							logger.Warn("telegram_image_download_dir_error", "conversation_key", conversationKey, "error", dirErr.Error())
						}
					}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				downloaded, err = downloadTelegramMessageFiles(ctx, api, downloadDir, filesMaxBytes, msg, chatID)
				cancel()
				if err != nil {
					correlationID := fmt.Sprintf("telegram:file_download_error:%d:%d", chatID, msg.MessageID)
					if _, publishErr := publishTelegramBusOutbound(context.Background(), inprocBus, chatID, messageThreadID, "file download error: "+err.Error(), "", correlationID); publishErr != nil {
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
				text = appendDownloadedFilesToTask(text, downloaded, downloadRoots)
			}
			imageAttachments := collectDownloadedImageAttachments(downloaded, 3)
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
			if len(downloaded) == 0 && len(imageAttachments) == 0 {
				if result := runControl.Steer("telegram", conversationKey, text); result.Found {
					correlationID := fmt.Sprintf("telegram:steer:%d:%d", chatID, msg.MessageID)
					if _, publishErr := publishTelegramBusOutbound(context.Background(), inprocBus, chatID, messageThreadID, runtimecontrol.SteerFeedback(result.Found, result.Queued), "", correlationID); publishErr != nil {
						logger.Warn("telegram_bus_publish_error", "channel", busruntime.ChannelTelegram, "chat_id", chatID, "message_id", msg.MessageID, "bus_error_code", busErrorCodeString(publishErr), "error", publishErr.Error())
					}
					continue
				}
			}
			accepted, publishErr := telegramInboundAdapter.HandleInboundMessage(context.Background(), telegrambus.InboundMessage{
				ChatID:           chatID,
				MessageThreadID:  messageThreadID,
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
				ImageAttachments: imageAttachments,
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
	chatID, messageThreadID, err := telegrambus.ConversationPartsFromBusMessage(msg)
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
		MessageThreadID:  messageThreadID,
		ReplyToMessageID: replyToMessageID,
		Text:             strings.TrimSpace(env.Text),
		CorrelationID:    strings.TrimSpace(msg.CorrelationID),
		Kind:             telegramOutboundKind(msg.CorrelationID),
	}, nil
}

func telegramChatIDFromConversationKey(conversationKey string) (int64, error) {
	chatID, _, err := telegramConversationPartsFromKey(conversationKey)
	if err != nil {
		return 0, err
	}
	return chatID, nil
}

func telegramConversationPartsFromKey(conversationKey string) (int64, int64, error) {
	return busruntime.ParseTelegramConversationKey(conversationKey)
}

func telegramConversationMapKey(chatID int64, messageThreadID int64) string {
	key, err := busruntime.BuildTelegramTopicConversationKey(strconv.FormatInt(chatID, 10), messageThreadID)
	if err != nil {
		return fmt.Sprintf("tg:%d", chatID)
	}
	return key
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

func telegramTaskID(chatID int64, messageThreadID int64, messageID int64) string {
	if messageThreadID > 0 {
		return daemonruntime.BuildTaskID("tg", chatID, messageThreadID, messageID)
	}
	return daemonruntime.BuildTaskID("tg", chatID, messageID)
}

func telegramApprovalDecisionError(err error) error {
	if errors.Is(err, guard.ErrApprovalNotFound) {
		return daemonruntime.BadRequest("approval not found")
	}
	if errors.Is(err, guard.ErrApprovalNotPending) {
		return daemonruntime.BadRequest("approval is not pending")
	}
	return err
}

func markTelegramApprovalResumeFailed(store daemonruntime.TaskUpdater, taskID string, msg string) error {
	taskID = strings.TrimSpace(taskID)
	msg = strings.TrimSpace(msg)
	if msg == "" {
		msg = "unknown error"
	}
	displayErr := "approval resume failed: " + msg
	if store != nil && taskID != "" {
		finishedAt := time.Now().UTC()
		store.Update(taskID, func(info *daemonruntime.TaskInfo) {
			info.Status = daemonruntime.TaskFailed
			info.Error = displayErr
			info.FinishedAt = &finishedAt
			runtimecore.ClearTaskPendingApprovalFields(info)
		})
	}
	return fmt.Errorf("%s", displayErr)
}

func markTelegramMissingApprovalHandle(store daemonruntime.TaskView, approvalID string, approved bool) (string, bool, error) {
	taskID := runtimecore.TaskIDForPendingApproval(store, approvalID)
	if taskID == "" {
		return "", false, fmt.Errorf("pending approval handle is unavailable")
	}
	if approved {
		return taskID, false, markTelegramApprovalResumeFailed(store, taskID, "pending approval handle is unavailable")
	}
	finishedAt := time.Now().UTC()
	store.Update(taskID, func(info *daemonruntime.TaskInfo) {
		info.Status = daemonruntime.TaskCanceled
		info.Error = telegramApprovalResultText(false)
		info.FinishedAt = &finishedAt
		runtimecore.ClearTaskPendingApprovalFields(info)
	})
	return taskID, false, nil
}

func telegramTaskRef(chatID int64, messageThreadID int64, messageID int64) string {
	if messageThreadID > 0 {
		return fmt.Sprintf("telegram/%d/%d/%d", chatID, messageThreadID, messageID)
	}
	return fmt.Sprintf("telegram/%d/%d", chatID, messageID)
}

func telegramManagedTopicInfo(chatID int64, messageThreadID int64, chatType string, displayName string, username string) (string, string) {
	topicID := fmt.Sprintf("telegram:%d", chatID)
	if messageThreadID > 0 {
		topicID = fmt.Sprintf("telegram:%d:%d", chatID, messageThreadID)
	}
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

func telegramReplyToMessageLogFields(reply *telegramMessage) []any {
	if reply == nil {
		return []any{"reply_to_present", false}
	}
	text := strings.TrimSpace(messageTextOrCaption(reply))
	fields := []any{
		"reply_to_present", true,
		"reply_to_message_id", reply.MessageID,
		"reply_to_message_thread_id", reply.MessageThreadID,
		"reply_to_is_topic_message", reply.IsTopicMessage,
		"reply_to_forum_topic_created", isTelegramForumTopicRootMessage(reply),
		"reply_to_text_len", len(text),
		"reply_to_text_preview", daemonruntime.TruncateUTF8(text, 160),
		"reply_to_has_document", reply.Document != nil,
		"reply_to_photo_count", len(reply.Photo),
	}
	if reply.Chat != nil {
		fields = append(fields,
			"reply_to_chat_id", reply.Chat.ID,
			"reply_to_chat_type", strings.TrimSpace(reply.Chat.Type),
		)
	}
	if reply.From != nil {
		fields = append(fields,
			"reply_to_from_user_id", reply.From.ID,
			"reply_to_from_is_bot", reply.From.IsBot,
			"reply_to_from_username", strings.TrimSpace(reply.From.Username),
			"reply_to_from_first_name", strings.TrimSpace(reply.From.FirstName),
		)
	}
	return fields
}

func isTelegramForumTopicRootMessage(msg *telegramMessage) bool {
	return msg != nil && len(msg.ForumTopicCreated) > 0
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
