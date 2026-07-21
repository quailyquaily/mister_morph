package integration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/domainjournal"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/taskdomain"
	"github.com/quailyquaily/mistermorph/llm"
)

func TestRunTaskWithOptionsGeneratesIDsAndDoesNotInventTraceOrTopic(t *testing.T) {
	chat := func(ctx context.Context, req llm.Request) (llm.Result, error) {
		return llm.Result{Text: `{"type":"final","output":"ok"}`}, nil
	}

	cfg := DefaultConfig()
	cfg.Features.Skills = false
	cfg.Set("file_state_dir", t.TempDir())
	cfg.Set("llm.provider", "openai")
	cfg.Set("llm.model", "gpt-5.2")

	rt := newRuntimeWithStubIntegrationClient(cfg, chat)
	result, err := rt.RunTaskWithOptions(context.Background(), "ping", RunTaskOptions{})
	if err != nil {
		t.Fatalf("RunTaskWithOptions() error = %v", err)
	}
	if strings.TrimSpace(result.TaskID) == "" {
		t.Fatal("result.TaskID is empty")
	}
	if !strings.HasPrefix(result.TaskID, "integration_") {
		t.Fatalf("result.TaskID = %q, want integration_ prefix", result.TaskID)
	}
	if result.RunID != result.TaskID {
		t.Fatalf("result.RunID = %q, want task id %q", result.RunID, result.TaskID)
	}
	if result.TraceID != "" {
		t.Fatalf("result.TraceID = %q, want empty", result.TraceID)
	}
	if result.TopicID != "" {
		t.Fatalf("result.TopicID = %q, want empty", result.TopicID)
	}
}

func TestRunTaskWithOptionsInjectsMetaAndPersistsTaskJournal(t *testing.T) {
	var captured llm.Request
	chat := func(ctx context.Context, req llm.Request) (llm.Result, error) {
		captured = req
		return llm.Result{Text: `{"type":"final","output":"ok"}`}, nil
	}

	stateDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Features.Skills = false
	cfg.Set("file_state_dir", stateDir)
	cfg.Set("llm.provider", "openai")
	cfg.Set("llm.model", "gpt-5.2")

	rt := newRuntimeWithStubIntegrationClient(cfg, chat)
	result, err := rt.RunTaskWithOptions(context.Background(), "ping", RunTaskOptions{
		Agent: agent.RunOptions{
			Meta: map[string]any{
				"caller": "test",
			},
		},
		TaskID:      "task_explicit",
		TopicID:     "topic_explicit",
		TraceID:     "trace_explicit",
		PersistTask: true,
	})
	if err != nil {
		t.Fatalf("RunTaskWithOptions() error = %v", err)
	}
	if result.TaskID != "task_explicit" || result.RunID != "task_explicit" || result.TopicID != "topic_explicit" || result.TraceID != "trace_explicit" {
		t.Fatalf("result ids = %+v, want explicit ids", result)
	}

	meta := injectedMetaFromRequest(t, captured)
	for key, want := range map[string]string{
		"run_id":   "task_explicit",
		"task_id":  "task_explicit",
		"topic_id": "topic_explicit",
		"trace_id": "trace_explicit",
		"caller":   "test",
	} {
		if got, _ := meta[key].(string); got != want {
			t.Fatalf("meta[%s] = %q, want %q", key, got, want)
		}
	}

	events := replayIntegrationTaskEvents(t, filepath.Join(stateDir, "journal"))
	if got, want := len(events), 3; got != want {
		t.Fatalf("len(task events) = %d, want %d", got, want)
	}
	if got := events[0].Event.Type; got != "task_upsert" {
		t.Fatalf("first event type = %q, want task_upsert", got)
	}
	if got := events[1].Event.Type; got != "task_update" {
		t.Fatalf("second event type = %q, want task_update", got)
	}
	if got := events[2].Event.Type; got != "task_update" {
		t.Fatalf("third event type = %q, want task_update", got)
	}
	last := decodeTaskJournalPayload(t, events[2].Event.Payload)
	if last.Target != "integration" {
		t.Fatalf("last.Target = %q, want integration", last.Target)
	}
	if last.Task == nil || last.Task.ID != "task_explicit" || last.Task.Status != "done" || last.Task.TopicID != "topic_explicit" {
		t.Fatalf("last.Task = %+v, want done explicit task", last.Task)
	}
	if last.Task.Task != "ping" || last.Task.Model != "gpt-5.2" {
		t.Fatalf("last.Task task/model = %q/%q, want ping/gpt-5.2", last.Task.Task, last.Task.Model)
	}
	if events[2].Event.Trace.TraceID != "trace_explicit" || events[2].Event.Trace.TaskID != "task_explicit" || events[2].Event.Trace.TopicID != "topic_explicit" {
		t.Fatalf("last trace = %+v, want explicit trace/task/topic", events[2].Event.Trace)
	}
	projectionPath := filepath.Join(stateDir, "tasks", "integration", "projection.json")
	if _, err := os.Stat(projectionPath); !os.IsNotExist(err) {
		t.Fatalf("projection stat error = %v, want projection to be absent", err)
	}
	store, err := daemonruntime.NewFileTaskStore(daemonruntime.FileTaskStoreOptions{
		RootDir:    filepath.Join(stateDir, "tasks", "integration"),
		Target:     "integration",
		Persist:    true,
		JournalDir: filepath.Join(stateDir, "journal"),
	})
	if err != nil {
		t.Fatalf("NewFileTaskStore() error = %v", err)
	}
	replayed, ok := store.Get("task_explicit")
	if !ok || replayed == nil {
		t.Fatalf("replayed task_explicit missing")
	}
	if replayed.Status != daemonruntime.TaskDone || replayed.Task != "ping" || replayed.Model != "gpt-5.2" || replayed.TopicID != "topic_explicit" {
		t.Fatalf("replayed task = %+v, want complete done task", replayed)
	}
}

