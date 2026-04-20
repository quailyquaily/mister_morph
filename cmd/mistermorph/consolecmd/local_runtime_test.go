package consolecmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	heartbeatloop "github.com/quailyquaily/mistermorph/internal/channelruntime/heartbeat"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/heartbeatutil"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/spf13/viper"
)

func TestConsoleLocalRoutesOptionsPoke(t *testing.T) {
	rt := &consoleLocalRuntime{}
	if got := rt.routesOptions("token").Poke; got != nil {
		t.Fatalf("Poke = %#v, want nil when heartbeat loop is unavailable", got)
	}

	rt.heartbeatPokeRequests = make(chan heartbeatloop.PokeRequest)
	if got := rt.routesOptions("token").Poke; got == nil {
		t.Fatal("Poke = nil, want non-nil when heartbeat loop is available")
	}
}

func TestConsoleLocalRoutesOptionsOverviewHeartbeatRunning(t *testing.T) {
	reader := viper.New()
	reader.Set("telegram.bot_token", "tg-token")
	reader.Set("slack.bot_token", "slack-bot")
	reader.Set("slack.app_token", "slack-app")
	rt := &consoleLocalRuntime{
		generation:            &consoleLocalRuntimeGeneration{reader: reader},
		heartbeatState:        &heartbeatutil.State{},
		heartbeatPokeRequests: make(chan heartbeatloop.PokeRequest),
	}
	if ok := rt.heartbeatState.Start(); !ok {
		t.Fatal("Start() = false, want true")
	}

	payload, err := rt.routesOptions("token").Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if got, _ := payload["heartbeat_running"].(bool); !got {
		t.Fatalf("heartbeat_running = %v, want true", payload["heartbeat_running"])
	}
}

func TestConsoleLocalRuntimeCompleteHeartbeatTask(t *testing.T) {
	t.Run("success clears running and records timestamp", func(t *testing.T) {
		now := time.Date(2026, time.March, 23, 10, 0, 0, 0, time.UTC)
		rt := &consoleLocalRuntime{heartbeatState: &heartbeatutil.State{}}
		if ok := rt.heartbeatState.Start(); !ok {
			t.Fatal("Start() = false, want true")
		}

		rt.completeHeartbeatTask(consoleLocalTaskJob{
			Trigger: daemonruntime.TaskTrigger{Source: "heartbeat"},
		}, heartbeatTaskResultSuccess, nil, now)

		failures, lastSuccess, lastError, running := rt.heartbeatState.Snapshot()
		if running {
			t.Fatal("running = true, want false")
		}
		if failures != 0 {
			t.Fatalf("failures = %d, want 0", failures)
		}
		if lastError != "" {
			t.Fatalf("lastError = %q, want empty", lastError)
		}
		if !lastSuccess.Equal(now) {
			t.Fatalf("lastSuccess = %v, want %v", lastSuccess, now)
		}
	})

	t.Run("failure clears running and records error", func(t *testing.T) {
		rt := &consoleLocalRuntime{heartbeatState: &heartbeatutil.State{}}
		if ok := rt.heartbeatState.Start(); !ok {
			t.Fatal("Start() = false, want true")
		}

		rt.completeHeartbeatTask(consoleLocalTaskJob{
			Trigger: daemonruntime.TaskTrigger{Source: "heartbeat"},
		}, heartbeatTaskResultFailure, errors.New("boom"), time.Time{})

		failures, _, lastError, running := rt.heartbeatState.Snapshot()
		if running {
			t.Fatal("running = true, want false")
		}
		if failures != 1 {
			t.Fatalf("failures = %d, want 1", failures)
		}
		if lastError != "boom" {
			t.Fatalf("lastError = %q, want %q", lastError, "boom")
		}
	})

	t.Run("skipped clears running without failure", func(t *testing.T) {
		rt := &consoleLocalRuntime{heartbeatState: &heartbeatutil.State{}}
		if ok := rt.heartbeatState.Start(); !ok {
			t.Fatal("Start() = false, want true")
		}

		rt.completeHeartbeatTask(consoleLocalTaskJob{
			Trigger: daemonruntime.TaskTrigger{Source: "heartbeat"},
		}, heartbeatTaskResultSkipped, nil, time.Time{})

		failures, lastSuccess, lastError, running := rt.heartbeatState.Snapshot()
		if running {
			t.Fatal("running = true, want false")
		}
		if failures != 0 {
			t.Fatalf("failures = %d, want 0", failures)
		}
		if !lastSuccess.IsZero() {
			t.Fatalf("lastSuccess = %v, want zero", lastSuccess)
		}
		if lastError != "" {
			t.Fatalf("lastError = %q, want empty", lastError)
		}
	})
}

