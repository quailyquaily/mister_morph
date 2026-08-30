package slack

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
	slackbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/slack"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/personautil"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
	"github.com/quailyquaily/mistermorph/internal/workspace"
)

type slackRuntimeStateConfig struct {
	ctx                 context.Context
	logger              *slog.Logger
	dependencies        Dependencies
	options             RunOptions
	taskStore           daemonruntime.TaskView
	api                 *slackAPI
	botUserID           string
	botID               string
	allowedTeams        map[string]bool
	allowedChannels     map[string]bool
	availableEmojiNames []string
	inprocBus           *busruntime.Inproc
	contactsService     *contacts.Service
	avatarRefresher     *contacts.ContactAvatarRefresher
	pairManager         *agentpair.Manager
	workspaceStore      *workspace.Store
	inboundAdapter      *slackbus.InboundAdapter
	deliveryAdapter     *slackbus.DeliveryAdapter
	runtimeBundle       runtimecore.ChannelRuntimeBundle
	runtimeGenerations  *runtimecore.RuntimeGenerationManager
}

const (
	slackRuntimeClosedTaskError = "slack runtime closed"
	slackWorkerPanicTaskError   = "conversation worker panicked"
	slackApprovalShutdownActor  = "system:slack_shutdown"
	slackApprovalShutdownNote   = "slack runtime closed before approval decision"
)

type slackDaemonServer interface {
	Shutdown(context.Context) error
}

// slackRuntimeState owns the mutable objects whose lifetime spans Slack socket
// mode, task execution, approval handling, and the optional daemon server.
// Slack keeps its own owner because its acknowledgement and delivery rules are
// not interchangeable with the other channel adapters.
type slackRuntimeState struct {
	ctx                           context.Context
	workersCtx                    context.Context
	logger                        *slog.Logger
	dependencies                  Dependencies
	options                       RunOptions
	taskStore                     daemonruntime.TaskView
	guard                         *guard.Guard
	api                           *slackAPI
	botUserID                     string
	botID                         string
	allowedTeams                  map[string]bool
	allowedChannels               map[string]bool
	availableEmojiNames           []string
	availableEmojiList            string
	contactsService               *contacts.Service
	avatarRefresher               *contacts.ContactAvatarRefresher
	pairManager                   *agentpair.Manager
	workspaceStore                *workspace.Store
	inboundAdapter                *slackbus.InboundAdapter
	deliveryAdapter               *slackbus.DeliveryAdapter
	runControl                    *runtimecontrol.RunControl
	untriggeredRecorder           *runtimecore.UntriggeredRecorder
	taskTimeout                   time.Duration
	groupTriggerMode              string
	fileCacheDir                  string
	historyCap                    int
	addressingConfidenceThreshold float64
	addressingInterjectThreshold  float64
	mu                            sync.Mutex
	history                       map[string][]chathistory.ChatHistoryItem
	stickySkillsByConv            map[string][]string
	userIdentityCache             map[string]slackUserIdentityCacheEntry
	agentInteractions             runtimecore.AgentInteractionLimiter
	pendingApprovals              *runtimecore.PendingApprovalRegistry[slackJob]
	runner                        *runtimecore.ConversationRunner[string, slackJob]
	inprocBus                     *busruntime.Inproc
	runtimeBundle                 runtimecore.ChannelRuntimeBundle
	runtimeGenerations            *runtimecore.RuntimeGenerationManager
	stopWorkers                   context.CancelFunc
	server                        slackDaemonServer
	closeOnce                     sync.Once
}

