package slack

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	slackbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/slack"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/spf13/viper"
)

type slackLoopOwner interface {
	runJob(context.Context, string, slackJob)
	enqueueInbound(context.Context, busruntime.BusMessage) error
	handleBusMessage(context.Context, busruntime.BusMessage) error
	deliverOutbound(context.Context, busruntime.BusMessage) error
	handleSocketEnvelope(context.Context, slackSocketEnvelope) error
}

var _ slackLoopOwner = (*slackRuntimeState)(nil)

type slackRuntimeSingleConnListener struct {
	mu        sync.Mutex
	conn      net.Conn
	delivered bool
	closed    chan struct{}
	closeOnce sync.Once
}

type slackRuntimeShutdownErrorServer struct {
	err   error
	calls atomic.Int32
}

func (s *slackRuntimeShutdownErrorServer) Shutdown(context.Context) error {
	s.calls.Add(1)
	return s.err
}

func (l *slackRuntimeSingleConnListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if !l.delivered {
		l.delivered = true
		conn := l.conn
		l.mu.Unlock()
		return conn, nil
	}
	l.mu.Unlock()
	<-l.closed
	return nil, net.ErrClosed
}

func (l *slackRuntimeSingleConnListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func (*slackRuntimeSingleConnListener) Addr() net.Addr { return &net.TCPAddr{} }

func TestSlackQueuedJobKeepsAdmissionRouteWithoutTaskStore(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	currentRoute := llmutil.ResolvedRoute{
		Purpose:  llmutil.RoutePurposeMainLoop,
		Identity: "slack-admission-route",
		Profile:  "admitted",
		Values:   llmutil.RuntimeValues{CacheTTL: "1h"},
		ClientConfig: llmconfig.ClientConfig{
			Provider: "test",
			Model:    "admitted-model",
		},
	}
	runtime, err := taskruntime.NewRunPreparer(depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) { return logger, nil },
		ResolveLLMRoute: func(purpose string) (llmutil.ResolvedRoute, error) {
			if purpose != llmutil.RoutePurposeMainLoop {
				t.Fatalf("route purpose = %q, want main_loop", purpose)
			}
			return currentRoute, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) { return nil, nil },
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.PromptSpec{}, nil, nil
		},
	}, taskruntime.BootstrapOptions{})
	if err != nil {
		t.Fatalf("NewRunPreparer() error = %v", err)
	}
	defer runtime.Close()

	workersCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	jobs := make(chan slackJob, 1)
	state := &slackRuntimeState{
		workersCtx:    workersCtx,
		logger:        logger,
		runtimeBundle: runtimecore.ChannelRuntimeBundle{TaskRuntime: runtime},
		runControl:    runtimecontrol.New(),
	}
	state.runner = runtimecore.NewConversationRunner[string, slackJob](workersCtx, make(chan struct{}, 1), 1, func(_ context.Context, _ string, job slackJob) {
		jobs <- job
	}, runtimecore.ConversationRunnerOptions[string, slackJob]{Logger: logger})

	payload, err := busruntime.EncodeMessageEnvelope(busruntime.TopicChatMessage, busruntime.MessageEnvelope{
		MessageID: "slack:T111:C222:1739667600.000100",
		Text:      "inspect this",
		SentAt:    "2026-07-21T00:00:00Z",
		SessionID: "0194e9d5-2f8f-7000-8000-000000000001",
	})
	if err != nil {
		t.Fatalf("EncodeMessageEnvelope() error = %v", err)
	}
	conversationKey, err := busruntime.BuildSlackChannelConversationKey("T111:C222")
	if err != nil {
		t.Fatalf("BuildSlackChannelConversationKey() error = %v", err)
	}
	message := busruntime.BusMessage{
		Direction:       busruntime.DirectionInbound,
		Channel:         busruntime.ChannelSlack,
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: conversationKey,
		PayloadBase64:   payload,
		Extensions: busruntime.MessageExtensions{
			PlatformMessageID: "T111:C222:1739667600.000100",
			ChatType:          "channel",
			TeamID:            "T111",
			ChannelID:         "C222",
		},
	}
	if err := state.enqueueInbound(context.Background(), message); err != nil {
		t.Fatalf("enqueueInbound() error = %v", err)
	}
	currentRoute = llmutil.ResolvedRoute{ClientConfig: llmconfig.ClientConfig{Provider: "changed", Model: "changed-model"}}

	select {
	case job := <-jobs:
		if job.Route == nil {
			t.Fatal("queued route = nil")
		}
		if job.Route.Identity != "slack-admission-route" || job.Route.ClientConfig.Model != "admitted-model" {
			t.Fatalf("queued route = %#v, want admitted route", job.Route)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for queued job")
	}
}

