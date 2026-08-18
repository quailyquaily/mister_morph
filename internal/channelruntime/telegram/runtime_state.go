package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/agentpair"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	telegrambus "github.com/quailyquaily/mistermorph/internal/bus/adapters/telegram"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/filecache"
	"github.com/quailyquaily/mistermorph/internal/personautil"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
	"github.com/quailyquaily/mistermorph/internal/workspace"
)

type telegramRuntimeStateConfig struct {
	ctx                context.Context
	logger             *slog.Logger
	dependencies       Dependencies
	options            RunOptions
	taskStore          daemonruntime.TaskView
	api                *telegramAPI
	allowedChatIDs     map[int64]bool
	botUser            string
	botID              int64
	inprocBus          *busruntime.Inproc
	runtimeBundle      runtimecore.ChannelRuntimeBundle
	runtimeGenerations *runtimecore.RuntimeGenerationManager
	contactsService    *contacts.Service
	pairManager        *agentpair.Manager
	workspaceStore     *workspace.Store
	inboundAdapter     *telegrambus.InboundAdapter
}

const (
	telegramRuntimeClosedTaskError = "telegram runtime closed"
	telegramWorkerPanicTaskError   = "conversation worker panicked"
	telegramApprovalShutdownActor  = "system:telegram_shutdown"
	telegramApprovalShutdownNote   = "telegram runtime closed before approval decision"
)

type telegramDaemonServer interface {
	Shutdown(context.Context) error
}

// telegramRuntimeState owns the mutable objects whose lifetime spans Telegram
// polling, task execution, approval handling, and the optional daemon server.
// It is deliberately platform-specific because those lifecycles are coupled to
// Telegram delivery semantics.
type telegramRuntimeState struct {
	ctx                 context.Context
	workersCtx          context.Context
	stopWorkers         context.CancelFunc
	logger              *slog.Logger
	dependencies        Dependencies
	options             RunOptions
	taskStore           daemonruntime.TaskView
	guard               *guard.Guard
	api                 *telegramAPI
	pendingApprovals    *runtimecore.PendingApprovalRegistry[telegramJob]
	runner              *runtimecore.ConversationRunner[string, telegramJob]
	inprocBus           *busruntime.Inproc
	sharedRuntime       runtimecore.ChannelRuntimeBundle
	runtimeGenerations  *runtimecore.RuntimeGenerationManager
	server              telegramDaemonServer
	inboundAdapter      *telegrambus.InboundAdapter
	deliveryAdapter     *telegrambus.DeliveryAdapter
	contactsService     *contacts.Service
	pairManager         *agentpair.Manager
	workspaceStore      *workspace.Store
	runControl          *runtimecontrol.RunControl
	untriggeredRecorder *runtimecore.UntriggeredRecorder
	allowedChatIDs      map[int64]bool
	botUser             string
	botID               int64
	historyCap          int
	groupTriggerMode    string

	stateMu            sync.Mutex
	history            map[string][]chathistory.ChatHistoryItem
	stickySkillsByChat map[string][]string
	lastActivity       map[int64]time.Time
	lastFromUser       map[int64]int64
	lastFromUsername   map[int64]string
	lastFromName       map[int64]string
	lastFromFirst      map[int64]string
	lastFromLast       map[int64]string
	lastChatType       map[int64]string
	knownMentions      map[int64]map[string]string
	agentInteractions  runtimecore.AgentInteractionLimiter
	offset             int64
	planProgressMu     sync.Mutex
	planProgressByID   map[string]telegramPlanProgressEditState
	closeOnce          sync.Once
}