func newSlackRuntimeState(config slackRuntimeStateConfig) (*slackRuntimeState, error) {
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
	groupTriggerMode := strings.ToLower(strings.TrimSpace(config.options.GroupTriggerMode))
	maxConcurrency := config.options.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 1
	}
	pendingApprovals := newSlackPendingApprovalRegistry(sharedGuard, config.taskStore, logger)
	state := &slackRuntimeState{
		ctx:                           ctx,
		workersCtx:                    workersCtx,
		logger:                        logger,
		dependencies:                  config.dependencies,
		options:                       config.options,
		taskStore:                     config.taskStore,
		guard:                         sharedGuard,
		api:                           config.api,
		botUserID:                     strings.TrimSpace(config.botUserID),
		botID:                         strings.TrimSpace(config.botID),
		allowedTeams:                  config.allowedTeams,
		allowedChannels:               config.allowedChannels,
		availableEmojiNames:           append([]string(nil), config.availableEmojiNames...),
		availableEmojiList:            strings.Join(config.availableEmojiNames, ","),
		contactsService:               config.contactsService,
		avatarRefresher:               config.avatarRefresher,
		pairManager:                   config.pairManager,
		workspaceStore:                config.workspaceStore,
		inboundAdapter:                config.inboundAdapter,
		deliveryAdapter:               config.deliveryAdapter,
		runControl:                    runtimecontrol.New(),
		taskTimeout:                   config.options.TaskTimeout,
		groupTriggerMode:              groupTriggerMode,
		fileCacheDir:                  strings.TrimSpace(config.options.FileCacheDir),
		historyCap:                    slackHistoryCapForMode(groupTriggerMode),
		addressingConfidenceThreshold: config.options.AddressingConfidenceThreshold,
		addressingInterjectThreshold:  config.options.AddressingInterjectThreshold,
		history:                       make(map[string][]chathistory.ChatHistoryItem),
		stickySkillsByConv:            make(map[string][]string),
		userIdentityCache:             make(map[string]slackUserIdentityCacheEntry),
		pendingApprovals:              pendingApprovals,
		inprocBus:                     config.inprocBus,
		runtimeBundle:                 runtimeBundle,
		runtimeGenerations:            config.runtimeGenerations,
		stopWorkers:                   stopWorkers,
	}
	fail := func(err error) (*slackRuntimeState, error) {
		state.close()
		return nil, err
	}
	if config.options.RecordUntriggered {
		untriggeredRecorder, err := runtimecore.NewUntriggeredRecorder(config.dependencies.RuntimePaths.JournalDir, config.dependencies.TaskRotateMaxBytes)
		if err != nil {
			return fail(fmt.Errorf("slack untriggered journal: %w", err))
		}
		state.untriggeredRecorder = untriggeredRecorder
	}
	switch {
	case state.api == nil:
		return fail(fmt.Errorf("slack api is required"))
	case state.inprocBus == nil:
		return fail(fmt.Errorf("slack in-process bus is required"))
	case state.contactsService == nil:
		return fail(fmt.Errorf("slack contacts service is required"))
	case state.workspaceStore == nil:
		return fail(fmt.Errorf("slack workspace store is required"))
	case state.inboundAdapter == nil:
		return fail(fmt.Errorf("slack inbound adapter is required"))
	case state.deliveryAdapter == nil:
		return fail(fmt.Errorf("slack delivery adapter is required"))
	case state.runtimeBundle.TaskRuntime == nil:
		return fail(fmt.Errorf("slack task runtime is required"))
	}
	state.runner = runtimecore.NewConversationRunner[string, slackJob](
		workersCtx,
		make(chan struct{}, maxConcurrency),
		16,
		state.runJob,
		runtimecore.ConversationRunnerOptions[string, slackJob]{
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

func (s *slackRuntimeState) close() {
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
				logger.Error("slack_daemon_server_shutdown_error", "error", err.Error())
			}
		}
		if s.stopWorkers != nil {
			s.stopWorkers()
		}
		if s.avatarRefresher != nil {
			s.avatarRefresher.Close()
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
					slackApprovalShutdownActor,
					slackApprovalShutdownNote,
				)
				if resolveErr != nil && !errors.Is(resolveErr, guard.ErrApprovalNotPending) {
					logger.Error("slack_approval_close_resolve_error", "approval_request_id", handle.ID, "task_id", handle.Job.TaskID, "error", resolveErr.Error())
				}
				if _, err := finalizeSlackPendingApproval(s.taskStore, handle.ID, handle.Job, slackRuntimeClosedTaskError); err != nil {
					logger.Error("slack_task_state_write_error", "task_id", handle.Job.TaskID, "status", daemonruntime.TaskFailed, "error", err.Error())
				}
			}
		}
		if s.inprocBus != nil {
			_ = s.inprocBus.Close()
		}
		if s.runtimeGenerations != nil {
			s.runtimeGenerations.Close()
		} else if s.runtimeBundle.Cleanup != nil {
			s.runtimeBundle.Cleanup()
		}
		if s.untriggeredRecorder != nil {
			_ = s.untriggeredRecorder.Close()
		}
	})
}