func TestConsoleTopicTitleFromOutput(t *testing.T) {
	t.Run("short output becomes title", func(t *testing.T) {
		got := consoleTopicTitleFromOutput("  Short answer.  ")
		if got != "Short answer" {
			t.Fatalf("consoleTopicTitleFromOutput() = %q, want %q", got, "Short answer")
		}
	})

	t.Run("long output requires llm", func(t *testing.T) {
		got := consoleTopicTitleFromOutput(strings.Repeat("a", consoleTopicTitleDirectOutputMaxRunes+1))
		if got != "" {
			t.Fatalf("consoleTopicTitleFromOutput() = %q, want empty", got)
		}
	})
}

func TestConsoleLocalRuntimeMaybeRefreshTopicTitleUsesShortOutput(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		HeartbeatTopicID: "_heartbeat",
		Persist:          false,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	topic, err := store.CreateTopic("seed title")
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}

	rt := &consoleLocalRuntime{store: store}
	rt.maybeRefreshTopicTitle(consoleLocalTaskJob{
		TopicID:         topic.ID,
		Task:            "first task",
		AutoRenameTopic: true,
	}, "Direct title")

	updated, ok := store.GetTopic(topic.ID)
	if !ok || updated == nil {
		t.Fatalf("GetTopic(%q) missing", topic.ID)
	}
	if updated.Title != "Direct title" {
		t.Fatalf("updated.Title = %q, want %q", updated.Title, "Direct title")
	}
	if updated.LLMTitleGeneratedAt != nil {
		t.Fatal("updated.LLMTitleGeneratedAt != nil, want nil for direct title path")
	}
}