func newTelegramRuntimeState(config telegramRuntimeStateConfig) (*telegramRuntimeState, error) {
	ctx := config.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	logger := config.logger
	if logger == nil {
		logger = slog.Default()
	}
	workersCtx, stopWorkers := context.WithCancel(ctx)
	runtimeBundle := config.runtimeBundle
	if config.runtimeGenerations != nil {
		lease, captureErr := config.runtimeGenerations.Capture()
		if captureErr != nil {
			stopWorkers()
			return nil, captureErr
		}
		if bundle := lease.Bundle(); bundle != nil {
			runtimeBundle = *bundle
		}
		lease.Release()
	}
	var sharedGuard *guard.Guard
	if runtimeBundle.TaskRuntime != nil {
		sharedGuard = runtimeBundle.TaskRuntime.SharedGuard
	}
	allowedChatIDs := make(map[int64]bool, len(config.allowedChatIDs))
	for chatID, allowed := range config.allowedChatIDs {
		if chatID != 0 && allowed {
			allowedChatIDs[chatID] = true
		}
	}
	groupTriggerMode := strings.ToLower(strings.TrimSpace(config.options.GroupTriggerMode))
	state := &telegramRuntimeState{
		ctx:                ctx,
		workersCtx:         workersCtx,
		stopWorkers:        stopWorkers,
		logger:             logger,
		dependencies:       config.dependencies,
		options:            config.options,
		taskStore:          config.taskStore,
		guard:              sharedGuard,
		api:                config.api,
		inprocBus:          config.inprocBus,
		sharedRuntime:      runtimeBundle,
		runtimeGenerations: config.runtimeGenerations,
		inboundAdapter:     config.inboundAdapter,
		contactsService:    config.contactsService,
		pairManager:        config.pairManager,
		workspaceStore:     config.workspaceStore,
		runControl:         runtimecontrol.New(),
		allowedChatIDs:     allowedChatIDs,
		botUser:            strings.TrimSpace(config.botUser),
		botID:              config.botID,
		historyCap:         telegramHistoryCapForMode(groupTriggerMode),
		groupTriggerMode:   groupTriggerMode,
		history:            make(map[string][]chathistory.ChatHistoryItem),
		stickySkillsByChat: make(map[string][]string),
		lastActivity:       make(map[int64]time.Time),
		lastFromUser:       make(map[int64]int64),
		lastFromUsername:   make(map[int64]string),
		lastFromName:       make(map[int64]string),
		lastFromFirst:      make(map[int64]string),
		lastFromLast:       make(map[int64]string),
		lastChatType:       make(map[int64]string),
		knownMentions:      make(map[int64]map[string]string),
		planProgressByID:   make(map[string]telegramPlanProgressEditState),
	}
	fail := func(err error) (*telegramRuntimeState, error) {
		state.close()
		return nil, err
	}
	if config.options.RecordUntriggered {
		untriggeredRecorder, err := runtimecore.NewUntriggeredRecorder(config.dependencies.RuntimePaths.JournalDir, config.dependencies.TaskRotateMaxBytes)
		if err != nil {
			return fail(fmt.Errorf("telegram untriggered journal: %w", err))
		}
		state.untriggeredRecorder = untriggeredRecorder
	}
	switch {
	case state.api == nil:
		return fail(fmt.Errorf("telegram api is required"))
	case state.inprocBus == nil:
		return fail(fmt.Errorf("telegram in-process bus is required"))
	case state.contactsService == nil:
		return fail(fmt.Errorf("telegram contacts service is required"))
	case state.workspaceStore == nil:
		return fail(fmt.Errorf("telegram workspace store is required"))
	case state.inboundAdapter == nil:
		return fail(fmt.Errorf("telegram inbound adapter is required"))
	case state.sharedRuntime.TaskRuntime == nil:
		return fail(fmt.Errorf("telegram task runtime is required"))
	}
	deliveryAdapter, err := telegrambus.NewDeliveryAdapter(telegrambus.DeliveryAdapterOptions{SendText: state.sendText})
	if err != nil {
		return fail(err)
	}
	state.deliveryAdapter = deliveryAdapter
	state.pendingApprovals = newTelegramPendingApprovalRegistry(state.guard, state.taskStore, logger)
	maxConcurrency := config.options.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 1
	}
	state.runner = runtimecore.NewConversationRunner[string, telegramJob](
		workersCtx,
		make(chan struct{}, maxConcurrency),
		16,
		state.runJob,
		runtimecore.ConversationRunnerOptions[string, telegramJob]{
			Logger:  logger,
			OnDrop:  state.finalizeRuntimeClosedJob,
			OnPanic: state.finalizePanickedJob,
		},
	)
	for _, topic := range busruntime.AllTopics() {
		if err := state.inprocBus.Subscribe(topic, state.handleBusMessage); err != nil {
			return fail(err)
		}
	}
	return state, nil
}

