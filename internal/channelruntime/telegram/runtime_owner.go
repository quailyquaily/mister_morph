package telegram

import (
	"context"
	"errors"
	"fmt"
	htmlstd "html"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/agentpair"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	telegrambus "github.com/quailyquaily/mistermorph/internal/bus/adapters/telegram"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/imagehistory"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/filecache"
	"github.com/quailyquaily/mistermorph/internal/imagesession"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
	"github.com/quailyquaily/mistermorph/internal/taskdomain"
	"github.com/quailyquaily/mistermorph/internal/telegramutil"
	"github.com/quailyquaily/mistermorph/internal/textutil"
	"github.com/quailyquaily/mistermorph/internal/topiccontext"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	telegramtools "github.com/quailyquaily/mistermorph/tools/telegram"
)

const telegramFilesMaxBytes = int64(20 * 1024 * 1024)

func bootstrapTelegramRuntimeState(ctx context.Context, d Dependencies, opts RunOptions) (*telegramRuntimeState, error) {
	token := strings.TrimSpace(opts.BotToken)
	if token == "" {
		return nil, fmt.Errorf("missing telegram.bot_token (set via --telegram-bot-token or MISTER_MORPH_TELEGRAM_BOT_TOKEN)")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	logger, err := d.Logger()
	if err != nil {
		return nil, err
	}

	taskStore := opts.TaskStore
	if taskStore == nil {
		taskStore, err = daemonruntime.NewTaskViewForTarget("telegram", opts.Server.MaxQueue, daemonruntime.TaskViewConfig{
			PersistenceTargets: d.TaskPersistenceTargets,
			TasksDir:           d.RuntimePaths.TasksDir,
			JournalDir:         d.RuntimePaths.JournalDir,
			RotateMaxBytes:     d.TaskRotateMaxBytes,
		})
		if err != nil {
			return nil, err
		}
	}

	inprocBus, err := busruntime.StartInproc(busruntime.BootstrapOptions{
		MaxInFlight: opts.BusMaxInFlight,
		Logger:      logger,
		Component:   "telegram",
	})
	if err != nil {
		return nil, err
	}
	var runtimeGenerations *runtimecore.RuntimeGenerationManager
	ownerOwnsResources := false
	defer func() {
		if ownerOwnsResources {
			return
		}
		_ = inprocBus.Close()
		if runtimeGenerations != nil {
			runtimeGenerations.Close()
		}
	}()

	contactsStore := contacts.NewFileStore(d.RuntimePaths.ContactsDir)
	contactsService := contacts.NewService(contactsStore)
	workspaceStore := workspace.NewStore(d.RuntimePaths.WorkspaceAttachmentsPath)
	inboundAdapter, err := telegrambus.NewInboundAdapter(telegrambus.InboundAdapterOptions{
		Bus:   inprocBus,
		Store: contactsStore,
	})
	if err != nil {
		return nil, err
	}

	runtimeGenerations, err = runtimecore.BootstrapRuntimeGenerationManager(ctx, d.CommonDependencies, runtimecore.ChannelBootstrapOptions{
		Mode:              "telegram",
		InspectRequest:    opts.InspectRequest,
		InspectPrompt:     opts.InspectPrompt,
		AgentConfig:       opts.AgentLimits.ToConfig(),
		EngineToolsConfig: &opts.EngineToolsConfig,
		Logger:            logger,
	})
	if err != nil {
		return nil, err
	}
	api := newTelegramAPI(&http.Client{Timeout: 60 * time.Second}, "https://api.telegram.org", token)

	fileCacheDir := strings.TrimSpace(opts.FileCacheDir)
	if err := telegramutil.EnsureSecureCacheDir(fileCacheDir); err != nil {
		return nil, fmt.Errorf("telegram file cache dir: %w", err)
	}
	telegramCacheDir := filepath.Join(fileCacheDir, "telegram")
	if err := ensureSecureChildDir(fileCacheDir, telegramCacheDir); err != nil {
		return nil, fmt.Errorf("telegram cache subdir: %w", err)
	}
	protected, protectedErr := imagesession.NewStore(d.CommonDependencies.RuntimeToolsConfig.Image.FileStateDir).ProtectedPaths(fileCacheDir)
	if protectedErr != nil {
		logger.Warn("file_cache_protected_paths_error", "error", protectedErr.Error())
	}
	if err := filecache.Cleanup(telegramCacheDir, filecache.Limits{
		MaxAge:        opts.FileCacheMaxAge,
		MaxFiles:      opts.FileCacheMaxFiles,
		MaxTotalBytes: opts.FileCacheMaxTotalBytes,
	}, protected); err != nil {
		logger.Warn("file_cache_cleanup_error", "error", err.Error())
	}

	var me *telegramUser
	for {
		me, err = api.getMe(ctx)
		if err == nil {
			break
		}
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			logger.Info("telegram_stop", "reason", "context_canceled")
			return nil, nil
		}
		logger.Warn("telegram_get_me_error", "error", err.Error())
		select {
		case <-ctx.Done():
			logger.Info("telegram_stop", "reason", "context_canceled")
			return nil, nil
		case <-time.After(2 * time.Second):
		}
	}
	allowedChatIDs := make(map[int64]bool)
	for _, id := range normalizeAllowedChatIDs(opts.AllowedChatIDs) {
		if id != 0 {
			allowedChatIDs[id] = true
		}
	}
	adminValues := []string(nil)
	if d.AgentSettingsReader != nil {
		adminValues = d.AgentSettingsReader.GetStringSlice("admins")
	}
	admins, err := agentpair.ParseAdmins(adminValues)
	if err != nil {
		return nil, err
	}
	pairManager, err := agentpair.New(agentpair.Options{
		Context:               ctx,
		Self:                  telegramInboundAgentPeer(me.ID, me.Username, telegramDisplayName(me), 0),
		Admins:                admins,
		Contacts:              contactsService,
		JournalDir:            d.RuntimePaths.JournalDir,
		JournalRotateMaxBytes: d.TaskRotateMaxBytes,
		Logger:                logger,
		Send: func(sendCtx context.Context, target agentpair.Peer, body string) error {
			chatTarget, targetErr := telegramPairSendTarget(target)
			if targetErr != nil {
				return targetErr
			}
			return api.sendDirectText(sendCtx, chatTarget, body)
		},
	})
	if err != nil {
		return nil, err
	}
	state, err := newTelegramRuntimeState(telegramRuntimeStateConfig{
		ctx:                ctx,
		logger:             logger,
		dependencies:       d,
		options:            opts,
		taskStore:          taskStore,
		api:                api,
		allowedChatIDs:     allowedChatIDs,
		botUser:            me.Username,
		botID:              me.ID,
		inprocBus:          inprocBus,
		runtimeGenerations: runtimeGenerations,
		contactsService:    contactsService,
		pairManager:        pairManager,
		workspaceStore:     workspaceStore,
		inboundAdapter:     inboundAdapter,
	})
	ownerOwnsResources = true
	if err != nil {
		return nil, err
	}
	runtimeGenerations.Start(ctx)

	logger.Info("telegram_start",
		"base_url", "https://api.telegram.org",
		"bot_username", state.botUser,
		"bot_id", state.botID,
		"poll_timeout", opts.PollTimeout.String(),
		"task_timeout", opts.TaskTimeout.String(),
		"max_concurrency", opts.MaxConcurrency,
		"telegram_history_mode_cap_talkative", 16,
		"telegram_history_mode_cap_others", 8,
		"reactions_enabled", true,
		"group_trigger_mode", state.groupTriggerMode,
		"group_reply_policy", "humanlike",
		"addressing_confidence_threshold", opts.AddressingConfidenceThreshold,
		"addressing_interject_threshold", opts.AddressingInterjectThreshold,
		"telegram_history_cap", state.historyCap,
	)
	return state, nil
}