func (s *slackRuntimeState) finalizeRuntimeClosedJob(_ string, job slackJob) {
	s.finalizeAcceptedTask(job.TaskID, daemonruntime.TaskCanceled, slackRuntimeClosedTaskError)
	job.releaseGeneration()
}

func (s *slackRuntimeState) finalizePanickedJob(conversationKey string, job slackJob) {
	runControlKey := slackRunControlConversationKeyForJob(job)
	if runControlKey == "" {
		runControlKey = conversationKey
	}
	if s != nil && s.runControl != nil {
		s.runControl.Finish("slack", runControlKey, job.TaskID)
	}
	s.finalizeAcceptedTask(job.TaskID, daemonruntime.TaskFailed, slackWorkerPanicTaskError)
	job.releaseGeneration()
}

func (s *slackRuntimeState) finalizeAcceptedTask(taskID string, status daemonruntime.TaskStatus, taskError string) {
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
		logger.Error("slack_task_state_write_error", "task_id", taskID, "status", status, "error", err.Error())
	}
}

func (s *slackRuntimeState) daemonRoutes() (daemonruntime.RoutesOptions, error) {
	if s == nil || s.runner == nil {
		return daemonruntime.RoutesOptions{}, fmt.Errorf("slack runtime runner is not initialized")
	}
	return daemonruntime.RoutesOptions{
		Mode:          "slack",
		AgentNameFunc: func() string { return personautil.LoadAgentName(s.dependencies.RuntimePaths.StateDir) },
		RuntimePaths:  s.dependencies.RuntimePaths,
		AuthToken:     strings.TrimSpace(s.options.Server.AuthToken), TaskTopic: daemonruntime.TaskTopicRoutes{TaskReader: s.taskStore}, Overview: func(context.Context) (map[string]any, error) {
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
					"telegram_configured": false,
					"slack_configured":    true,
					"running":             "slack",
					"telegram_running":    false,
					"slack_running":       true,
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

func (s *slackRuntimeState) serveDaemon() error {
	listen := strings.TrimSpace(s.options.Server.Listen)
	if listen == "" {
		return nil
	}
	routes, err := s.daemonRoutes()
	if err != nil {
		return err
	}
	s.server, err = daemonruntime.StartServer(s.ctx, s.logger, daemonruntime.ServerOptions{Listen: listen, Routes: routes})
	return err
}

func (s *slackRuntimeState) listApprovals(ctx context.Context, req daemonruntime.ApprovalListRequest) (daemonruntime.ApprovalListResponse, error) {
	lease, bundle, err := s.captureRuntimeGeneration()
	if err != nil {
		return daemonruntime.ApprovalListResponse{}, err
	}
	if lease != nil {
		defer lease.Release()
	}
	return runtimecore.ListPendingApprovals(ctx, s.taskStore, bundle.TaskRuntime.SharedGuard, req, "slack")
}

func (s *slackRuntimeState) getApproval(ctx context.Context, approvalID string) (daemonruntime.ApprovalInfo, bool, error) {
	lease, bundle, err := s.captureRuntimeGeneration()
	if err != nil {
		return daemonruntime.ApprovalInfo{}, false, err
	}
	if lease != nil {
		defer lease.Release()
	}
	return runtimecore.GetApprovalInfo(ctx, bundle.TaskRuntime.SharedGuard, approvalID, "slack")
}

func (s *slackRuntimeState) captureRuntimeGeneration() (*runtimecore.RuntimeGenerationLease, *runtimecore.ChannelRuntimeBundle, error) {
	if s == nil {
		return nil, nil, fmt.Errorf("slack runtime is unavailable")
	}
	if s.runtimeGenerations != nil {
		lease, err := s.runtimeGenerations.Capture()
		if err != nil {
			return nil, nil, err
		}
		bundle := lease.Bundle()
		if bundle == nil || bundle.TaskRuntime == nil {
			lease.Release()
			return nil, nil, fmt.Errorf("slack runtime generation is unavailable")
		}
		return lease, bundle, nil
	}
	if s.runtimeBundle.TaskRuntime == nil {
		return nil, nil, fmt.Errorf("slack task runtime is unavailable")
	}
	return nil, &s.runtimeBundle, nil
}

func (s *slackRuntimeState) approve(ctx context.Context, req daemonruntime.ApprovalDecisionRequest) (daemonruntime.ApprovalDecisionResponse, error) {
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

func (s *slackRuntimeState) deny(ctx context.Context, req daemonruntime.ApprovalDecisionRequest) (daemonruntime.ApprovalDecisionResponse, error) {
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

func (s *slackRuntimeState) registerPendingApproval(approvalID string, job slackJob) error {
	if s == nil {
		return fmt.Errorf("slack runtime is unavailable")
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

func (s *slackRuntimeState) notifyPendingApproval(ctx context.Context, approvalID string, job slackJob) error {
	if s == nil {
		return fmt.Errorf("approvals are unavailable")
	}
	approvalGuard := job.approvalGuard(s.guard)
	if approvalGuard == nil {
		return fmt.Errorf("approvals are unavailable")
	}
	if s.api == nil {
		return fmt.Errorf("slack api is unavailable")
	}
	rec, ok, err := approvalGuard.GetApproval(ctx, approvalID)
	if err != nil {
		return err
	}
	if !ok {
		return guard.ErrApprovalNotFound
	}
	text := slackApprovalRequestText(job, rec)
	blocks := buildSlackApprovalBlocks(text, approvalID)
	channelID := strings.TrimSpace(job.ChannelID)
	if len(blocks) == 0 || channelID == "" {
		return fmt.Errorf("slack approval target is unavailable")
	}
	return s.api.postMessageWithBlocks(ctx, channelID, "Approval required.", job.ThreadTS, blocks)
}

func (s *slackRuntimeState) applyApprovalDecision(ctx context.Context, approvalID string, approved bool, actor string) (string, bool, error) {
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		return "", false, daemonruntime.BadRequest("approval_request_id is required")
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "slack:console"
	}
	var claim runtimecore.PendingApprovalClaim[slackJob]
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
		return markSlackMissingApprovalHandle(s.taskStore, approvalID, approved)
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
				info.Error = slackApprovalResultText(false)
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
		if err := runtimecore.ExpirePendingApproval(ctx, approvalGuard, s.taskStore, approvalID, job.TaskID, "slack:expiry"); err != nil {
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
			return job.TaskID, false, slackApprovalDecisionError(resolveErr)
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
		return job.TaskID, false, markSlackApprovalResumeFailed(s.taskStore, job.TaskID, "runner unavailable")
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
	if err := s.runner.Enqueue(s.workersCtx, job.ConversationKey, func(version uint64) slackJob {
		job.Version = version
		return job
	}); err != nil {
		return job.TaskID, false, markSlackApprovalResumeFailed(s.taskStore, job.TaskID, strings.TrimSpace(err.Error()))
	}
	retainGeneration = true
	return job.TaskID, true, nil
}

func (s *slackRuntimeState) handleApprovalAction(ctx context.Context, event slackApprovalActionEvent) bool {
	approvalID := strings.TrimSpace(event.ApprovalRequestID)
	if approvalID == "" {
		return false
	}
	notifyActionResult := func(text string) {
		if s.api == nil || strings.TrimSpace(event.ChannelID) == "" || strings.TrimSpace(text) == "" {
			return
		}
		if err := s.api.postMessage(ctx, event.ChannelID, text, event.ThreadTS); err != nil {
			s.logger.Warn("slack_approval_action_notify_error", "approval_request_id", approvalID, "channel_id", event.ChannelID, "error", err.Error())
		}
	}
	resultText := slackApprovalResultText(event.Approved)
	if _, _, err := s.applyApprovalDecision(ctx, approvalID, event.Approved, slackApprovalActor(event)); err != nil {
		s.logger.Warn("slack_approval_decision_error", "approval_request_id", approvalID, "error", err.Error())
		notifyActionResult(strings.TrimSpace(err.Error()))
		return true
	}
	notifyActionResult(resultText)
	return true
}