func (s *telegramRuntimeState) close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		logger := s.logger
		if logger == nil {
			logger = slog.Default()
		}
		if s.server != nil {
			if err := s.server.Shutdown(context.Background()); err != nil {
				logger.Error("telegram_daemon_server_shutdown_error", "error", err.Error())
			}
		}
		if s.stopWorkers != nil {
			s.stopWorkers()
		}
		if s.runner != nil {
			s.runner.WaitClosed()
		}
		if s.pendingApprovals != nil {
			for _, handle := range s.pendingApprovals.Close() {
				approvalGuard := handle.Job.approvalGuard(s.guard)
				_, _, resolveErr := runtimecore.ResolveApprovalCommit(
					context.Background(),
					approvalGuard,
					handle.ID,
					guard.ApprovalExpired,
					telegramApprovalShutdownActor,
					telegramApprovalShutdownNote,
				)
				if resolveErr != nil && !errors.Is(resolveErr, guard.ErrApprovalNotPending) {
					logger.Error("telegram_approval_close_resolve_error", "approval_request_id", handle.ID, "task_id", handle.Job.TaskID, "error", resolveErr.Error())
				}
				if _, err := finalizeTelegramPendingApproval(s.taskStore, handle.ID, handle.Job, telegramRuntimeClosedTaskError); err != nil {
					logger.Error("telegram_task_state_write_error", "task_id", handle.Job.TaskID, "status", daemonruntime.TaskFailed, "error", err.Error())
				}
			}
		}
		if s.inprocBus != nil {
			_ = s.inprocBus.Close()
		}
		if s.runtimeGenerations != nil {
			s.runtimeGenerations.Close()
		} else if s.sharedRuntime.Cleanup != nil {
			s.sharedRuntime.Cleanup()
		}
		if s.untriggeredRecorder != nil {
			_ = s.untriggeredRecorder.Close()
		}
	})
}

func (s *telegramRuntimeState) finalizeRuntimeClosedJob(_ string, job telegramJob) {
	s.finalizeAcceptedTask(job.TaskID, daemonruntime.TaskCanceled, telegramRuntimeClosedTaskError)
	job.releaseGeneration()
}

func (s *telegramRuntimeState) finalizePanickedJob(conversationKey string, job telegramJob) {
	if s != nil && s.runControl != nil {
		s.runControl.Finish("telegram", conversationKey, job.TaskID)
	}
	s.finalizeAcceptedTask(job.TaskID, daemonruntime.TaskFailed, telegramWorkerPanicTaskError)
	job.releaseGeneration()
}

func (s *telegramRuntimeState) finalizeAcceptedTask(taskID string, status daemonruntime.TaskStatus, taskError string) {
	if s == nil || s.taskStore == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	finishedAt := time.Now().UTC()
	err := s.taskStore.Update(taskID, func(info *daemonruntime.TaskInfo) {
		if info == nil || (info.Status != daemonruntime.TaskQueued && info.Status != daemonruntime.TaskRunning) {
			return
		}
		info.Status = status
		info.Error = strings.TrimSpace(taskError)
		info.FinishedAt = &finishedAt
		runtimecore.ClearTaskPendingApprovalFields(info)
	})
	if err != nil {
		logger := s.logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Error("telegram_task_state_write_error", "task_id", taskID, "status", status, "error", err.Error())
	}
}

func (s *telegramRuntimeState) daemonRoutes() (daemonruntime.RoutesOptions, error) {
	if s == nil || s.runner == nil {
		return daemonruntime.RoutesOptions{}, fmt.Errorf("telegram runtime runner is not initialized")
	}
	return daemonruntime.RoutesOptions{
		Mode:            "telegram",
		AgentNameFunc:   func() string { return personautil.LoadAgentName(s.dependencies.RuntimePaths.StateDir) },
		RuntimePaths:    s.dependencies.RuntimePaths,
		FileCacheLimits: filecache.Limits{MaxAge: s.options.FileCacheMaxAge, MaxFiles: s.options.FileCacheMaxFiles, MaxTotalBytes: s.options.FileCacheMaxTotalBytes},
		AuthToken:       strings.TrimSpace(s.options.Server.AuthToken), TaskTopic: daemonruntime.TaskTopicRoutes{TaskReader: s.taskStore}, Overview: func(context.Context) (map[string]any, error) {
			lease, bundle, err := s.captureRuntimeGeneration()
			if err != nil {
				return nil, err
			}
			if lease != nil {
				defer lease.Release()
			}
			mainRoute := bundle.TaskRuntime.BootstrapMainRoute
			return map[string]any{
				"llm": map[string]any{
					"provider": strings.TrimSpace(mainRoute.ClientConfig.Provider),
					"model":    bundle.TaskRuntime.BootstrapMainModel,
				},
				"channel": map[string]any{
					"configured":          true,
					"telegram_configured": true,
					"slack_configured":    false,
					"running":             "telegram",
					"telegram_running":    true,
					"slack_running":       false,
				},
				"poke_enabled":     s.options.Server.Poke != nil,
				"cron_run_enabled": s.options.Server.CronRun != nil,
			}, nil
		},
		Poke:    s.options.Server.Poke,
		CronRun: s.options.Server.CronRun, Approvals: daemonruntime.ApprovalRoutes{List: s.listApprovals, Get: s.getApproval, Approve: s.approve, Deny: s.deny}, AgentSettingsEnabled: true,
		AgentSettingsOwner:  s.dependencies.AgentSettingsOwner,
		AgentSettingsReader: s.dependencies.AgentSettingsReader,
		HealthEnabled:       true,
	}, nil
}