func (s *telegramRuntimeState) sendText(ctx context.Context, target any, text string, opts telegrambus.SendTextOptions) error {
	deliveryTarget, ok := target.(telegrambus.DeliveryTarget)
	if !ok {
		return fmt.Errorf("telegram target is invalid")
	}
	replyToMessageID := int64(0)
	if replyToRaw := strings.TrimSpace(opts.ReplyTo); replyToRaw != "" {
		parsed, err := strconv.ParseInt(replyToRaw, 10, 64)
		if err != nil || parsed <= 0 {
			return fmt.Errorf("telegram reply_to is invalid")
		}
		replyToMessageID = parsed
	}
	correlationID := strings.TrimSpace(opts.CorrelationID)
	if telegramOutboundKind(correlationID) == "plan_progress" {
		return s.sendPlanProgress(ctx, deliveryTarget.ChatID, deliveryTarget.MessageThreadID, text, replyToMessageID, correlationID)
	}
	return s.api.sendMessageChunkedReplyInThread(ctx, deliveryTarget.ChatID, deliveryTarget.MessageThreadID, text, replyToMessageID)
}

func (s *telegramRuntimeState) sendPlanProgress(ctx context.Context, chatID, messageThreadID int64, text string, replyToMessageID int64, correlationID string) error {
	line := strings.TrimSpace(text)
	if line == "" {
		return nil
	}
	progressKey := telegramConversationMapKey(chatID, messageThreadID)
	s.planProgressMu.Lock()
	state := s.planProgressByID[progressKey]
	s.planProgressMu.Unlock()
	nextState, rendered := nextTelegramPlanProgressState(state, correlationID, line)
	if rendered == "" {
		return nil
	}
	if nextState.MessageID > 0 && strings.EqualFold(nextState.CorrelationID, correlationID) {
		if err := s.api.editMessageHTML(ctx, chatID, nextState.MessageID, rendered, true); err == nil || isTelegramMessageNotModified(err) {
			s.planProgressMu.Lock()
			s.planProgressByID[progressKey] = nextState
			s.planProgressMu.Unlock()
			return nil
		} else {
			s.logger.Warn("telegram_plan_progress_edit_failed", "chat_id", chatID, "message_id", nextState.MessageID, "correlation_id", correlationID, "error", err.Error())
		}
	}
	messageID, err := s.api.sendMessageChunkedReplyInThreadWithFirstMessageID(ctx, chatID, messageThreadID, rendered, replyToMessageID)
	if err != nil {
		return err
	}
	if messageID > 0 && correlationID != "" {
		nextState.MessageID = messageID
		s.planProgressMu.Lock()
		s.planProgressByID[progressKey] = nextState
		s.planProgressMu.Unlock()
	}
	return nil
}

func (s *telegramRuntimeState) publishText(ctx context.Context, chatID, messageThreadID int64, text, correlationID string) error {
	_, err := publishTelegramBusOutbound(ctx, s.inprocBus, chatID, messageThreadID, text, "", correlationID)
	if err != nil {
		callErrorHook(ctx, s.logger, s.options.Hooks, ErrorEvent{Stage: ErrorStagePublishOutbound, ChatID: chatID, Err: err})
	}
	return err
}

