package awareness

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/awarenessutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/memory"
	"github.com/quailyquaily/mistermorph/tools"
)

type awarenessTaskRecordStore struct {
	*daemonruntime.MemoryStore
	upsertErr   error
	updateErr   error
	upsertCalls int
	updateCalls int
}

func (s *awarenessTaskRecordStore) RecordTaskUpsert(info daemonruntime.TaskInfo, _ daemonruntime.TaskTrigger) error {
	s.upsertCalls++
	if s.upsertErr != nil {
		return s.upsertErr
	}
	return s.MemoryStore.Upsert(info)
}

func (s *awarenessTaskRecordStore) RecordTaskUpdate(id string, _ daemonruntime.TaskTrigger, update func(*daemonruntime.TaskInfo)) error {
	s.updateCalls++
	if s.updateErr != nil {
		return s.updateErr
	}
	return s.MemoryStore.Update(id, update)
}

type awarenessMemoryJournal struct {
	appendCalls int
}

type awarenessTaskErrorClient struct {
	err   error
	calls int
}

func (c *awarenessTaskErrorClient) Chat(context.Context, llm.Request) (llm.Result, error) {
	c.calls++
	return llm.Result{}, c.err
}

func (j *awarenessMemoryJournal) Append(memory.MemoryEvent) error {
	j.appendCalls++
	return nil
}

func (*awarenessMemoryJournal) ReplayFrom(cursor memory.JournalCursor, _ int, _ func(memory.JournalRecord) error) (memory.JournalCursor, bool, error) {
	return cursor, true, nil
}

func (*awarenessMemoryJournal) LoadCheckpoint() (memory.JournalCheckpoint, bool, error) {
	return memory.JournalCheckpoint{}, false, nil
}

func (*awarenessMemoryJournal) SaveCheckpoint(memory.JournalCheckpoint) error {
	return nil
}

func TestRunAwarenessTaskRecordsConsoleAwarenessTask(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		RootDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	baseClient := &awarenessPromptCaptureClient{}
	profileClient := &awarenessPromptCaptureClient{}

	_, err = runAwarenessTask(context.Background(), depsutil.CommonDependencies{
		ResolveLLMRouteWithProfile: func(purpose, profile string) (llmutil.ResolvedRoute, error) {
			return llmutil.ResolvedRoute{
				Purpose: purpose,
				Profile: profile,
				ClientConfig: llmconfig.ClientConfig{
					Provider: "openai",
					Model:    "batch-model",
				},
			}, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
			return profileClient, nil
		},
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.PromptSpec{Identity: "identity"}, nil, nil
		},
	}, awarenessTaskOptions{
		Behavior:     awarenessutil.BehaviorCron,
		Client:       baseClient,
		Model:        "test-model",
		LLMProfile:   "batch",
		Task:         "cron task",
		TaskRunID:    "awareness:cron:test",
		Meta:         awarenessutil.BuildCronMeta("cron", "cron-a", time.Now().UTC(), "* * * * *", "UTC", "", nil),
		BaseRegistry: tools.NewRegistry(),
		Config:       agent.Config{MaxSteps: 1},
		TaskStore:    store,
	})
	if err != nil {
		t.Fatalf("runAwarenessTask() error = %v", err)
	}

	items := store.List(daemonruntime.TaskListOptions{Limit: 20, TopicID: daemonruntime.ConsoleAwarenessTopicID})
	if len(items) != 1 {
		t.Fatalf("len(awareness items) = %d, want 1", len(items))
	}
	item := items[0]
	if item.ID != "awareness:cron:test" {
		t.Fatalf("task id = %q, want awareness:cron:test", item.ID)
	}
	if item.Status != daemonruntime.TaskDone {
		t.Fatalf("status = %q, want %q", item.Status, daemonruntime.TaskDone)
	}
	if item.TopicID != daemonruntime.ConsoleAwarenessTopicID {
		t.Fatalf("topic_id = %q, want %q", item.TopicID, daemonruntime.ConsoleAwarenessTopicID)
	}
	if item.Model != "batch-model" {
		t.Fatalf("model = %q, want batch-model", item.Model)
	}
	if item.StartedAt == nil || item.FinishedAt == nil {
		t.Fatalf("started_at/finished_at missing: started=%v finished=%v", item.StartedAt, item.FinishedAt)
	}
	final, _ := item.Result.(map[string]any)["final"].(map[string]any)
	if got := strings.TrimSpace(fmt.Sprint(final["output"])); got != "ok" {
		t.Fatalf("final.output = %q, want ok", got)
	}

	topic, ok := store.GetTopic(daemonruntime.ConsoleAwarenessTopicID)
	if !ok {
		t.Fatal("awareness topic missing")
	}
	if topic.Title != daemonruntime.ConsoleAwarenessTopicTitle {
		t.Fatalf("awareness topic title = %q, want %q", topic.Title, daemonruntime.ConsoleAwarenessTopicTitle)
	}
}