func (s *telegramRuntimeState) serveDaemon() error {
	listen := strings.TrimSpace(s.options.Server.Listen)
	if listen == "" {
		return nil
	}
	routes, err := s.daemonRoutes()
	if err != nil {
		return err
	}
	s.server, err = daemonruntime.StartServer(s.ctx, s.logger, daemonruntime.ServerOptions{
		Listen: listen,
		Routes: routes,
	})
	return err
}

func (s *telegramRuntimeState) listApprovals(ctx context.Context, req daemonruntime.ApprovalListRequest) (daemonruntime.ApprovalListResponse, error) {
	lease, bundle, err := s.captureRuntimeGeneration()
	if err != nil {
		return daemonruntime.ApprovalListResponse{}, err
	}
	if lease != nil {
		defer lease.Release()
	}
	return runtimecore.ListPendingApprovals(ctx, s.taskStore, bundle.TaskRuntime.SharedGuard, req, "telegram")
}

func (s *telegramRuntimeState) getApproval(ctx context.Context, approvalID string) (daemonruntime.ApprovalInfo, bool, error) {
	lease, bundle, err := s.captureRuntimeGeneration()
	if err != nil {
		return daemonruntime.ApprovalInfo{}, false, err
	}
	if lease != nil {
		defer lease.Release()
	}
	return runtimecore.GetApprovalInfo(ctx, bundle.TaskRuntime.SharedGuard, approvalID, "telegram")
}

func (s *telegramRuntimeState) captureRuntimeGeneration() (*runtimecore.RuntimeGenerationLease, *runtimecore.ChannelRuntimeBundle, error) {
	if s == nil {
		return nil, nil, fmt.Errorf("telegram runtime is unavailable")
	}
	if s.runtimeGenerations != nil {
		lease, err := s.runtimeGenerations.Capture()
		if err != nil {
			return nil, nil, err
		}
		bundle := lease.Bundle()
		if bundle == nil || bundle.TaskRuntime == nil {
			lease.Release()
			return nil, nil, fmt.Errorf("telegram runtime generation is unavailable")
		}
		return lease, bundle, nil
	}
	if s.sharedRuntime.TaskRuntime == nil {
		return nil, nil, fmt.Errorf("telegram task runtime is unavailable")
	}
	return nil, &s.sharedRuntime, nil
}