func TestSlackRuntimeStateDoesNotPublishDaemonRoutesBeforeRunnerIsReady(t *testing.T) {
	state := &slackRuntimeState{}
	if _, err := state.daemonRoutes(); err == nil || !strings.Contains(err.Error(), "runner") {
		t.Fatalf("daemonRoutes() error = %v, want uninitialized runner error", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state.runner = runtimecore.NewConversationRunner[string, slackJob](
		ctx,
		make(chan struct{}, 1),
		1,
		func(context.Context, string, slackJob) {},
		runtimecore.ConversationRunnerOptions[string, slackJob]{},
	)
	routes, err := state.daemonRoutes()
	if err != nil {
		t.Fatalf("daemonRoutes() error = %v", err)
	}
	if routes.Approvals.List == nil || routes.Approvals.Approve == nil || routes.Approvals.Deny == nil {
		t.Fatal("daemon approval routes are incomplete")
	}
}

func TestSlackDaemonSettingsUseRuntimeSettingsSnapshot(t *testing.T) {
	settings := viper.New()
	settings.Set("llm.model", "slack-model")
	global := viper.GetViper()
	global.Set("llm.model", "global-model")
	t.Cleanup(func() { global.Set("llm.model", nil) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &slackRuntimeState{
		dependencies: Dependencies{CommonDependencies: depsutil.CommonDependencies{
			AgentSettingsReader: agentsettings.NewReaderSnapshot(settings),
		}},
		options: RunOptions{Server: ServerOptions{AuthToken: "token"}},
		runner: runtimecore.NewConversationRunner[string, slackJob](
			ctx,
			make(chan struct{}, 1),
			1,
			func(context.Context, string, slackJob) {},
			runtimecore.ConversationRunnerOptions[string, slackJob]{},
		),
	}
	routes, err := state.daemonRoutes()
	if err != nil {
		t.Fatalf("daemonRoutes() error = %v", err)
	}
	handler := daemonruntime.NewHandler(routes)
	req := httptest.NewRequest(http.MethodGet, "/settings/agent", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		LLM struct {
			Model string `json:"model"`
		} `json:"llm"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.LLM.Model != "slack-model" {
		t.Fatalf("model = %q, want slack-model", payload.LLM.Model)
	}
}

func TestSlackRuntimeStateCloseOwnsLongLivedResourcesOnce(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	taskStore := daemonruntime.NewMemoryStore(4)
	pendingAt := time.Now().UTC()
	taskStore.Upsert(daemonruntime.TaskInfo{
		ID:                "task-1",
		Status:            daemonruntime.TaskPending,
		PendingAt:         &pendingAt,
		ApprovalRequestID: "approval-1",
		Result:            map[string]any{"pending": true},
	})
	inprocBus, err := busruntime.StartInproc(busruntime.BootstrapOptions{
		MaxInFlight: 1,
		Logger:      logger,
		Component:   "slack-state-test",
	})
	if err != nil {
		t.Fatalf("StartInproc() error = %v", err)
	}
	workersCtx, stopWorkers := context.WithCancel(context.Background())
	pending := runtimecore.NewPendingApprovalRegistry[slackJob](nil)
	pending.Register("approval-1", slackJob{TaskID: "task-1"}, time.Now().Add(time.Hour))

	var runtimeCleanupCalls atomic.Int32
	var cleanupBusErr error
	serverShutdown := make(chan struct{})
	server := &http.Server{}
	server.RegisterOnShutdown(func() { close(serverShutdown) })
	state := &slackRuntimeState{
		workersCtx:  workersCtx,
		stopWorkers: stopWorkers,
		logger:      logger,
		taskStore:   taskStore,
		inprocBus:   inprocBus,
		runtimeBundle: runtimecore.ChannelRuntimeBundle{Cleanup: func() {
			runtimeCleanupCalls.Add(1)
			cleanupBusErr = inprocBus.Publish(context.Background(), busruntime.BusMessage{Topic: busruntime.TopicChatMessage})
		}},
		pendingApprovals: pending,
		server:           server,
	}

	state.close()
	state.close()

	if workersCtx.Err() != context.Canceled {
		t.Fatalf("workers context error = %v, want context canceled", workersCtx.Err())
	}
	if got := runtimeCleanupCalls.Load(); got != 1 {
		t.Fatalf("runtime cleanup calls = %d, want 1", got)
	}
	if cleanupBusErr == nil || !strings.Contains(cleanupBusErr.Error(), "bus is closed") {
		t.Fatalf("bus state during shared runtime cleanup = %v, want closed bus", cleanupBusErr)
	}
	if _, ok := pending.Get("approval-1"); ok {
		t.Fatal("pending approval survived runtime close")
	}
	task, ok := taskStore.Get("task-1")
	if !ok || task == nil {
		t.Fatal("pending approval task is missing after close")
	}
	if task.Status != daemonruntime.TaskFailed || task.Error != "slack runtime closed" || task.FinishedAt == nil {
		t.Fatalf("pending approval task after close = %#v, want failed terminal state", task)
	}
	if task.PendingAt != nil || task.ApprovalRequestID != "" || task.Result != nil {
		t.Fatalf("pending approval fields after close = %v/%q/%#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
	}
	select {
	case <-serverShutdown:
	case <-time.After(time.Second):
		t.Fatal("daemon server was not shut down")
	}
}

func TestSlackRuntimeStateCloseWaitsForRunnerBeforeCleanup(t *testing.T) {
	workersCtx, stopWorkers := context.WithCancel(context.Background())
	handlerStarted := make(chan struct{})
	handlerCanceled := make(chan struct{})
	allowHandlerExit := make(chan struct{})
	cleanupCalled := make(chan struct{}, 1)
	closeDone := make(chan struct{})
	state := &slackRuntimeState{
		workersCtx:  workersCtx,
		stopWorkers: stopWorkers,
		runtimeBundle: runtimecore.ChannelRuntimeBundle{Cleanup: func() {
			cleanupCalled <- struct{}{}
		}},
	}
	state.runner = runtimecore.NewConversationRunner[string, slackJob](
		workersCtx,
		make(chan struct{}, 1),
		1,
		func(ctx context.Context, _ string, _ slackJob) {
			close(handlerStarted)
			<-ctx.Done()
			close(handlerCanceled)
			<-allowHandlerExit
		},
		runtimecore.ConversationRunnerOptions[string, slackJob]{},
	)
	if err := state.runner.Enqueue(context.Background(), "slack:close", func(uint64) slackJob {
		return slackJob{TaskID: "task-active"}
	}); err != nil {
		t.Fatalf("runner.Enqueue() error = %v", err)
	}
	select {
	case <-handlerStarted:
	case <-time.After(time.Second):
		t.Fatal("runner handler did not start")
	}

	go func() {
		state.close()
		close(closeDone)
	}()
	select {
	case <-handlerCanceled:
	case <-time.After(time.Second):
		t.Fatal("runner handler did not observe cancellation")
	}
	select {
	case <-cleanupCalled:
		close(allowHandlerExit)
		<-closeDone
		t.Fatal("runtime cleanup ran before the active handler exited")
	default:
	}
	close(allowHandlerExit)
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("runtime close did not wait for the runner")
	}
	select {
	case <-cleanupCalled:
	default:
		t.Fatal("runtime cleanup was not called")
	}
}

func TestSlackRuntimeStateCloseWaitsForDaemonHandlerBeforeCleanup(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	listener := &slackRuntimeSingleConnListener{conn: serverConn, closed: make(chan struct{})}
	handlerStarted := make(chan struct{})
	allowHandlerExit := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(handlerStarted)
		<-allowHandlerExit
		w.WriteHeader(http.StatusNoContent)
	})}
	go func() { _ = server.Serve(listener) }()
	go func() {
		_, _ = io.WriteString(clientConn, "GET / HTTP/1.1\r\nHost: test\r\nConnection: close\r\n\r\n")
		_, _ = io.Copy(io.Discard, clientConn)
	}()
	select {
	case <-handlerStarted:
	case <-time.After(time.Second):
		t.Fatal("daemon handler did not start")
	}
	cleanupCalled := make(chan struct{}, 1)
	closeDone := make(chan struct{})
	state := &slackRuntimeState{
		server: server,
		runtimeBundle: runtimecore.ChannelRuntimeBundle{Cleanup: func() {
			cleanupCalled <- struct{}{}
		}},
	}
	go func() {
		state.close()
		close(closeDone)
	}()

	cleanupRanEarly := false
	select {
	case <-cleanupCalled:
		cleanupRanEarly = true
	case <-time.After(2200 * time.Millisecond):
	}
	close(allowHandlerExit)
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("runtime close did not finish after daemon handler exit")
	}
	if cleanupRanEarly {
		t.Fatal("runtime cleanup ran before the daemon handler exited")
	}
}

func TestSlackRuntimeStateShutdownErrorStillCleansOwnedResources(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	inprocBus, err := busruntime.StartInproc(busruntime.BootstrapOptions{
		MaxInFlight: 1,
		Logger:      logger,
		Component:   "slack-shutdown-error-test",
	})
	if err != nil {
		t.Fatalf("StartInproc() error = %v", err)
	}
	workersCtx, stopWorkers := context.WithCancel(context.Background())
	server := &slackRuntimeShutdownErrorServer{err: errors.New("listener close failed")}
	var cleanupCalls atomic.Int32
	state := &slackRuntimeState{
		logger:      logger,
		workersCtx:  workersCtx,
		stopWorkers: stopWorkers,
		server:      server,
		inprocBus:   inprocBus,
		runtimeBundle: runtimecore.ChannelRuntimeBundle{Cleanup: func() {
			cleanupCalls.Add(1)
		}},
	}

	state.close()

	if got := server.calls.Load(); got != 1 {
		t.Fatalf("Shutdown() calls = %d, want 1", got)
	}
	if workersCtx.Err() != context.Canceled {
		t.Fatalf("workers context error = %v, want canceled", workersCtx.Err())
	}
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("runtime cleanup calls = %d, want 1", got)
	}
	if err := inprocBus.Publish(context.Background(), busruntime.BusMessage{Topic: busruntime.TopicChatMessage}); err == nil {
		t.Fatal("in-process bus remained open after shutdown error")
	}
}

func TestSlackRunJobCancellationMarksActiveTaskCanceled(t *testing.T) {
	store := daemonruntime.NewMemoryStore(4)
	store.Upsert(daemonruntime.TaskInfo{ID: "task-active", Status: daemonruntime.TaskQueued})
	runnerCtx, cancelRunner := context.WithCancel(context.Background())
	defer cancelRunner()
	state := &slackRuntimeState{
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		taskStore:          store,
		runControl:         runtimecontrol.New(),
		history:            make(map[string][]chathistory.ChatHistoryItem),
		stickySkillsByConv: make(map[string][]string),
	}
	state.runner = runtimecore.NewConversationRunner[string, slackJob](
		runnerCtx,
		make(chan struct{}, 1),
		1,
		func(context.Context, string, slackJob) {},
		runtimecore.ConversationRunnerOptions[string, slackJob]{},
	)
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	cancelWorker()
	state.runJob(workerCtx, "slack:active", slackJob{TaskID: "task-active", ConversationKey: "slack:active", Text: "work"})

	task, _ := store.Get("task-active")
	if task.Status != daemonruntime.TaskCanceled || task.Error != "slack runtime closed" || task.FinishedAt == nil {
		t.Fatalf("active task after worker cancellation = %#v, want canceled", task)
	}
}

func TestSlackDroppedJobOnlyTerminatesQueuedOrRunningTask(t *testing.T) {
	store := daemonruntime.NewMemoryStore(8)
	for _, status := range []daemonruntime.TaskStatus{
		daemonruntime.TaskQueued,
		daemonruntime.TaskRunning,
		daemonruntime.TaskPending,
		daemonruntime.TaskDone,
	} {
		store.Upsert(daemonruntime.TaskInfo{ID: string(status), Status: status})
	}
	state := &slackRuntimeState{taskStore: store, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, status := range []daemonruntime.TaskStatus{
		daemonruntime.TaskQueued,
		daemonruntime.TaskRunning,
		daemonruntime.TaskPending,
		daemonruntime.TaskDone,
	} {
		state.finalizeRuntimeClosedJob("slack:test", slackJob{TaskID: string(status)})
	}
	for _, status := range []daemonruntime.TaskStatus{daemonruntime.TaskQueued, daemonruntime.TaskRunning} {
		task, _ := store.Get(string(status))
		if task.Status != daemonruntime.TaskCanceled || task.Error != "slack runtime closed" {
			t.Fatalf("%s task after drop = %#v, want canceled", status, task)
		}
	}
	for _, status := range []daemonruntime.TaskStatus{daemonruntime.TaskPending, daemonruntime.TaskDone} {
		task, _ := store.Get(string(status))
		if task.Status != status {
			t.Fatalf("%s task after drop = %#v, want unchanged", status, task)
		}
	}
}

func TestSlackPanickedJobDoesNotOverwriteTerminalTask(t *testing.T) {
	store := daemonruntime.NewMemoryStore(8)
	for _, status := range []daemonruntime.TaskStatus{
		daemonruntime.TaskQueued,
		daemonruntime.TaskRunning,
		daemonruntime.TaskDone,
		daemonruntime.TaskCanceled,
	} {
		if err := store.Upsert(daemonruntime.TaskInfo{ID: string(status), Status: status}); err != nil {
			t.Fatalf("Upsert(%s) error = %v", status, err)
		}
	}
	state := &slackRuntimeState{
		taskStore:  store,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		runControl: runtimecontrol.New(),
	}
	for _, status := range []daemonruntime.TaskStatus{
		daemonruntime.TaskQueued,
		daemonruntime.TaskRunning,
		daemonruntime.TaskDone,
		daemonruntime.TaskCanceled,
	} {
		job := slackJob{
			TaskID:          string(status),
			ConversationKey: "slack:T:C",
			TeamID:          "T",
			ChannelID:       "C",
		}
		runControlKey := slackRunControlConversationKeyForJob(job)
		lease, err := state.runControl.StartLease(context.Background(), time.Minute, runtimecontrol.ActiveRun{
			Runtime:         "slack",
			ConversationKey: runControlKey,
			TaskID:          string(status),
		})
		if err != nil {
			t.Fatalf("StartLease(%s) error = %v", status, err)
		}
		state.finalizePanickedJob("slack:T:C", job)
		if probe, err := state.runControl.StartLease(context.Background(), time.Minute, runtimecontrol.ActiveRun{
			Runtime:         "slack",
			ConversationKey: runControlKey,
			TaskID:          "probe-" + string(status),
		}); err != nil {
			lease.Finish()
			t.Fatalf("StartLease() after panic for %s error = %v", status, err)
		} else {
			probe.Finish()
		}
	}
	for _, status := range []daemonruntime.TaskStatus{daemonruntime.TaskQueued, daemonruntime.TaskRunning} {
		task, _ := store.Get(string(status))
		if task.Status != daemonruntime.TaskFailed || task.Error != "conversation worker panicked" {
			t.Fatalf("%s task after panic = %#v, want failed", status, task)
		}
	}
	for _, status := range []daemonruntime.TaskStatus{daemonruntime.TaskDone, daemonruntime.TaskCanceled} {
		task, _ := store.Get(string(status))
		if task.Status != status {
			t.Fatalf("%s task after panic = %#v, want unchanged", status, task)
		}
	}
}

func TestSlackRuntimeStateHandlersAreMethods(t *testing.T) {
	state := &slackRuntimeState{botUserID: "U-BOT"}
	err := state.handleBusMessage(context.Background(), busruntime.BusMessage{
		Direction: busruntime.DirectionInbound,
		Channel:   busruntime.ChannelTelegram,
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported inbound channel") {
		t.Fatalf("handleBusMessage() error = %v, want unsupported channel", err)
	}
	if err := state.handleSocketEnvelope(context.Background(), slackSocketEnvelope{Type: "hello"}); err != nil {
		t.Fatalf("handleSocketEnvelope(ignored) error = %v", err)
	}
}

func TestNewSlackRuntimeStatePublishesCompleteOwner(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	inprocBus, err := busruntime.StartInproc(busruntime.BootstrapOptions{
		MaxInFlight: 1,
		Logger:      logger,
		Component:   "slack-constructor-test",
	})
	if err != nil {
		t.Fatalf("StartInproc() error = %v", err)
	}
	contactsStore := contacts.NewFileStore(t.TempDir())
	if err := contactsStore.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	inboundAdapter, err := slackbus.NewInboundAdapter(slackbus.InboundAdapterOptions{Bus: inprocBus, Store: contactsStore})
	if err != nil {
		t.Fatalf("NewInboundAdapter() error = %v", err)
	}
	deliveryAdapter, err := slackbus.NewDeliveryAdapter(slackbus.DeliveryAdapterOptions{
		SendText: func(context.Context, any, string, slackbus.SendTextOptions) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewDeliveryAdapter() error = %v", err)
	}

	state, err := newSlackRuntimeState(slackRuntimeStateConfig{
		ctx:             context.Background(),
		logger:          logger,
		options:         RunOptions{MaxConcurrency: 1},
		api:             &slackAPI{},
		botUserID:       "U-BOT",
		inprocBus:       inprocBus,
		contactsService: contacts.NewService(contactsStore),
		workspaceStore:  workspace.NewStore(filepath.Join(t.TempDir(), "workspaces.json")),
		inboundAdapter:  inboundAdapter,
		deliveryAdapter: deliveryAdapter,
		runtimeBundle: runtimecore.ChannelRuntimeBundle{
			TaskRuntime: &taskruntime.Runtime{},
			Cleanup:     func() {},
		},
	})
	if err != nil {
		t.Fatalf("newSlackRuntimeState() error = %v", err)
	}
	defer state.close()
	if state.runner == nil || state.pendingApprovals == nil || state.stopWorkers == nil || state.workersCtx == nil {
		t.Fatal("constructor published an incomplete Slack runtime owner")
	}
	if state.inprocBus != inprocBus || state.runtimeBundle.TaskRuntime == nil {
		t.Fatal("constructor did not publish the prepared bus and shared runtime")
	}
	if state.server != nil {
		t.Fatal("constructor started the daemon server before publication")
	}
}
