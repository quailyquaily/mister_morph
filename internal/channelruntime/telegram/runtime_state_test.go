package telegram

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
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	telegrambus "github.com/quailyquaily/mistermorph/internal/bus/adapters/telegram"
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

type telegramLoopOwner interface {
	runJob(context.Context, string, telegramJob)
	enqueueInbound(context.Context, busruntime.BusMessage) error
	handleBusMessage(context.Context, busruntime.BusMessage) error
	deliverOutbound(context.Context, busruntime.BusMessage) error
	handleUpdate(telegramUpdate)
}

var _ telegramLoopOwner = (*telegramRuntimeState)(nil)

func TestTelegramQueuedJobKeepsAdmissionRouteAfterResolverChanges(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	supportsText := false
	supportsVision := true
	admissionRoute := llmutil.ResolvedRoute{
		Purpose:  llmutil.RoutePurposeThink,
		Identity: "telegram-weighted-think",
		Candidates: []llmutil.ResolvedCandidate{
			{Profile: "text", Weight: 1, Values: llmutil.RuntimeValues{SupportsImageParts: &supportsText}, ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "text-model"}},
			{Profile: "vision", Weight: 1, Values: llmutil.RuntimeValues{SupportsImageParts: &supportsVision}, ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "vision-model"}},
		},
	}
	currentRoute := admissionRoute
	runtime, err := taskruntime.NewRunPreparer(depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) { return logger, nil },
		ResolveLLMRoute: func(purpose string) (llmutil.ResolvedRoute, error) {
			if purpose != llmutil.RoutePurposeThink {
				t.Fatalf("route purpose = %q, want think", purpose)
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
	jobs := make(chan telegramJob, 1)
	state := &telegramRuntimeState{
		workersCtx:          workersCtx,
		logger:              logger,
		sharedRuntime:       runtimecore.ChannelRuntimeBundle{TaskRuntime: runtime},
		lastActivity:        make(map[int64]time.Time),
		lastFromUser:        make(map[int64]int64),
		lastFromUsername:    make(map[int64]string),
		lastFromName:        make(map[int64]string),
		lastFromFirst:       make(map[int64]string),
		lastFromLast:        make(map[int64]string),
		lastChatType:        make(map[int64]string),
		knownMentions:       make(map[int64]map[string]string),
		planProgressByID:    make(map[string]telegramPlanProgressEditState),
		systemWarningsSeen:  make(map[string]bool),
		warningsSentVersion: make(map[string]int),
	}
	state.runner = runtimecore.NewConversationRunner[string, telegramJob](workersCtx, make(chan struct{}, 1), 1, func(_ context.Context, _ string, job telegramJob) {
		jobs <- job
	}, runtimecore.ConversationRunnerOptions[string, telegramJob]{Logger: logger})

	payload, err := busruntime.EncodeMessageEnvelope(busruntime.TopicChatMessage, busruntime.MessageEnvelope{
		MessageID: "telegram:12345:678",
		Text:      "/think inspect this",
		SentAt:    "2026-07-21T00:00:00Z",
		SessionID: "0194e9d5-2f8f-7000-8000-000000000001",
	})
	if err != nil {
		t.Fatalf("EncodeMessageEnvelope() error = %v", err)
	}
	conversationKey, err := busruntime.BuildTelegramTopicConversationKey("12345", 0)
	if err != nil {
		t.Fatalf("BuildTelegramTopicConversationKey() error = %v", err)
	}
	message := busruntime.BusMessage{
		Direction:       busruntime.DirectionInbound,
		Channel:         busruntime.ChannelTelegram,
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: conversationKey,
		PayloadBase64:   payload,
		Extensions: busruntime.MessageExtensions{
			PlatformMessageID: "12345:678",
			ChatType:          "private",
		},
	}
	if err := state.enqueueInbound(context.Background(), message); err != nil {
		t.Fatalf("enqueueInbound() error = %v", err)
	}
	currentRoute = llmutil.ResolvedRoute{Purpose: llmutil.RoutePurposeThink, ClientConfig: llmconfig.ClientConfig{Provider: "changed", Model: "changed-model"}}

	select {
	case job := <-jobs:
		want := llmutil.SelectRouteCandidate(admissionRoute, job.TaskID)
		if job.Route == nil || len(job.Route.Candidates) != 0 {
			t.Fatalf("queued route = %#v, want one concrete admission route", job.Route)
		}
		if job.Route.ClientConfig.Model != want.ClientConfig.Model || job.Route.Profile != want.Profile {
			t.Fatalf("queued route = %#v, want selected route %#v", job.Route, want)
		}
		if job.Route.Values.ReasoningEffortRaw != llmutil.ReasoningEffortXHigh {
			t.Fatalf("reasoning effort = %q, want %q", job.Route.Values.ReasoningEffortRaw, llmutil.ReasoningEffortXHigh)
		}
		if job.Route.ClientConfig.Model == "changed-model" {
			t.Fatal("queued job used route selected after admission")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for queued job")
	}
}

type telegramRuntimeTestListener struct {
	acceptStarted chan struct{}
	closed        chan struct{}
	acceptOnce    sync.Once
	closeOnce     sync.Once
}

type telegramRuntimeSingleConnListener struct {
	mu        sync.Mutex
	conn      net.Conn
	delivered bool
	closed    chan struct{}
	closeOnce sync.Once
}

type telegramRuntimeShutdownErrorServer struct {
	err   error
	calls atomic.Int32
}

func (s *telegramRuntimeShutdownErrorServer) Shutdown(context.Context) error {
	s.calls.Add(1)
	return s.err
}

func (l *telegramRuntimeSingleConnListener) Accept() (net.Conn, error) {
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

func (l *telegramRuntimeSingleConnListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func (*telegramRuntimeSingleConnListener) Addr() net.Addr { return &net.TCPAddr{} }

func (l *telegramRuntimeTestListener) Accept() (net.Conn, error) {
	l.acceptOnce.Do(func() { close(l.acceptStarted) })
	<-l.closed
	return nil, net.ErrClosed
}

func (l *telegramRuntimeTestListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func (*telegramRuntimeTestListener) Addr() net.Addr {
	return &net.TCPAddr{}
}

func TestTelegramRuntimeStateDoesNotPublishDaemonRoutesBeforeRunnerIsReady(t *testing.T) {
	state := &telegramRuntimeState{}
	if _, err := state.daemonRoutes(); err == nil || !strings.Contains(err.Error(), "runner") {
		t.Fatalf("daemonRoutes() error = %v, want uninitialized runner error", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state.runner = runtimecore.NewConversationRunner[string, telegramJob](
		ctx,
		make(chan struct{}, 1),
		1,
		func(context.Context, string, telegramJob) {},
		runtimecore.ConversationRunnerOptions[string, telegramJob]{},
	)
	routes, err := state.daemonRoutes()
	if err != nil {
		t.Fatalf("daemonRoutes() error = %v", err)
	}
	if routes.Approvals.List == nil || routes.Approvals.Approve == nil || routes.Approvals.Deny == nil {
		t.Fatal("daemon approval routes are incomplete")
	}
}

func TestTelegramDaemonProfilesUseRuntimeSettingsSnapshot(t *testing.T) {
	settings := viper.New()
	settings.Set("llm.profiles", map[string]any{
		"telegram-profile": map[string]any{"model": "model-a"},
	})
	global := viper.GetViper()
	global.Set("llm.profiles", map[string]any{
		"global-profile": map[string]any{"model": "model-b"},
	})
	t.Cleanup(func() { global.Set("llm.profiles", nil) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &telegramRuntimeState{
		dependencies: Dependencies{CommonDependencies: depsutil.CommonDependencies{
			AgentSettingsReader: agentsettings.NewReaderSnapshot(settings),
		}},
		options: RunOptions{Server: ServerOptions{AuthToken: "token"}},
		runner: runtimecore.NewConversationRunner[string, telegramJob](
			ctx,
			make(chan struct{}, 1),
			1,
			func(context.Context, string, telegramJob) {},
			runtimecore.ConversationRunnerOptions[string, telegramJob]{},
		),
	}
	routes, err := state.daemonRoutes()
	if err != nil {
		t.Fatalf("daemonRoutes() error = %v", err)
	}
	handler := daemonruntime.NewHandler(routes)
	req := httptest.NewRequest(http.MethodGet, "/llm/profiles", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].Name != "telegram-profile" {
		t.Fatalf("items = %#v, want telegram runtime profile", payload.Items)
	}
}

func TestTelegramRuntimeStateCloseOwnsChannelResources(t *testing.T) {
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
	inprocBus, err := busruntime.NewInproc(busruntime.InprocOptions{MaxInFlight: 1, Logger: logger})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}
	workersCtx, stopWorkers := context.WithCancel(context.Background())
	pending := runtimecore.NewPendingApprovalRegistry[telegramJob](nil)
	pending.Register("approval-1", telegramJob{TaskID: "task-1"}, time.Now().Add(time.Hour))
	var sharedCleanupCalls atomic.Int32
	var cleanupBusErr error
	server := &http.Server{}
	listener := &telegramRuntimeTestListener{
		acceptStarted: make(chan struct{}),
		closed:        make(chan struct{}),
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	select {
	case <-listener.acceptStarted:
	case <-time.After(time.Second):
		t.Fatal("test server did not start accepting")
	}
	state := &telegramRuntimeState{
		workersCtx:       workersCtx,
		stopWorkers:      stopWorkers,
		logger:           logger,
		taskStore:        taskStore,
		pendingApprovals: pending,
		inprocBus:        inprocBus,
		server:           server,
		sharedRuntime: runtimecore.ChannelRuntimeBundle{
			Cleanup: func() {
				sharedCleanupCalls.Add(1)
				cleanupBusErr = inprocBus.Publish(context.Background(), busruntime.BusMessage{Topic: busruntime.TopicChatMessage})
			},
		},
	}

	state.close()
	state.close()

	select {
	case <-workersCtx.Done():
	default:
		t.Fatal("workers context is still active after close")
	}
	if got := sharedCleanupCalls.Load(); got != 1 {
		t.Fatalf("shared runtime cleanup calls = %d, want 1", got)
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
	if task.Status != daemonruntime.TaskFailed || task.Error != "telegram runtime closed" || task.FinishedAt == nil {
		t.Fatalf("pending approval task after close = %#v, want failed terminal state", task)
	}
	if task.PendingAt != nil || task.ApprovalRequestID != "" || task.Result != nil {
		t.Fatalf("pending approval fields after close = %v/%q/%#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
	}
	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("Serve() error = %v, want http.ErrServerClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("owned server is still serving after close")
	}
	err = inprocBus.Publish(context.Background(), busruntime.BusMessage{
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: "tg:1",
	})
	if err == nil || !strings.Contains(err.Error(), "bus is closed") {
		t.Fatalf("publish after close error = %v, want closed bus", err)
	}
}

func TestTelegramRuntimeStateCloseWaitsForRunnerBeforeCleanup(t *testing.T) {
	workersCtx, stopWorkers := context.WithCancel(context.Background())
	handlerStarted := make(chan struct{})
	handlerCanceled := make(chan struct{})
	allowHandlerExit := make(chan struct{})
	cleanupCalled := make(chan struct{}, 1)
	closeDone := make(chan struct{})
	state := &telegramRuntimeState{
		workersCtx:  workersCtx,
		stopWorkers: stopWorkers,
		sharedRuntime: runtimecore.ChannelRuntimeBundle{Cleanup: func() {
			cleanupCalled <- struct{}{}
		}},
	}
	state.runner = runtimecore.NewConversationRunner[string, telegramJob](
		workersCtx,
		make(chan struct{}, 1),
		1,
		func(ctx context.Context, _ string, _ telegramJob) {
			close(handlerStarted)
			<-ctx.Done()
			close(handlerCanceled)
			<-allowHandlerExit
		},
		runtimecore.ConversationRunnerOptions[string, telegramJob]{},
	)
	if err := state.runner.Enqueue(context.Background(), "telegram:close", func(uint64) telegramJob {
		return telegramJob{TaskID: "task-active"}
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

func TestTelegramRuntimeStateCloseWaitsForDaemonHandlerBeforeCleanup(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	listener := &telegramRuntimeSingleConnListener{conn: serverConn, closed: make(chan struct{})}
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
	state := &telegramRuntimeState{
		server: server,
		sharedRuntime: runtimecore.ChannelRuntimeBundle{Cleanup: func() {
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
	case <-time.After(50 * time.Millisecond):
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

func TestTelegramRuntimeStateShutdownErrorStillCleansOwnedResources(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	inprocBus, err := busruntime.NewInproc(busruntime.InprocOptions{MaxInFlight: 1, Logger: logger})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}
	workersCtx, stopWorkers := context.WithCancel(context.Background())
	server := &telegramRuntimeShutdownErrorServer{err: errors.New("listener close failed")}
	var cleanupCalls atomic.Int32
	state := &telegramRuntimeState{
		logger:      logger,
		workersCtx:  workersCtx,
		stopWorkers: stopWorkers,
		server:      server,
		inprocBus:   inprocBus,
		sharedRuntime: runtimecore.ChannelRuntimeBundle{Cleanup: func() {
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
		t.Fatalf("shared cleanup calls = %d, want 1", got)
	}
	if err := inprocBus.Publish(context.Background(), busruntime.BusMessage{Topic: busruntime.TopicChatMessage}); err == nil {
		t.Fatal("in-process bus remained open after shutdown error")
	}
}

func TestTelegramRunJobCancellationMarksActiveTaskCanceled(t *testing.T) {
	store := daemonruntime.NewMemoryStore(4)
	store.Upsert(daemonruntime.TaskInfo{ID: "task-active", Status: daemonruntime.TaskQueued})
	runnerCtx, cancelRunner := context.WithCancel(context.Background())
	defer cancelRunner()
	state := &telegramRuntimeState{
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		taskStore:          store,
		runControl:         runtimecontrol.New(),
		history:            make(map[string][]chathistory.ChatHistoryItem),
		stickySkillsByChat: make(map[string][]string),
	}
	state.runner = runtimecore.NewConversationRunner[string, telegramJob](
		runnerCtx,
		make(chan struct{}, 1),
		1,
		func(context.Context, string, telegramJob) {},
		runtimecore.ConversationRunnerOptions[string, telegramJob]{},
	)
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	cancelWorker()
	state.runJob(workerCtx, "telegram:active", telegramJob{TaskID: "task-active", ConversationKey: "telegram:active", Text: "work"})

	task, _ := store.Get("task-active")
	if task.Status != daemonruntime.TaskCanceled || task.Error != "telegram runtime closed" || task.FinishedAt == nil {
		t.Fatalf("active task after worker cancellation = %#v, want canceled", task)
	}
}

func TestTelegramDroppedJobOnlyTerminatesQueuedOrRunningTask(t *testing.T) {
	store := daemonruntime.NewMemoryStore(8)
	for _, status := range []daemonruntime.TaskStatus{
		daemonruntime.TaskQueued,
		daemonruntime.TaskRunning,
		daemonruntime.TaskPending,
		daemonruntime.TaskDone,
	} {
		store.Upsert(daemonruntime.TaskInfo{ID: string(status), Status: status})
	}
	state := &telegramRuntimeState{taskStore: store, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, status := range []daemonruntime.TaskStatus{
		daemonruntime.TaskQueued,
		daemonruntime.TaskRunning,
		daemonruntime.TaskPending,
		daemonruntime.TaskDone,
	} {
		state.finalizeRuntimeClosedJob("telegram:test", telegramJob{TaskID: string(status)})
	}
	for _, status := range []daemonruntime.TaskStatus{daemonruntime.TaskQueued, daemonruntime.TaskRunning} {
		task, _ := store.Get(string(status))
		if task.Status != daemonruntime.TaskCanceled || task.Error != "telegram runtime closed" {
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

func TestTelegramPanickedJobDoesNotOverwriteTerminalTask(t *testing.T) {
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
	state := &telegramRuntimeState{
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
		lease, err := state.runControl.StartLease(context.Background(), time.Minute, runtimecontrol.ActiveRun{
			Runtime:         "telegram",
			ConversationKey: "telegram:test",
			TaskID:          string(status),
		})
		if err != nil {
			t.Fatalf("StartLease(%s) error = %v", status, err)
		}
		state.finalizePanickedJob("telegram:test", telegramJob{TaskID: string(status)})
		if probe, err := state.runControl.StartLease(context.Background(), time.Minute, runtimecontrol.ActiveRun{
			Runtime:         "telegram",
			ConversationKey: "telegram:test",
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

func TestNewTelegramRuntimeStatePublishesCompleteOwner(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	inprocBus, err := busruntime.StartInproc(busruntime.BootstrapOptions{
		MaxInFlight: 1,
		Logger:      logger,
		Component:   "telegram-constructor-test",
	})
	if err != nil {
		t.Fatalf("StartInproc() error = %v", err)
	}
	contactsStore := contacts.NewFileStore(t.TempDir())
	if err := contactsStore.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	inboundAdapter, err := telegrambus.NewInboundAdapter(telegrambus.InboundAdapterOptions{
		Bus:   inprocBus,
		Store: contactsStore,
	})
	if err != nil {
		t.Fatalf("NewInboundAdapter() error = %v", err)
	}
	sharedGuard := guard.New(guard.Config{Enabled: true}, nil, nil)
	runtimeBundle := runtimecore.ChannelRuntimeBundle{
		TaskRuntime: &taskruntime.Runtime{SharedGuard: sharedGuard},
		Cleanup:     func() {},
	}

	state, err := newTelegramRuntimeState(telegramRuntimeStateConfig{
		ctx:             context.Background(),
		logger:          logger,
		options:         RunOptions{MaxConcurrency: 1},
		api:             &telegramAPI{},
		inprocBus:       inprocBus,
		contactsService: contacts.NewService(contactsStore),
		workspaceStore:  workspace.NewStore(filepath.Join(t.TempDir(), "workspaces.json")),
		inboundAdapter:  inboundAdapter,
		runtimeBundle:   runtimeBundle,
	})
	if err != nil {
		t.Fatalf("newTelegramRuntimeState() error = %v", err)
	}
	defer state.close()
	if state.runner == nil || state.pendingApprovals == nil || state.stopWorkers == nil || state.workersCtx == nil {
		t.Fatal("constructor published an incomplete Telegram runtime owner")
	}
	if state.inprocBus != inprocBus || state.sharedRuntime.TaskRuntime != runtimeBundle.TaskRuntime {
		t.Fatal("constructor did not publish the prepared bus and shared runtime")
	}
	if state.guard != sharedGuard {
		t.Fatal("constructor did not use the task runtime's guard")
	}
	if state.server != nil {
		t.Fatal("constructor started the daemon server before publication")
	}
}