func TestRunTaskWithOptionsPersistsFailedTaskOnRunError(t *testing.T) {
	chat := func(ctx context.Context, req llm.Request) (llm.Result, error) {
		return llm.Result{}, errors.New("model failed")
	}

	stateDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Features.Skills = false
	cfg.Set("file_state_dir", stateDir)
	cfg.Set("llm.provider", "openai")
	cfg.Set("llm.model", "gpt-5.2")

	rt := newRuntimeWithStubIntegrationClient(cfg, chat)
	_, err := rt.RunTaskWithOptions(context.Background(), "ping", RunTaskOptions{
		TaskID:      "task_failed",
		PersistTask: true,
	})
	if err == nil {
		t.Fatal("RunTaskWithOptions() error = nil, want model error")
	}

	events := replayIntegrationTaskEvents(t, filepath.Join(stateDir, "journal"))
	if got, want := len(events), 3; got != want {
		t.Fatalf("len(task events) = %d, want %d", got, want)
	}
	last := decodeTaskJournalPayload(t, events[2].Event.Payload)
	if last.Task == nil || last.Task.ID != "task_failed" || last.Task.Status != "failed" {
		t.Fatalf("last.Task = %+v, want failed task", last.Task)
	}
	if last.Task.Task != "ping" || last.Task.Model != "gpt-5.2" {
		t.Fatalf("last.Task task/model = %q/%q, want ping/gpt-5.2", last.Task.Task, last.Task.Model)
	}
}