func TestBuildConsoleTaskResultMetricsUsesSnakeCase(t *testing.T) {
	start := time.Date(2026, time.April, 4, 12, 34, 56, 0, time.UTC)
	result := buildConsoleTaskResult(&agent.Final{Output: "done"}, &agent.Context{
		Metrics: &agent.Metrics{
			LLMRounds:    3,
			TotalTokens:  120,
			TotalCost:    0.42,
			StartTime:    start,
			ElapsedMs:    1500,
			ToolCalls:    2,
			ParseRetries: 1,
		},
	})

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal(result) error = %v", err)
	}

	var payload struct {
		Metrics map[string]any `json:"metrics"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("json.Unmarshal(result) error = %v", err)
	}

	if payload.Metrics["llm_rounds"] != float64(3) {
		t.Fatalf("metrics.llm_rounds = %#v, want 3", payload.Metrics["llm_rounds"])
	}
	if payload.Metrics["total_tokens"] != float64(120) {
		t.Fatalf("metrics.total_tokens = %#v, want 120", payload.Metrics["total_tokens"])
	}
	if payload.Metrics["total_cost"] != 0.42 {
		t.Fatalf("metrics.total_cost = %#v, want 0.42", payload.Metrics["total_cost"])
	}
	if payload.Metrics["elapsed_ms"] != float64(1500) {
		t.Fatalf("metrics.elapsed_ms = %#v, want 1500", payload.Metrics["elapsed_ms"])
	}
	if payload.Metrics["tool_calls"] != float64(2) {
		t.Fatalf("metrics.tool_calls = %#v, want 2", payload.Metrics["tool_calls"])
	}
	if payload.Metrics["parse_retries"] != float64(1) {
		t.Fatalf("metrics.parse_retries = %#v, want 1", payload.Metrics["parse_retries"])
	}
	if got := payload.Metrics["start_time"]; got != start.Format(time.RFC3339) {
		t.Fatalf("metrics.start_time = %#v, want %q", got, start.Format(time.RFC3339))
	}
	if _, ok := payload.Metrics["LLMRounds"]; ok {
		t.Fatalf("metrics unexpectedly contains camelCase key: %#v", payload.Metrics)
	}
	if _, ok := payload.Metrics["TotalTokens"]; ok {
		t.Fatalf("metrics unexpectedly contains camelCase key: %#v", payload.Metrics)
	}
}

func TestBuildConsoleTopicHistoryUsesRecentPriorTasks(t *testing.T) {
	base := time.Date(2026, time.March, 23, 10, 0, 0, 0, time.UTC)
	tasks := make([]daemonruntime.TaskInfo, 0, 10)
	for i := 9; i >= 1; i-- {
		createdAt := base.Add(time.Duration(i) * time.Minute)
		finishedAt := createdAt.Add(15 * time.Second)
		result := map[string]any{
			"final": map[string]any{
				"output": fmt.Sprintf("answer %d", i),
			},
		}
		if i == 7 {
			result = map[string]any{"output": "answer 7"}
		}
		tasks = append(tasks, daemonruntime.TaskInfo{
			ID:         fmt.Sprintf("task_%02d", i),
			Status:     daemonruntime.TaskDone,
			Task:       fmt.Sprintf("question %d", i),
			CreatedAt:  createdAt,
			FinishedAt: &finishedAt,
			TopicID:    "topic_a",
			Result:     result,
		})
	}
	tasks = append(tasks, daemonruntime.TaskInfo{
		ID:        "task_future",
		Status:    daemonruntime.TaskDone,
		Task:      "future question",
		CreatedAt: base.Add(11 * time.Minute),
		TopicID:   "topic_a",
		Result:    map[string]any{"output": "future answer"},
	})

	history := buildConsoleTopicHistory(tasks, consoleLocalTaskJob{
		TaskID:     "task_current",
		TopicID:    "topic_a",
		Task:       "current question",
		CreatedAt:  base.Add(10 * time.Minute),
		Trigger:    daemonruntime.TaskTrigger{Source: "ui"},
		Timeout:    time.Minute,
		Version:    1,
		WakeSignal: daemonruntime.PokeInput{},
	}, consoleHistoryRestoreTaskLimit)

	if len(history) != 12 {
		t.Fatalf("len(history) = %d, want 12", len(history))
	}

	gotTexts := make([]string, 0, len(history))
	for _, item := range history {
		gotTexts = append(gotTexts, item.Text)
	}
	wantTexts := []string{
		"question 4", "answer 4",
		"question 5", "answer 5",
		"question 6", "answer 6",
		"question 7", "answer 7",
		"question 8", "answer 8",
		"question 9", "answer 9",
	}
	if strings.Join(gotTexts, "\n") != strings.Join(wantTexts, "\n") {
		t.Fatalf("history texts = %#v, want %#v", gotTexts, wantTexts)
	}
}

func TestConsoleLocalRuntimeLoadConsoleTopicHistoryReplaysPersistedTasks(t *testing.T) {
	root := t.TempDir()
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		RootDir:          root,
		HeartbeatTopicID: "_heartbeat",
		Persist:          true,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	topic, err := store.CreateTopic("Topic A")
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}

	base := time.Date(2026, time.March, 23, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 8; i++ {
		createdAt := base.Add(time.Duration(i) * time.Minute)
		finishedAt := createdAt.Add(30 * time.Second)
		result := map[string]any{
			"final": map[string]any{
				"output": fmt.Sprintf("persisted answer %d", i),
			},
		}
		if i == 6 {
			result = map[string]any{"output": "persisted answer 6"}
		}
		if err := store.UpsertWithTrigger(daemonruntime.TaskInfo{
			ID:         fmt.Sprintf("persisted_task_%02d", i),
			Status:     daemonruntime.TaskDone,
			Task:       fmt.Sprintf("persisted question %d", i),
			Model:      "gpt-5.2",
			Timeout:    "10m0s",
			CreatedAt:  createdAt,
			FinishedAt: &finishedAt,
			TopicID:    topic.ID,
			Result:     result,
		}, daemonruntime.TaskTrigger{Source: "ui", Event: "chat_submit"}, ""); err != nil {
			t.Fatalf("UpsertWithTrigger(done %d) error = %v", i, err)
		}
	}

	currentCreatedAt := base.Add(9 * time.Minute)
	if err := store.UpsertWithTrigger(daemonruntime.TaskInfo{
		ID:        "persisted_task_current",
		Status:    daemonruntime.TaskQueued,
		Task:      "current persisted question",
		Model:     "gpt-5.2",
		Timeout:   "10m0s",
		CreatedAt: currentCreatedAt,
		TopicID:   topic.ID,
	}, daemonruntime.TaskTrigger{Source: "ui", Event: "chat_submit"}, ""); err != nil {
		t.Fatalf("UpsertWithTrigger(current) error = %v", err)
	}

	reloaded, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		RootDir:          root,
		HeartbeatTopicID: "_heartbeat",
		Persist:          true,
	})
	if err != nil {
		t.Fatalf("reload NewConsoleFileStore() error = %v", err)
	}

	rt := &consoleLocalRuntime{store: reloaded}
	history := rt.loadConsoleTopicHistory(consoleLocalTaskJob{
		TaskID:    "persisted_task_current",
		TopicID:   topic.ID,
		Task:      "current persisted question",
		CreatedAt: currentCreatedAt,
	})
	if len(history) != 12 {
		t.Fatalf("len(history) = %d, want 12", len(history))
	}
	if history[0].Kind != chathistory.KindInboundUser || history[0].Text != "persisted question 3" {
		t.Fatalf("history[0] = %#v, want persisted question 3 inbound", history[0])
	}
	last := history[len(history)-1]
	if last.Kind != chathistory.KindOutboundAgent || last.Text != "persisted answer 8" {
		t.Fatalf("history[last] = %#v, want persisted answer 8 outbound", last)
	}
}

func TestConsoleLocalRuntimeHandleConsoleBusInboundUsesPendingJobGeneration(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		HeartbeatTopicID: "_heartbeat",
		Persist:          false,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}

	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	jobs := make(chan consoleLocalTaskJob, 1)
	rt := &consoleLocalRuntime{
		store:       store,
		pendingJobs: map[string]consoleLocalTaskJob{},
	}
	rt.runner = runtimecore.NewConversationRunner[string, consoleLocalTaskJob](
		workerCtx,
		make(chan struct{}, 1),
		1,
		func(_ context.Context, _ string, job consoleLocalTaskJob) {
			jobs <- job
		},
	)

	oldReader := viper.New()
	oldReader.Set("timeout", "2m")
	newReader := viper.New()
	newReader.Set("timeout", "9m")
	oldGeneration := &consoleLocalRuntimeGeneration{generation: 1, reader: oldReader}
	newGeneration := &consoleLocalRuntimeGeneration{generation: 2, reader: newReader}
	rt.generation = newGeneration

	oldGeneration.acquire()
	job, _, err := rt.acceptTask(
		oldGeneration,
		"hello",
		"",
		time.Minute,
		"",
		"",
		daemonruntime.TaskTrigger{Source: "ui", Event: "chat_submit", Ref: "web/console"},
	)
	if err != nil {
		t.Fatalf("acceptTask() error = %v", err)
	}
	rt.pendingJobs[job.TaskID] = job

	err = rt.handleConsoleBusInbound(context.Background(), busruntime.BusMessage{
		Channel:       busruntime.ChannelConsole,
		Direction:     busruntime.DirectionInbound,
		CorrelationID: job.TaskID,
	})
	if err != nil {
		t.Fatalf("handleConsoleBusInbound() error = %v", err)
	}

	select {
	case queued := <-jobs:
		if queued.Generation != oldGeneration {
			t.Fatalf("queued.Generation = %#v, want old generation %#v", queued.Generation, oldGeneration)
		}
		if queued.Timeout != time.Minute {
			t.Fatalf("queued.Timeout = %v, want %v", queued.Timeout, time.Minute)
		}
		queued.Generation.release()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for queued job")
	}

	if _, ok := rt.pendingJobs[job.TaskID]; ok {
		t.Fatalf("pendingJobs[%q] still exists, want removed after enqueue", job.TaskID)
	}
}

func TestConsoleLocalRuntimeAcceptTaskLoadsWorkspaceAttachment(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		HeartbeatTopicID: "_heartbeat",
		Persist:          false,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	topic, err := store.CreateTopic("Topic A")
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}

	workspaceRoot := t.TempDir()
	attachmentsPath := filepath.Join(workspaceRoot, "workspace_attachments.json")
	workspaceStore := workspace.NewStore(attachmentsPath)
	if _, _, err := workspaceStore.Set(buildConsoleConversationKey(topic.ID), workspace.Attachment{WorkspaceDir: workspaceRoot}); err != nil {
		t.Fatalf("workspaceStore.Set() error = %v", err)
	}

	generation := &consoleLocalRuntimeGeneration{reader: viper.New()}
	rt := &consoleLocalRuntime{
		store:          store,
		workspaceStore: workspaceStore,
	}

	job, _, err := rt.acceptTask(
		generation,
		"hello",
		"",
		time.Minute,
		topic.ID,
		"",
		daemonruntime.TaskTrigger{Source: "ui", Event: "chat_submit", Ref: "web/console"},
	)
	if err != nil {
		t.Fatalf("acceptTask() error = %v", err)
	}
	if job.WorkspaceDir != workspaceRoot {
		t.Fatalf("job.WorkspaceDir = %q, want %q", job.WorkspaceDir, workspaceRoot)
	}
}

func TestConsoleLocalRuntimeDeleteTopicRemovesWorkspaceAttachment(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		HeartbeatTopicID: "_heartbeat",
		Persist:          false,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	topic, err := store.CreateTopic("Topic A")
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}

	workspaceRoot := t.TempDir()
	attachmentsPath := filepath.Join(workspaceRoot, "workspace_attachments.json")
	workspaceStore := workspace.NewStore(attachmentsPath)
	if _, _, err := workspaceStore.Set(buildConsoleConversationKey(topic.ID), workspace.Attachment{WorkspaceDir: workspaceRoot}); err != nil {
		t.Fatalf("workspaceStore.Set() error = %v", err)
	}

	rt := &consoleLocalRuntime{
		store:          store,
		workspaceStore: workspaceStore,
	}
	if !rt.deleteTopic(topic.ID) {
		t.Fatalf("deleteTopic(%q) = false, want true", topic.ID)
	}
	currentDir, err := workspace.LookupWorkspaceDir(workspaceStore, buildConsoleConversationKey(topic.ID))
	if err != nil {
		t.Fatalf("LookupWorkspaceDir() error = %v", err)
	}
	if currentDir != "" {
		t.Fatalf("currentDir = %q, want empty after topic delete", currentDir)
	}
}

func TestConsoleLocalRuntimeSubmitTaskHandlesWorkspaceCommand(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		HeartbeatTopicID: "_heartbeat",
		Persist:          false,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	topic, err := store.CreateTopic("Topic A")
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}

	workspaceRoot := t.TempDir()
	attachmentsPath := filepath.Join(workspaceRoot, "workspace_attachments.json")
	workspaceStore := workspace.NewStore(attachmentsPath)
	reader := viper.New()
	generation := &consoleLocalRuntimeGeneration{reader: reader}
	rt := &consoleLocalRuntime{
		store:          store,
		workspaceStore: workspaceStore,
		generation:     generation,
	}

	resp, err := rt.submitTask(context.Background(), daemonruntime.SubmitTaskRequest{
		Task:    "/workspace attach " + workspaceRoot,
		TopicID: topic.ID,
	})
	if err != nil {
		t.Fatalf("submitTask() error = %v", err)
	}
	if resp.Status != daemonruntime.TaskDone {
		t.Fatalf("resp.Status = %q, want %q", resp.Status, daemonruntime.TaskDone)
	}
	if resp.TopicID != topic.ID {
		t.Fatalf("resp.TopicID = %q, want %q", resp.TopicID, topic.ID)
	}
	currentDir, err := workspace.LookupWorkspaceDir(workspaceStore, buildConsoleConversationKey(topic.ID))
	if err != nil {
		t.Fatalf("LookupWorkspaceDir() error = %v", err)
	}
	if currentDir != workspaceRoot {
		t.Fatalf("currentDir = %q, want %q", currentDir, workspaceRoot)
	}
	task, ok := store.Get(resp.ID)
	if !ok || task == nil {
		t.Fatalf("store.Get(%q) missing", resp.ID)
	}
	result, _ := task.Result.(map[string]any)
	final, _ := result["final"].(map[string]any)
	if got := strings.TrimSpace(fmt.Sprint(final["output"])); got != "workspace attached: "+workspaceRoot {
		t.Fatalf("final.output = %q, want %q", got, "workspace attached: "+workspaceRoot)
	}
}