func (s *telegramRuntimeState) approve(ctx context.Context, req daemonruntime.ApprovalDecisionRequest) (daemonruntime.ApprovalDecisionResponse, error) {
	taskID, resumed, err := s.applyApprovalDecision(ctx, req.ApprovalRequestID, true, strings.TrimSpace(req.Actor))
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

func (s *telegramRuntimeState) deny(ctx context.Context, req daemonruntime.ApprovalDecisionRequest) (daemonruntime.ApprovalDecisionResponse, error) {
	taskID, resumed, err := s.applyApprovalDecision(ctx, req.ApprovalRequestID, false, strings.TrimSpace(req.Actor))
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

func (s *telegramRuntimeState) registerPendingApproval(approvalID string, job telegramJob) error {
	if s == nil {
		return fmt.Errorf("telegram runtime is unavailable")
	}
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		return fmt.Errorf("approval id is required")
	}
	approvalGuard := job.approvalGuard(s.guard)
	if approvalGuard == nil || s.pendingApprovals == nil {
		return fmt.Errorf("approvals are unavailable")
	}
	rec, ok, err := approvalGuard.GetApproval(context.Background(), approvalID)
	if err != nil || !ok {
		if err == nil {
			err = guard.ErrApprovalNotFound
		}
		return err
	}
	displaced, replaced, err := s.pendingApprovals.Register(approvalID, job, rec.ExpiresAt)
	if replaced {
		displaced.releaseGeneration()
	}
	return err
}

func (s *telegramRuntimeState) notifyPendingApproval(ctx context.Context, approvalID string, job telegramJob) error {
	if s == nil {
		return fmt.Errorf("approvals are unavailable")
	}
	approvalGuard := job.approvalGuard(s.guard)
	if approvalGuard == nil {
		return fmt.Errorf("approvals are unavailable")
	}
	if s.api == nil {
		return fmt.Errorf("telegram api is unavailable")
	}
	rec, ok, err := approvalGuard.GetApproval(ctx, approvalID)
	if err != nil {
		return err
	}
	if !ok {
		return guard.ErrApprovalNotFound
	}
	text := telegramApprovalRequestText(job, rec)
	replyMarkup := telegramApprovalReplyMarkup(approvalID)
	if replyMarkup == nil || job.ChatID == 0 {
		return fmt.Errorf("telegram approval target is unavailable")
	}
	_, err = s.api.sendMessageHTMLReplyInThreadWithMessageIDAndMarkup(ctx, job.ChatID, job.MessageThreadID, text, true, 0, replyMarkup)
	return err
}

func (s *telegramRuntimeState) applyApprovalDecision(ctx context.Context, approvalID string, approved bool, actor string) (string, bool, error) {
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		return "", false, daemonruntime.BadRequest("approval_request_id is required")
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "telegram:console"
	}
	var claim runtimecore.PendingApprovalClaim[telegramJob]
	claimState := runtimecore.PendingApprovalClaimMissing
	var claimErr error
	if s.pendingApprovals != nil {
		claim, claimState, claimErr = s.pendingApprovals.Claim(approvalID)
	}
	if claimErr != nil {
		return "", false, claimErr
	}
	switch claimState {
	case runtimecore.PendingApprovalClaimInFlight:
		return claim.Job.TaskID, false, runtimecore.ErrPendingApprovalClaimInFlight
	case runtimecore.PendingApprovalClaimMissing:
		return markTelegramMissingApprovalHandle(s.taskStore, approvalID, approved)
	}
	job := claim.Job
	approvalGuard := job.approvalGuard(s.guard)
	if approvalGuard == nil {
		job.releaseGeneration()
		return job.TaskID, false, fmt.Errorf("approvals are unavailable")
	}
	retainGeneration := false
	defer func() {
		if !retainGeneration {
			job.releaseGeneration()
		}
	}()
	defer s.pendingApprovals.CompleteClaim(claim)
	failClaimed := func(cause error) (string, bool, error) {
		applied, updateErr := runtimecore.FailPendingApprovalTask(s.taskStore, job.TaskID, approvalID, "approval resume failed: "+strings.TrimSpace(cause.Error()))
		if updateErr != nil {
			return job.TaskID, false, errors.Join(cause, updateErr)
		}
		if !applied {
			return job.TaskID, false, errors.Join(cause, runtimecore.ErrApprovalTaskStateUnchanged)
		}
		return job.TaskID, false, cause
	}
	cancelClaimed := func(cause error) (string, bool, error) {
		var updateErr error
		finishedAt := time.Now().UTC()
		if s.taskStore != nil && strings.TrimSpace(job.TaskID) != "" {
			updateErr = s.taskStore.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
				info.Status = daemonruntime.TaskCanceled
				info.Error = telegramApprovalResultText(false)
				info.FinishedAt = &finishedAt
				runtimecore.ClearTaskPendingApprovalFields(info)
				info.ApprovalRequestID = approvalID
			})
		}
		if updateErr != nil {
			return job.TaskID, false, errors.Join(cause, updateErr)
		}
		return job.TaskID, false, cause
	}
	rec, found, err := approvalGuard.GetApproval(ctx, approvalID)
	if err != nil {
		return failClaimed(err)
	}
	if !found {
		return failClaimed(daemonruntime.BadRequest("approval not found"))
	}
	if rec.Status != guard.ApprovalPending {
		if !approved && rec.Status == guard.ApprovalDenied {
			return cancelClaimed(daemonruntime.BadRequest("approval is not pending"))
		}
		return failClaimed(daemonruntime.BadRequest("approval is not pending"))
	}
	if !rec.ExpiresAt.IsZero() && time.Now().UTC().After(rec.ExpiresAt) {
		if err := runtimecore.ExpirePendingApproval(ctx, approvalGuard, s.taskStore, approvalID, job.TaskID, "telegram:expiry"); err != nil {
			if !errors.Is(err, guard.ErrApprovalNotPending) {
				task, taskExists := s.taskStore.Get(job.TaskID)
				if taskExists && task.Status == daemonruntime.TaskPending && strings.TrimSpace(task.ApprovalRequestID) == approvalID {
					return failClaimed(err)
				}
				return job.TaskID, false, err
			}
		}
		return job.TaskID, false, daemonruntime.BadRequest("approval is expired")
	}
	status := guard.ApprovalDenied
	if approved {
		status = guard.ApprovalApproved
	}
	commitState, pendingRec, resolveErr := runtimecore.ResolveApprovalCommit(ctx, approvalGuard, approvalID, status, actor, "")
	if resolveErr != nil {
		switch commitState {
		case runtimecore.ApprovalCommitPending:
			if registerErr := s.pendingApprovals.RestoreClaim(claim, pendingRec.ExpiresAt); registerErr != nil {
				return failClaimed(errors.Join(resolveErr, registerErr))
			}
			retainGeneration = true
			return job.TaskID, false, telegramApprovalDecisionError(resolveErr)
		case runtimecore.ApprovalCommitCommitted:
			if !approved {
				return cancelClaimed(resolveErr)
			}
			return failClaimed(resolveErr)
		default:
			return failClaimed(resolveErr)
		}
	}
	if !approved {
		return cancelClaimed(nil)
	}
	if s.runner == nil {
		return job.TaskID, false, markTelegramApprovalResumeFailed(s.taskStore, job.TaskID, "runner unavailable")
	}
	job.ResumeApprovalID = approvalID
	resumedAt := time.Now().UTC()
	if s.taskStore != nil && strings.TrimSpace(job.TaskID) != "" {
		if err := s.taskStore.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
			info.Status = daemonruntime.TaskQueued
			info.Error = ""
			info.ResumedAt = &resumedAt
			runtimecore.ClearTaskPendingApprovalFields(info)
		}); err != nil {
			return failClaimed(err)
		}
	}
	if err := s.runner.Enqueue(s.workersCtx, job.ConversationKey, func(version uint64) telegramJob {
		job.Version = version
		return job
	}); err != nil {
		return job.TaskID, false, markTelegramApprovalResumeFailed(s.taskStore, job.TaskID, strings.TrimSpace(err.Error()))
	}
	retainGeneration = true
	return job.TaskID, true, nil
}