func TestRunAwarenessTaskStopsWhenStartRecordFails(t *testing.T) {
	wantErr := errors.New("persist running task")
	store := &awarenessTaskRecordStore{
		MemoryStore: daemonruntime.NewMemoryStore(10),
		upsertErr:   wantErr,
	}
	client := &awarenessPromptCaptureClient{}
	promptCalls := 0

	summary, err := runAwarenessTask(context.Background(), depsutil.CommonDependencies{
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			promptCalls++
			return agent.PromptSpec{Identity: "identity"}, nil, nil
		},
	}, awarenessTaskOptions{
		Behavior:     awarenessutil.BehaviorCron,
		Client:       client,
		Model:        "test-model",
		Task:         "cron task",
		TaskRunID:    "awareness:cron:start-failure",
		BaseRegistry: tools.NewRegistry(),
		Config:       agent.Config{MaxSteps: 1},
		TaskStore:    store,
	})
	if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "record awareness task start") {
		t.Fatalf("runAwarenessTask() error = %v, want wrapped start persistence error", err)
	}
	if summary != "" {
		t.Fatalf("runAwarenessTask() summary = %q, want empty", summary)
	}
	if len(client.requests) != 0 {
		t.Fatalf("LLM requests = %d, want 0", len(client.requests))
	}
	if promptCalls != 0 {
		t.Fatalf("prompt calls = %d, want 0", promptCalls)
	}
	if store.upsertCalls != 1 || store.updateCalls != 0 {
		t.Fatalf("record calls = upsert:%d update:%d, want upsert:1 update:0", store.upsertCalls, store.updateCalls)
	}
}

func TestRunAwarenessTaskReportsFinishRecordFailureBeforeMemorySideEffect(t *testing.T) {
	wantErr := errors.New("persist finished task")
	store := &awarenessTaskRecordStore{
		MemoryStore: daemonruntime.NewMemoryStore(10),
		updateErr:   wantErr,
	}
	client := &awarenessPromptCaptureClient{}
	journal := &awarenessMemoryJournal{}
	manager := memory.NewManager(t.TempDir(), 7)
	orchestrator, err := memoryruntime.New(
		manager,
		journal,
		memory.NewProjector(manager, journal, memory.ProjectorOptions{}),
		memoryruntime.OrchestratorOptions{},
	)
	if err != nil {
		t.Fatalf("memoryruntime.New() error = %v", err)
	}

	summary, err := runAwarenessTask(context.Background(), depsutil.CommonDependencies{
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.PromptSpec{Identity: "identity"}, nil, nil
		},
	}, awarenessTaskOptions{
		Behavior:           awarenessutil.BehaviorCron,
		Client:             client,
		Model:              "test-model",
		Task:               "cron task",
		TaskRunID:          "awareness:cron:finish-failure",
		BaseRegistry:       tools.NewRegistry(),
		Config:             agent.Config{MaxSteps: 1},
		TaskStore:          store,
		MemoryOrchestrator: orchestrator,
	})
	if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "record awareness task finish") {
		t.Fatalf("runAwarenessTask() error = %v, want wrapped finish persistence error", err)
	}
	if summary != "" {
		t.Fatalf("runAwarenessTask() summary = %q, want empty on uncommitted finish", summary)
	}
	if len(client.requests) != 1 {
		t.Fatalf("LLM requests = %d, want 1", len(client.requests))
	}
	if journal.appendCalls != 0 {
		t.Fatalf("memory journal appends = %d, want 0 before task finish commits", journal.appendCalls)
	}
	if store.upsertCalls != 1 || store.updateCalls != 1 {
		t.Fatalf("record calls = upsert:%d update:%d, want upsert:1 update:1", store.upsertCalls, store.updateCalls)
	}
}

func TestRunAwarenessTaskPreservesExecutionAndFinishRecordErrors(t *testing.T) {
	runErr := errors.New("llm failed")
	finishErr := errors.New("persist failed task")
	store := &awarenessTaskRecordStore{
		MemoryStore: daemonruntime.NewMemoryStore(10),
		updateErr:   finishErr,
	}
	client := &awarenessTaskErrorClient{err: runErr}

	_, err := runAwarenessTask(context.Background(), depsutil.CommonDependencies{
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.PromptSpec{Identity: "identity"}, nil, nil
		},
	}, awarenessTaskOptions{
		Behavior:     awarenessutil.BehaviorCron,
		Client:       client,
		Model:        "test-model",
		Task:         "cron task",
		TaskRunID:    "awareness:cron:execution-and-finish-failure",
		BaseRegistry: tools.NewRegistry(),
		Config:       agent.Config{MaxSteps: 1},
		TaskStore:    store,
	})
	if !errors.Is(err, runErr) {
		t.Fatalf("runAwarenessTask() error = %v, want execution error", err)
	}
	if !errors.Is(err, finishErr) || !strings.Contains(err.Error(), "record awareness task finish") {
		t.Fatalf("runAwarenessTask() error = %v, want wrapped finish persistence error", err)
	}
	if client.calls != 1 {
		t.Fatalf("LLM calls = %d, want 1", client.calls)
	}
	if store.upsertCalls != 1 || store.updateCalls != 1 {
		t.Fatalf("record calls = upsert:%d update:%d, want upsert:1 update:1", store.upsertCalls, store.updateCalls)
	}
}