func (s *telegramRuntimeState) runJob(workerCtx context.Context, conversationKey string, job telegramJob) {
	retainGeneration := false
	defer func() {
		if !retainGeneration {
			job.releaseGeneration()
		}
	}()
	if workerCtx.Err() != nil {
		s.finalizeRuntimeClosedJob(conversationKey, job)
		return
	}
	runtimeBundle := job.runtimeBundle(&s.sharedRuntime)
	if runtimeBundle == nil || runtimeBundle.TaskRuntime == nil {
		s.finalizeAcceptedTask(job.TaskID, daemonruntime.TaskFailed, "telegram runtime generation is unavailable")
		return
	}
	chatID := job.ChatID
	s.stateMu.Lock()
	history := append([]chathistory.ChatHistoryItem(nil), s.history[conversationKey]...)
	stickySkills := append([]string(nil), s.stickySkillsByChat[conversationKey]...)
	s.stateMu.Unlock()
	currentVersion := s.runner.CurrentVersion(conversationKey)
	if job.Version != currentVersion {
		history = nil
	}

	typingStop := startTypingTickerInThread(workerCtx, s.api, chatID, job.MessageThreadID, "typing", 4*time.Second)
	defer typingStop()
	if err := runtimecore.MarkTaskRunning(s.taskStore, job.TaskID); err != nil {
		s.logger.Error("telegram_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskRunning, "error", err.Error())
		return
	}
	defer func() {
		if workerCtx.Err() != nil {
			s.finalizeRuntimeClosedJob(conversationKey, job)
		}
	}()
	lease, err := s.runControl.StartLease(workerCtx, s.options.TaskTimeout, runtimecontrol.ActiveRun{
		Runtime:         "telegram",
		ConversationKey: conversationKey,
		TopicID:         telegramContextTopicID(job),
		TaskID:          job.TaskID,
		RunID:           job.TaskID,
	})
	if err != nil {
		if stateErr := runtimecore.MarkTaskFailed(s.taskStore, job.TaskID, strings.TrimSpace(err.Error()), false); stateErr != nil {
			s.logger.Error("telegram_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskFailed, "error", stateErr.Error())
		}
		return
	}
	runCtx := taskruntime.WithContextCompactionNotification(lease.Context, s.logger, func(notifyCtx context.Context, event agent.Event, text string) error {
		correlationID := fmt.Sprintf("telegram:context-compaction:%s:%d", job.TaskID, event.Step)
		return s.publishText(notifyCtx, chatID, job.MessageThreadID, text, correlationID)
	})
	runtimeOpts := runtimeTaskOptions{
		FileCacheDir: s.options.FileCacheDir,
	}
	final, _, loadedSkills, reaction, runErr := runTelegramTask(
		runCtx,
		runtimeBundle.TaskRuntime,
		s.api,
		s.options.FileCacheDir,
		telegramFilesMaxBytes,
		s.allowedChatIDs,
		job,
		s.botUser,
		history,
		stickySkills,
		s.options.RequestTimeout,
		runtimeOpts,
		lease.SteerQueue,
		s.publishText,
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
		if stateErr := runtimecore.MarkTaskFailed(s.taskStore, job.TaskID, displayErr, isTaskContextCanceled(runErr) || userStopped); stateErr != nil {
			s.logger.Error("telegram_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskFailed, "error", stateErr.Error())
		}
		callErrorHook(workerCtx, s.logger, s.options.Hooks, ErrorEvent{
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
		if _, publishErr := publishTelegramBusOutbound(workerCtx, s.inprocBus, chatID, job.MessageThreadID, errorText, "", errorCorrelationID); publishErr != nil {
			s.logger.Warn("telegram_bus_publish_error", "channel", busruntime.ChannelTelegram, "chat_id", chatID, "bus_error_code", string(busruntime.ErrorCodeOf(publishErr)), "error", publishErr.Error())
			callErrorHook(workerCtx, s.logger, s.options.Hooks, ErrorEvent{
				Stage:     ErrorStagePublishErrorReply,
				ChatID:    chatID,
				MessageID: job.MessageID,
				Err:       publishErr,
			})
		}
		return
	}

	if pendingID, ok := runtimecore.PendingApprovalID(final); ok {
		pendingAt := time.Now().UTC()
		if s.taskStore != nil {
			if err := s.taskStore.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
				info.Status = daemonruntime.TaskPending
				info.PendingAt = &pendingAt
				info.ApprovalRequestID = pendingID
				info.Result = map[string]any{"source": "telegram", "final": final}
			}); err != nil {
				s.logger.Error("telegram_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskPending, "error", err.Error())
				return
			}
		}
		if err := s.registerPendingApproval(pendingID, job); err != nil {
			applied, stateErr := runtimecore.FailPendingApprovalTask(s.taskStore, job.TaskID, pendingID, runtimecore.ApprovalRegistrationFailedTaskError)
			if stateErr != nil {
				err = errors.Join(err, stateErr)
			}
			s.logger.Error("telegram_approval_register_error", "approval_request_id", pendingID, "task_id", job.TaskID, "task_failed", applied, "error", err.Error())
			return
		}
		if err := s.notifyPendingApproval(context.Background(), pendingID, job); err != nil {
			s.logger.Warn("telegram_approval_notify_error", "approval_request_id", pendingID, "chat_id", job.ChatID, "error", err.Error())
		}
		retainGeneration = true
		return
	}

	outText := outputfmt.FormatFinalOutput(final)
	publishText := shouldPublishTelegramText(final)
	if err := runtimecore.MarkTaskDone(s.taskStore, job.TaskID, outText); err != nil {
		s.logger.Error("telegram_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskDone, "error", err.Error())
		return
	}
	if publishText {
		outCorrelationID := fmt.Sprintf("telegram:message:%d:%d", chatID, job.MessageID)
		if workerCtx.Err() != nil {
			return
		}
		replyTo := ""
		if job.ReplyToMessageID > 0 {
			replyTo = strconv.FormatInt(job.ReplyToMessageID, 10)
		}
		if _, err := publishTelegramBusOutbound(workerCtx, s.inprocBus, chatID, job.MessageThreadID, outText, replyTo, outCorrelationID); err != nil {
			s.logger.Warn("telegram_bus_publish_error", "channel", busruntime.ChannelTelegram, "chat_id", chatID, "bus_error_code", string(busruntime.ErrorCodeOf(err)), "error", err.Error())
			callErrorHook(workerCtx, s.logger, s.options.Hooks, ErrorEvent{
				Stage:     ErrorStagePublishOutbound,
				ChatID:    chatID,
				MessageID: job.MessageID,
				Err:       err,
			})
		}
	}

	s.stateMu.Lock()
	latestVersion := s.runner.CurrentVersion(conversationKey)
	contextCompactionOnly := chatcommands.IsContextCompactCommand(job.Text)
	if latestVersion != currentVersion {
		s.history[conversationKey] = nil
		s.stickySkillsByChat[conversationKey] = nil
	}
	if !contextCompactionOnly && latestVersion == currentVersion && len(loadedSkills) > 0 {
		s.stickySkillsByChat[conversationKey] = capUniqueStrings(loadedSkills, telegramStickySkillsCap)
	}
	if !contextCompactionOnly {
		current := s.history[conversationKey]
		inboundHistory := newTelegramInboundHistoryItem(job)
		if publishText {
			inboundHistory.Images = imagehistory.WithDescription(inboundHistory.Images, outText, "agent_final")
		}
		current = append(current, inboundHistory)
		if reaction != nil {
			note := "[reacted]"
			if emoji := strings.TrimSpace(reaction.Emoji); emoji != "" {
				note = "[reacted: " + emoji + "]"
			}
			current = append(current, newTelegramOutboundReactionHistoryItem(chatID, job.ChatType, note, reaction.Emoji, time.Now().UTC(), s.botUser))
		}
		if publishText {
			current = append(current, newTelegramOutboundAgentHistoryItem(chatID, job.ChatType, outText, time.Now().UTC(), s.botUser))
		}
		s.history[conversationKey] = trimChatHistoryItems(current, s.historyCap)
	}
	s.stateMu.Unlock()
}

func (s *telegramRuntimeState) enqueueInbound(ctx context.Context, message busruntime.BusMessage) error {
	if ctx == nil {
		ctx = s.workersCtx
	}
	inbound, err := telegrambus.InboundMessageFromBusMessage(message)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(inbound.Text)
	if text == "" {
		return fmt.Errorf("telegram inbound text is required")
	}
	if inbound.FromIsAgent && !s.agentInteractions.Allow(message.ConversationKey, time.Now().UTC()) {
		s.logger.Warn("agent_interaction_rate_limited",
			"channel", "telegram",
			"conversation_key", message.ConversationKey,
			"limit", runtimecore.AgentInteractionLimit,
			"window", runtimecore.AgentInteractionWindow,
		)
		return nil
	}
	s.stateMu.Lock()
	s.lastActivity[inbound.ChatID] = time.Now()
	if inbound.FromUserID > 0 {
		s.lastFromUser[inbound.ChatID] = inbound.FromUserID
		if inbound.FromUsername != "" {
			s.lastFromUsername[inbound.ChatID] = inbound.FromUsername
		}
		if inbound.FromDisplayName != "" {
			s.lastFromName[inbound.ChatID] = inbound.FromDisplayName
		}
		if inbound.FromFirstName != "" {
			s.lastFromFirst[inbound.ChatID] = inbound.FromFirstName
		}
		if inbound.FromLastName != "" {
			s.lastFromLast[inbound.ChatID] = inbound.FromLastName
		}
	}
	if inbound.ChatType != "" {
		s.lastChatType[inbound.ChatID] = inbound.ChatType
	}
	s.stateMu.Unlock()

	imagePaths := busruntime.ImagePathsFromAttachments(inbound.ImageAttachments)
	s.logger.Info("telegram_task_enqueued",
		"channel", message.Channel,
		"topic", message.Topic,
		"chat_id", inbound.ChatID,
		"type", inbound.ChatType,
		"idempotency_key", message.IdempotencyKey,
		"conversation_key", message.ConversationKey,
		"text_len", len(text),
		"image_count", len(inbound.ImageAttachments),
	)
	workspaceResolution, err := workspace.Resolve(s.workspaceStore, message.ConversationKey, s.dependencies.DefaultWorkspaceDir)
	if err != nil {
		return err
	}
	workspaceDir := workspaceResolution.WorkspaceDir
	images := imagehistory.BuildFromAttachments(inbound.ImageAttachments, pathroots.New(workspaceDir, s.options.FileCacheDir, ""))
	jobTaskID := telegramTaskID(inbound.ChatID, inbound.MessageThreadID, inbound.MessageID)
	generationLease, runtimeBundle, err := s.captureRuntimeGeneration()
	if err != nil {
		return err
	}
	transferredGeneration := false
	defer func() {
		if !transferredGeneration && generationLease != nil {
			generationLease.Release()
		}
	}()
	taskRoute, err := runtimeBundle.TaskRuntime.ResolveTaskRouteForRun(llmstats.WithRunID(ctx, jobTaskID), text)
	if err != nil {
		return err
	}
	buildJob := func(version uint64) telegramJob {
		admittedRoute := taskRoute
		return telegramJob{
			TaskID:           jobTaskID,
			ConversationKey:  message.ConversationKey,
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
			FromIsAgent:      inbound.FromIsAgent,
			Text:             text,
			ImagePaths:       imagePaths,
			Images:           append([]chathistory.ChatHistoryImage(nil), images...),
			WorkspaceDir:     workspaceDir,
			Route:            &admittedRoute,
			Version:          version,
			MentionUsers:     append([]string(nil), inbound.MentionUsers...),
			Generation:       generationLease,
		}
	}
	if s.taskStore != nil {
		createdAt := inbound.SentAt.UTC()
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		topicID, topicTitle := telegramManagedTopicInfo(inbound.ChatID, inbound.MessageThreadID, inbound.ChatType, inbound.FromDisplayName, inbound.FromUsername)
		if err := recordTelegramQueuedTask(s.taskStore, daemonruntime.TaskInfo{
			ID:           jobTaskID,
			Status:       daemonruntime.TaskQueued,
			Task:         textutil.TruncateRunes(text, 2000),
			Model:        strings.TrimSpace(taskRoute.ClientConfig.Model),
			Timeout:      s.options.TaskTimeout.String(),
			CreatedAt:    createdAt,
			TopicID:      topicID,
			Conversation: telegramTaskConversation(message.ConversationKey, inbound, s.botUser, s.botID),
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
		}, topicTitle); err != nil {
			return err
		}
	}
	if err := s.runner.Enqueue(ctx, message.ConversationKey, buildJob); err != nil {
		if stateErr := runtimecore.MarkTaskFailed(s.taskStore, jobTaskID, strings.TrimSpace(err.Error()), taskdomain.EndedByCancellation(ctx, err)); stateErr != nil {
			return fmt.Errorf("enqueue telegram task: %v; persist failed state: %w", err, stateErr)
		}
		return err
	}
	transferredGeneration = true
	callInboundHook(ctx, s.logger, s.options.Hooks, InboundEvent{
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

func (s *telegramRuntimeState) deliverOutbound(ctx context.Context, message busruntime.BusMessage) error {
	_, _, err := s.deliveryAdapter.Deliver(ctx, message)
	if err != nil {
		chatID, _, _ := busruntime.ParseTelegramConversationKey(message.ConversationKey)
		callErrorHook(ctx, s.logger, s.options.Hooks, ErrorEvent{Stage: ErrorStageDeliverOutbound, ChatID: chatID, Err: err})
		return err
	}
	event, eventErr := telegramOutboundEventFromBusMessage(message)
	if eventErr != nil {
		callErrorHook(ctx, s.logger, s.options.Hooks, ErrorEvent{Stage: ErrorStageDeliverOutbound, ChatID: event.ChatID, Err: eventErr})
	} else {
		callOutboundHook(ctx, s.logger, s.options.Hooks, event)
	}
	return nil
}

func (s *telegramRuntimeState) handleBusMessage(ctx context.Context, message busruntime.BusMessage) error {
	switch message.Direction {
	case busruntime.DirectionInbound:
		if message.Channel != busruntime.ChannelTelegram {
			return fmt.Errorf("unsupported inbound channel: %s", message.Channel)
		}
		if err := s.contactsService.ObserveInboundBusMessage(context.Background(), message, time.Now().UTC()); err != nil {
			s.logger.Warn("contacts_observe_bus_error", "channel", message.Channel, "idempotency_key", message.IdempotencyKey, "error", err.Error())
		}
		return s.enqueueInbound(ctx, message)
	case busruntime.DirectionOutbound:
		if message.Channel != busruntime.ChannelTelegram {
			return fmt.Errorf("unsupported outbound channel: %s", message.Channel)
		}
		return s.deliverOutbound(ctx, message)
	default:
		return fmt.Errorf("unsupported direction: %s", message.Direction)
	}
}

func (s *telegramRuntimeState) poll() error {
	for {
		updates, nextOffset, err := s.api.getUpdates(s.ctx, s.offset, s.options.PollTimeout)
		if err != nil {
			if errors.Is(err, context.Canceled) || s.ctx.Err() != nil {
				s.logger.Info("telegram_stop", "reason", "context_canceled")
				return nil
			}
			if isTelegramPollTimeoutError(err) {
				s.logger.Debug("telegram_get_updates_timeout", "error", err.Error())
			} else {
				s.logger.Warn("telegram_get_updates_error", "error", err.Error())
			}
			select {
			case <-s.ctx.Done():
				return nil
			case <-time.After(time.Second):
			}
			continue
		}
		s.offset = nextOffset
		for _, update := range updates {
			s.handleUpdate(update)
		}
	}
}

func (s *telegramRuntimeState) handleUpdate(update telegramUpdate) {
	if s.handleApprovalCallback(context.Background(), update.CallbackQuery) {
		return
	}
	message := update.Message
	if message == nil {
		message = update.EditedMessage
	}
	if message == nil {
		message = update.ChannelPost
	}
	if message == nil {
		message = update.EditedChannelPost
	}
	if message == nil || message.Chat == nil {
		return
	}
	chatID := message.Chat.ID
	messageThreadID := message.MessageThreadID
	conversationKey, err := busruntime.BuildTelegramTopicConversationKey(strconv.FormatInt(chatID, 10), messageThreadID)
	if err != nil {
		s.logger.Warn("telegram_conversation_key_error", "chat_id", chatID, "message_thread_id", messageThreadID, "error", err.Error())
		return
	}
	text := strings.TrimSpace(messageTextOrCaption(message))
	rawText := text

	fromUserID := int64(0)
	fromUsername := ""
	fromFirst := ""
	fromLast := ""
	fromDisplay := ""
	fromIsAgent := false
	if message.From != nil {
		fromUserID = message.From.ID
		fromUsername = strings.TrimSpace(message.From.Username)
		fromFirst = strings.TrimSpace(message.From.FirstName)
		fromLast = strings.TrimSpace(message.From.LastName)
		fromDisplay = telegramDisplayName(message.From)
		fromIsAgent = message.From.IsBot
	}
	chatType := strings.ToLower(strings.TrimSpace(message.Chat.Type))
	isGroup := chatType == "group" || chatType == "supergroup"
	messageSentAt := telegramMessageSentAt(message)
	if fromIsAgent && (fromUserID == s.botID || fromUsername != "" && strings.EqualFold(fromUsername, s.botUser)) {
		return
	}
	commandWord, commandArgs := chatcommands.ParseCommand(text)
	normalizedCommand := chatcommands.NormalizeCommand(commandWord)
	if agentpair.IsControlMessage(rawText) {
		if s.pairManager == nil || isGroup || !fromIsAgent {
			s.logger.Warn("agent_pair_failed",
				"channel", "telegram",
				"chat_id", chatID,
				"message_id", message.MessageID,
				"reason", "invalid_control_sender_or_scope",
			)
			return
		}
		peer := telegramInboundAgentPeer(fromUserID, fromUsername, fromDisplay, chatID)
		_, handled, pairErr := s.pairManager.Handle(context.Background(), peer, rawText)
		if pairErr != nil {
			s.logger.Warn("agent_pair_failed", "channel", "telegram", "chat_id", chatID, "message_id", message.MessageID, "peer_agent_id", peer.ID, "reason", "offer_rejected", "error", pairErr.Error())
		}
		if handled {
			return
		}
	}
	if normalizedCommand == "/pair" {
		if isGroup || fromIsAgent || s.pairManager == nil {
			s.logger.Warn("agent_pair_failed",
				"channel", "telegram",
				"chat_id", chatID,
				"message_id", message.MessageID,
				"reason", "pair_command_requires_private_human_sender",
			)
			return
		}
		target, targetErr := telegramPairTarget(commandArgs)
		var status agentpair.Status
		if targetErr == nil {
			adminReference := ""
			if fromUsername != "" {
				adminReference = "tg:@" + fromUsername
			}
			status, targetErr = s.pairManager.Start(context.Background(), "tg:"+strconv.FormatInt(fromUserID, 10), target, adminReference)
		} else {
			s.logger.Warn("agent_pair_failed", "channel", "telegram", "chat_id", chatID, "message_id", message.MessageID, "reason", "invalid_target", "error", targetErr.Error())
		}
		sendTelegramPairReply(context.Background(), s.api, chatID, messageThreadID, status, targetErr)
		return
	}
	pairedAgent := false
	if fromIsAgent && !isGroup && s.pairManager != nil {
		peer := telegramInboundAgentPeer(fromUserID, fromUsername, fromDisplay, chatID)
		pairedAgent, err = s.pairManager.IsPaired(context.Background(), peer)
		if err != nil {
			s.logger.Warn("telegram_agent_pair_lookup_failed", "chat_id", chatID, "message_id", message.MessageID, "peer_agent_id", peer.ID, "error", err.Error())
			pairedAgent = false
		}
	}
	if fromIsAgent && !isGroup {
		s.logger.Debug("telegram_private_agent_message_received",
			"chat_id", chatID,
			"message_id", message.MessageID,
			"text", rawText,
		)
	}
	chatAuthorized := telegramChatAuthorized(s.allowedChatIDs, chatID, isGroup, fromIsAgent, pairedAgent)
	if fromIsAgent && !chatAuthorized {
		s.logger.Warn("telegram_unauthorized_chat",
			"chat_id", chatID,
			"message_id", message.MessageID,
			"from_is_agent", true,
		)
		return
	}
	var mentionCandidates []string
	if isGroup {
		mentionCandidates = collectMentionCandidates(message, s.botUser)
		if len(mentionCandidates) > 0 {
			s.stateMu.Lock()
			addKnownUsernames(s.knownMentions, chatID, mentionCandidates)
			s.stateMu.Unlock()
		}
	}
	mentionParticipants := collectTelegramTaskParticipants(message, s.botUser, s.botID)
	appendIgnoredInboundHistory := func(ignoredText string) {
		ignoredText = strings.TrimSpace(ignoredText)
		if ignoredText == "" && messageHasDownloadableFile(message) {
			ignoredText = "[attachment]"
		}
		if message.ReplyTo != nil {
			if quoted := buildReplyContext(message.ReplyTo); quoted != "" {
				if ignoredText == "" {
					ignoredText = "(empty)"
				}
				ignoredText = "Quoted message:\n> " + quoted + "\n\nUser request:\n" + ignoredText
			}
		}
		s.stateMu.Lock()
		current := s.history[conversationKey]
		current = append(current, newTelegramInboundHistoryItem(telegramJob{
			ChatID:          chatID,
			MessageThreadID: messageThreadID,
			MessageID:       message.MessageID,
			SentAt:          messageSentAt,
			ChatType:        chatType,
			FromUserID:      fromUserID,
			FromUsername:    fromUsername,
			FromFirstName:   fromFirst,
			FromLastName:    fromLast,
			FromDisplayName: fromDisplay,
			FromIsAgent:     fromIsAgent,
			Text:            ignoredText,
		}))
		s.history[conversationKey] = trimChatHistoryItems(current, s.historyCap)
		s.stateMu.Unlock()
	}
	recordUntriggered := func() {
		if s.untriggeredRecorder == nil || message.From == nil || message.From.IsBot {
			return
		}
		senderID := ""
		if message.From.ID != 0 {
			senderID = strconv.FormatInt(message.From.ID, 10)
		}
		if err := s.untriggeredRecorder.Record(runtimecore.UntriggeredMessage{
			Channel:         string(busruntime.ChannelTelegram),
			ConversationKey: conversationKey,
			MessageID:       strconv.FormatInt(message.MessageID, 10),
			SenderID:        senderID,
			SentAt:          messageSentAt,
			Text:            rawText,
			HasAttachment:   messageHasDownloadableFile(message),
		}); err != nil {
			s.logger.Error("telegram_untriggered_journal_append_error", "chat_id", chatID, "message_id", message.MessageID, "error", err.Error())
		}
	}
	firstMentionFound, firstMentionTargetsSelf := telegramFirstBodyMentionTargetsSelf(message, s.botUser, s.botID)
	if shouldIgnoreTelegramFirstMention(isGroup, fromIsAgent, firstMentionFound, firstMentionTargetsSelf) {
		s.logger.Info("telegram_message_ignored_first_mention",
			"chat_id", chatID,
			"message_id", message.MessageID,
			"from_is_agent", fromIsAgent,
			"first_mention_found", firstMentionFound,
		)
		if strings.EqualFold(s.groupTriggerMode, "talkative") {
			appendIgnoredInboundHistory(rawText)
		}
		recordUntriggered()
		return
	}

	contextCompactionOnly := chatcommands.IsContextCompactCommand(text)
	replyToMessageID := int64(0)
	switch normalizedCommand {
	case "/stop":
		if !chatAuthorized {
			s.logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
			sendTelegramUnauthorizedMessage(s.api, chatID, messageThreadID, chatType)
			return
		}
		result := s.runControl.Stop("telegram", conversationKey, "/stop")
		_ = s.api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, htmlstd.EscapeString(runtimecontrol.StopFeedback(result.Found)), true)
		return
	case "/help":
		help := "Send a message and I will run it as an agent task.\n" +
			"Commands: /think, /stop, /models, /skills, /ctx, /ctx compact, /workspace, /reset, /id\n\n" +
			"Group chats: reply to me, or mention @" + s.botUser + ".\n" +
			"You can also send a file (document/photo). It will be downloaded under file_cache_dir/telegram/ and the agent can process it.\n" +
			"Note: if Bot Privacy Mode is enabled, I may not receive normal group messages."
		_ = s.api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, help, true)
		return
	case "/models":
		if !chatAuthorized {
			s.logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
			sendTelegramUnauthorizedMessage(s.api, chatID, messageThreadID, chatType)
			return
		}
		if executeTelegramProfileCommand(s.dependencies, s.api, chatID, messageThreadID, text) {
			return
		}
		_ = s.api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, "error: "+htmlstd.EscapeString("missing llm profile command handler"), true)
		return
	case "/skills":
		if !chatAuthorized {
			s.logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
			sendTelegramUnauthorizedMessage(s.api, chatID, messageThreadID, chatType)
			return
		}
		s.stateMu.Lock()
		currentSkills := append([]string(nil), s.stickySkillsByChat[conversationKey]...)
		s.stateMu.Unlock()
		if executeTelegramSkillCommand(s.dependencies, s.api, chatID, messageThreadID, currentSkills) {
			return
		}
		_ = s.api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, "error: "+htmlstd.EscapeString("missing skill command handler"), true)
		return
	case "/ctx":
		if !chatAuthorized {
			s.logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
			sendTelegramUnauthorizedMessage(s.api, chatID, messageThreadID, chatType)
			return
		}
		if contextCompactionOnly {
			break
		}
		reply, commandErr := topiccontext.NewStore(s.dependencies.RuntimePaths.TopicContextPath).RenderCommandText(conversationKey)
		if commandErr != nil {
			reply = "error: " + strings.TrimSpace(commandErr.Error())
		}
		_ = s.api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, htmlstd.EscapeString(reply), true)
		return
	case "/id":
		idText := fmt.Sprintf("chat_id=%d type=%s", chatID, chatType)
		if messageThreadID > 0 {
			idText += fmt.Sprintf(" message_thread_id=%d", messageThreadID)
		}
		_ = s.api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, idText, true)
		return
	case "/workspace":
		if !chatAuthorized {
			s.logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
			sendTelegramUnauthorizedMessage(s.api, chatID, messageThreadID, chatType)
			return
		}
		result, commandErr := workspace.ExecuteStoreCommand(s.workspaceStore, conversationKey, commandArgs, s.dependencies.DefaultWorkspaceDir, nil)
		reply := result.Reply
		if commandErr != nil {
			reply = "error: " + strings.TrimSpace(commandErr.Error())
		}
		_ = s.api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, htmlstd.EscapeString(reply), true)
		return
	case "/think":
		if !chatAuthorized {
			s.logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
			sendTelegramUnauthorizedMessage(s.api, chatID, messageThreadID, chatType)
			return
		}
	case "/reset":
		if !chatAuthorized {
			s.logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
			sendTelegramUnauthorizedMessage(s.api, chatID, messageThreadID, chatType)
			return
		}
		s.runControl.Stop("telegram", conversationKey, "/reset")
		generationLease, runtimeBundle, captureErr := s.captureRuntimeGeneration()
		if captureErr != nil {
			_ = s.api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, htmlstd.EscapeString("error: "+captureErr.Error()), true)
			return
		}
		resetErr := runtimeBundle.TaskRuntime.ResetContextHistory(context.Background(), conversationKey)
		if generationLease != nil {
			generationLease.Release()
		}
		if resetErr != nil {
			_ = s.api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, htmlstd.EscapeString("error: "+resetErr.Error()), true)
			return
		}
		s.stateMu.Lock()
		delete(s.history, conversationKey)
		delete(s.stickySkillsByChat, conversationKey)
		delete(s.knownMentions, chatID)
		s.runner.IncrementVersion(conversationKey)
		s.stateMu.Unlock()
		s.planProgressMu.Lock()
		delete(s.planProgressByID, conversationKey)
		s.planProgressMu.Unlock()
		_ = s.api.sendMessageHTMLInThread(context.Background(), chatID, messageThreadID, "ok (reset)", true)
		return
	default:
		if !chatAuthorized {
			s.logger.Warn("telegram_unauthorized_chat", "chat_id", chatID)
			sendTelegramUnauthorizedMessage(s.api, chatID, messageThreadID, chatType)
			return
		}
		if isGroup {
			if shouldSkipGroupReplyWithoutBodyMention(message, text, s.botUser, s.botID) {
				logFields := []any{
					"chat_id", chatID,
					"message_id", message.MessageID,
					"message_thread_id", messageThreadID,
					"is_topic_message", message.IsTopicMessage,
					"type", chatType,
					"text_len", len(text),
					"entities_count", len(message.Entities),
					"caption_entities_count", len(message.CaptionEntities),
				}
				logFields = append(logFields, telegramReplyToMessageLogFields(message.ReplyTo)...)
				s.logger.Info("telegram_group_ignored_reply_without_at_mention", logFields...)
				appendIgnoredInboundHistory(rawText)
				recordUntriggered()
				return
			}
			s.stateMu.Lock()
			historySnapshot := append([]chathistory.ChatHistoryItem(nil), s.history[conversationKey]...)
			s.stateMu.Unlock()
			var addressingReactionTool *telegramtools.ReactTool
			if s.api != nil && message.MessageID > 0 {
				addressingReactionTool = telegramtools.NewReactTool(newTelegramToolAPI(s.api), chatID, message.MessageID, s.allowedChatIDs)
			}
			decisionCtx := context.Background()
			if message.MessageID > 0 {
				decisionCtx = llmstats.WithRunID(decisionCtx, telegramTaskID(chatID, messageThreadID, message.MessageID))
			}
			generationLease, runtimeBundle, captureErr := s.captureRuntimeGeneration()
			if captureErr != nil {
				s.logger.Warn("telegram_runtime_generation_unavailable", "error", captureErr.Error())
				return
			}
			addressingTimeout := runtimeBundle.AddressingRoute.ClientConfig.RequestTimeout
			if addressingTimeout <= 0 {
				addressingTimeout = s.options.RequestTimeout
			}
			decision, accepted, decisionErr := groupTriggerDecision(
				decisionCtx,
				runtimeBundle.AddressingClient,
				runtimeBundle.AddressingModel,
				message,
				s.botUser,
				s.botID,
				s.groupTriggerMode,
				addressingTimeout,
				s.options.AddressingConfidenceThreshold,
				s.options.AddressingInterjectThreshold,
				historySnapshot,
				addressingReactionTool,
				s.dependencies.RuntimePaths.PersonaDir,
			)
			if generationLease != nil {
				generationLease.Release()
			}
			if addressingReactionTool != nil {
				if reaction := addressingReactionTool.LastReaction(); reaction != nil {
					s.logger.Info("telegram_group_addressing_reaction_applied", "chat_id", reaction.ChatID, "message_id", reaction.MessageID, "emoji", reaction.Emoji, "source", reaction.Source)
				}
			}
			if decisionErr != nil {
				s.logger.Warn("telegram_addressing_llm_error", "chat_id", chatID, "type", chatType, "error", decisionErr.Error())
				return
			}
			if !accepted {
				s.logger.Info("telegram_group_ignored",
					"chat_id", chatID,
					"type", chatType,
					"text_len", len(text),
					"llm_attempted", decision.AddressingLLMAttempted,
					"llm_ok", decision.AddressingLLMOK,
					"llm_addressed", decision.Addressing.Addressed,
					"confidence", decision.Addressing.Confidence,
					"wanna_interject", decision.Addressing.WannaInterject,
					"interject", decision.Addressing.Interject,
					"impulse", decision.Addressing.Impulse,
					"is_lightweight", decision.Addressing.IsLightweight,
					"reason", decision.Reason,
				)
				if strings.EqualFold(s.groupTriggerMode, "talkative") {
					appendIgnoredInboundHistory(rawText)
				}
				recordUntriggered()
				return
			}
			replyToMessageID = quoteReplyMessageIDForGroupTrigger(message, decision)
			s.logger.Info("telegram_group_trigger",
				"chat_id", chatID,
				"type", chatType,
				"reason", decision.Reason,
				"llm_addressed", decision.Addressing.Addressed,
				"confidence", decision.Addressing.Confidence,
				"wanna_interject", decision.Addressing.WannaInterject,
				"interject", decision.Addressing.Interject,
				"impulse", decision.Addressing.Impulse,
				"is_lightweight", decision.Addressing.IsLightweight,
				"quote_reply", replyToMessageID > 0,
			)
			text = strings.TrimSpace(rawText)
			if text == "" && !messageHasDownloadableFile(message) && message.ReplyTo == nil {
				return
			}
		} else if text == "" && !messageHasDownloadableFile(message) {
			return
		}
	}

	var downloaded []telegramDownloadedFile
	downloadRoots := pathroots.New("", s.options.FileCacheDir, "")
	if messageHasDownloadableFile(message) || (message.ReplyTo != nil && messageHasDownloadableFile(message.ReplyTo)) {
		downloadDir := filepath.Join(s.options.FileCacheDir, "telegram")
		if key, keyErr := busruntime.BuildTelegramTopicConversationKey(strconv.FormatInt(chatID, 10), messageThreadID); keyErr == nil {
			if resolution, workspaceErr := workspace.Resolve(s.workspaceStore, key, s.dependencies.DefaultWorkspaceDir); workspaceErr == nil {
				downloadRoots = pathroots.New(resolution.WorkspaceDir, s.options.FileCacheDir, "")
				if dir, dirErr := imagehistory.DownloadDir(s.options.FileCacheDir, resolution.WorkspaceDir, chathistory.ChannelTelegram); dirErr == nil {
					downloadDir = dir
				} else {
					s.logger.Warn("telegram_image_download_dir_error", "conversation_key", key, "error", dirErr.Error())
				}
			}
		}
		downloadCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		downloaded, err = downloadTelegramMessageFiles(downloadCtx, s.api, downloadDir, telegramFilesMaxBytes, message, chatID)
		cancel()
		if err != nil {
			correlationID := fmt.Sprintf("telegram:file_download_error:%d:%d", chatID, message.MessageID)
			if _, publishErr := publishTelegramBusOutbound(context.Background(), s.inprocBus, chatID, messageThreadID, "file download error: "+err.Error(), "", correlationID); publishErr != nil {
				s.logger.Warn("telegram_bus_publish_error", "channel", busruntime.ChannelTelegram, "chat_id", chatID, "message_id", message.MessageID, "bus_error_code", string(busruntime.ErrorCodeOf(publishErr)), "error", publishErr.Error())
				callErrorHook(context.Background(), s.logger, s.options.Hooks, ErrorEvent{Stage: ErrorStagePublishFileDownloadError, ChatID: chatID, MessageID: message.MessageID, Err: publishErr})
			}
			return
		}
	}
	if text == "" && len(downloaded) > 0 {
		text = "Please process the uploaded file(s)."
	}
	if len(downloaded) > 0 {
		text = appendDownloadedFilesToTask(text, downloaded, downloadRoots)
	}
	imageAttachments := collectDownloadedImageAttachments(downloaded, 3)
	if message.ReplyTo != nil {
		if quoted := buildReplyContext(message.ReplyTo); quoted != "" {
			if strings.TrimSpace(text) == "" {
				text = "Please read the quoted message, and proceed according to the previous context, or your understanding, in the same langauge."
			}
			text = "Quoted message:\n> " + quoted + "\n\nUser request:\n" + strings.TrimSpace(text)
		}
	}
	mentionUsers := dedupeNonEmptyStrings(mentionCandidates)
	if isGroup && mentionUserSnapshotLimit > 0 && len(mentionUsers) > mentionUserSnapshotLimit {
		mentionUsers = mentionUsers[:mentionUserSnapshotLimit]
	}
	if !contextCompactionOnly && len(downloaded) == 0 && len(imageAttachments) == 0 {
		if result := s.runControl.Steer("telegram", conversationKey, text); result.Found {
			correlationID := fmt.Sprintf("telegram:steer:%d:%d", chatID, message.MessageID)
			if _, publishErr := publishTelegramBusOutbound(context.Background(), s.inprocBus, chatID, messageThreadID, runtimecontrol.SteerFeedback(result.Found, result.Queued), "", correlationID); publishErr != nil {
				s.logger.Warn("telegram_bus_publish_error", "channel", busruntime.ChannelTelegram, "chat_id", chatID, "message_id", message.MessageID, "bus_error_code", string(busruntime.ErrorCodeOf(publishErr)), "error", publishErr.Error())
			}
			return
		}
	}
	accepted, publishErr := s.inboundAdapter.HandleInboundMessage(context.Background(), telegrambus.InboundMessage{
		ChatID:              chatID,
		MessageThreadID:     messageThreadID,
		MessageID:           message.MessageID,
		ReplyToMessageID:    replyToMessageID,
		SentAt:              messageSentAt,
		ChatType:            chatType,
		FromUserID:          fromUserID,
		FromUsername:        fromUsername,
		FromFirstName:       fromFirst,
		FromLastName:        fromLast,
		FromDisplayName:     fromDisplay,
		FromIsAgent:         fromIsAgent,
		Text:                text,
		MentionUsers:        mentionUsers,
		MentionParticipants: mentionParticipants,
		ImageAttachments:    imageAttachments,
	})
	if publishErr != nil {
		s.logger.Warn("telegram_bus_publish_error", "channel", busruntime.ChannelTelegram, "chat_id", chatID, "message_id", message.MessageID, "bus_error_code", string(busruntime.ErrorCodeOf(publishErr)), "error", publishErr.Error())
		callErrorHook(context.Background(), s.logger, s.options.Hooks, ErrorEvent{Stage: ErrorStagePublishInbound, ChatID: chatID, MessageID: message.MessageID, Err: publishErr})
		return
	}
	if !accepted {
		s.logger.Debug("telegram_bus_inbound_deduped", "chat_id", chatID, "message_id", message.MessageID)
	}
}