func (s *telegramRuntimeState) handleApprovalCallback(ctx context.Context, query *telegramCallbackQuery) bool {
	if query == nil {
		return false
	}
	approvalID, approved, ok := parseTelegramApprovalCallbackData(query.Data)
	if !ok {
		return false
	}
	answer := func(text string, alert bool) {
		if s.api == nil || strings.TrimSpace(query.ID) == "" {
			return
		}
		if err := s.api.answerCallbackQuery(context.Background(), query.ID, text, alert); err != nil {
			s.logger.Warn("telegram_approval_callback_answer_error", "approval_request_id", approvalID, "error", err.Error())
		}
	}
	sendResultMessage := func(text string) {
		if s.api == nil || strings.TrimSpace(text) == "" {
			return
		}
		chatID, messageThreadID, ok := telegramApprovalCallbackMessageTarget(query)
		if !ok {
			return
		}
		if _, err := s.api.sendMessageHTMLReplyInThreadWithMessageID(ctx, chatID, messageThreadID, text, true, 0); err != nil {
			s.logger.Warn("telegram_approval_result_message_error", "approval_request_id", approvalID, "chat_id", chatID, "error", err.Error())
		}
	}
	answerText := telegramApprovalResultText(approved)
	if _, _, err := s.applyApprovalDecision(ctx, approvalID, approved, telegramApprovalActor(query.From)); err != nil {
		answer(strings.TrimSpace(err.Error()), true)
		s.logger.Warn("telegram_approval_decision_error", "approval_request_id", approvalID, "error", err.Error())
		return true
	}
	answer(answerText, false)
	sendResultMessage(answerText)
	return true
}