func TestRunTaskWithOptionsUsesPerTaskProfileWithoutChangingRuntimeSelection(t *testing.T) {
	buildClient := func(cfg llmconfig.ClientConfig, _ llmutil.RuntimeValues) (llm.Client, error) {
		model := cfg.Model
		return &stubIntegrationLLMClient{
			chatFn: func(context.Context, llm.Request) (llm.Result, error) {
				return llm.Result{Text: `{"type":"final","output":"` + model + `"}`}, nil
			},
		}, nil
	}

	stateDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Features.Skills = false
	cfg.Set("file_state_dir", stateDir)
	cfg.Set("llm.provider", "openai")
	cfg.Set("llm.model", "gpt-5.2")
	cfg.Set("llm.profiles", map[string]any{
		"cheap": map[string]any{
			"model": "gpt-4.1-mini",
		},
	})

	rt := newRuntime(cfg, runtimeBuildDependencies{buildClient: buildClient})
	profileResult, err := rt.RunTaskWithOptions(context.Background(), "ping", RunTaskOptions{
		LLMProfile:  "cheap",
		TaskID:      "profile_task",
		PersistTask: true,
	})
	if err != nil {
		t.Fatalf("RunTaskWithOptions(profile) error = %v", err)
	}
	if profileResult.Final == nil || profileResult.Final.Output != "gpt-4.1-mini" {
		t.Fatalf("profile result = %#v, want gpt-4.1-mini", profileResult.Final)
	}
	events := replayIntegrationTaskEvents(t, filepath.Join(stateDir, "journal"))
	if len(events) != 3 {
		t.Fatalf("len(task events) = %d, want 3", len(events))
	}
	profileTask := decodeTaskJournalPayload(t, events[len(events)-1].Event.Payload).Task
	if profileTask == nil || profileTask.LLMProfile != "cheap" || profileTask.Model != "gpt-4.1-mini" {
		t.Fatalf("persisted profile task = %#v, want cheap/gpt-4.1-mini", profileTask)
	}

	defaultResult, err := rt.RunTaskWithOptions(context.Background(), "ping", RunTaskOptions{})
	if err != nil {
		t.Fatalf("RunTaskWithOptions(default) error = %v", err)
	}
	if defaultResult.Final == nil || defaultResult.Final.Output != "gpt-5.2" {
		t.Fatalf("default result = %#v, want gpt-5.2", defaultResult.Final)
	}

	selection, err := rt.GetLLMProfileSelection()
	if err != nil {
		t.Fatalf("GetLLMProfileSelection() error = %v", err)
	}
	if selection.Mode != "auto" || selection.ManualProfile != "" {
		t.Fatalf("selection = %#v, want unchanged auto selection", selection)
	}
}

func newRuntimeWithStubIntegrationClient(cfg Config, chat func(context.Context, llm.Request) (llm.Result, error)) *Runtime {
	return newRuntime(cfg, runtimeBuildDependencies{
		buildClient: func(_ llmconfig.ClientConfig, _ llmutil.RuntimeValues) (llm.Client, error) {
			return &stubIntegrationLLMClient{chatFn: chat}, nil
		},
	})
}

func injectedMetaFromRequest(t *testing.T, req llm.Request) map[string]any {
	t.Helper()
	for _, msg := range req.Messages {
		if msg.Role != "user" || !strings.Contains(msg.Content, "mister_morph_meta") {
			continue
		}
		var envelope struct {
			Meta map[string]any `json:"mister_morph_meta"`
		}
		if err := json.Unmarshal([]byte(msg.Content), &envelope); err != nil {
			t.Fatalf("decode meta message: %v", err)
		}
		return envelope.Meta
	}
	t.Fatal("request is missing mister_morph_meta message")
	return nil
}

func replayIntegrationTaskEvents(t *testing.T, journalDir string) []domainjournal.Record {
	t.Helper()
	var out []domainjournal.Record
	if err := domainjournal.ReplayDir(journalDir, func(rec domainjournal.Record) error {
		if rec.Event.Domain == "task" {
			out = append(out, rec)
		}
		return nil
	}); err != nil {
		t.Fatalf("ReplayDir() error = %v", err)
	}
	return out
}

func decodeTaskJournalPayload(t *testing.T, raw json.RawMessage) taskdomain.JournalPayload {
	t.Helper()
	payload, err := taskdomain.DecodeJournalPayload(raw)
	if err != nil {
		t.Fatalf("decode task journal payload: %v", err)
	}
	return payload
}
