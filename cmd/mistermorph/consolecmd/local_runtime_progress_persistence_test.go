package consolecmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/domainjournal"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/viper"
)

type consoleProgressPersistenceLLMStep struct {
	result llm.Result
	err    error
}

type consoleProgressPersistenceClient struct {
	mu    sync.Mutex
	steps []consoleProgressPersistenceLLMStep
	next  int
}

func (c *consoleProgressPersistenceClient) Chat(_ context.Context, _ llm.Request) (llm.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.next >= len(c.steps) {
		return llm.Result{}, fmt.Errorf("no more scripted responses")
	}
	step := c.steps[c.next]
	c.next++
	return step.result, step.err
}

type consoleProgressPersistenceTool struct {
	name   string
	result string
}

func (t consoleProgressPersistenceTool) Name() string            { return t.name }
func (t consoleProgressPersistenceTool) Description() string     { return "test tool" }
func (t consoleProgressPersistenceTool) ParameterSchema() string { return `{}` }
func (t consoleProgressPersistenceTool) Execute(context.Context, map[string]any) (string, error) {
	return t.result, nil
}

func TestConsoleLocalRuntimeStreamsProgressWithoutDurablePartialUpdates(t *testing.T) {
	tests := []struct {
		name       string
		terminal   daemonruntime.TaskStatus
		withGuard  bool
		llmSteps   []consoleProgressPersistenceLLMStep
		secondTool string
	}{
		{
			name:       "done",
			terminal:   daemonruntime.TaskDone,
			secondTool: "work",
			llmSteps: []consoleProgressPersistenceLLMStep{
				{result: consoleProgressToolCall("plan_create")},
				{result: consoleProgressToolCall("work")},
				{result: llm.Result{Text: `{"type":"final","output":"done"}`}},
			},
		},
		{
			name:       "failed",
			terminal:   daemonruntime.TaskFailed,
			secondTool: "work",
			llmSteps: []consoleProgressPersistenceLLMStep{
				{result: consoleProgressToolCall("plan_create")},
				{result: consoleProgressToolCall("work")},
				{err: errors.New("model failed")},
			},
		},
		{
			name:       "pending",
			terminal:   daemonruntime.TaskPending,
			withGuard:  true,
			secondTool: "bash",
			llmSteps: []consoleProgressPersistenceLLMStep{
				{result: consoleProgressToolCall("plan_create")},
				{result: consoleProgressToolCall("bash")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt, job, journal := newConsoleProgressPersistenceRuntime(t, tt.llmSteps, tt.secondTool, tt.withGuard)
			frames, unsubscribe := rt.streamHub.Subscribe(job.TaskID)
			defer unsubscribe()

			rt.handleTaskJob(context.Background(), job.ConversationKey, job)

			updates := consoleProgressTaskUpdates(t, journal, job.TaskID)
			if len(updates) != 2 {
				t.Fatalf("durable task updates = %d, want 2 (running and one terminal summary)", len(updates))
			}
			if updates[0].Status != daemonruntime.TaskRunning || updates[0].Result != nil {
				t.Fatalf("first durable update = status %q result %#v, want running without progress result", updates[0].Status, updates[0].Result)
			}
			terminal := updates[1]
			if terminal.Status != tt.terminal {
				t.Fatalf("terminal durable status = %q, want %q", terminal.Status, tt.terminal)
			}
			terminalResult, ok := terminal.Result.(map[string]any)
			if !ok || terminalResult["plan"] == nil || terminalResult["activity"] == nil {
				t.Fatalf("terminal durable result = %#v, want one final plan/activity summary", terminal.Result)
			}

			var sawPlan, sawActivity bool
		drainFrames:
			for {
				select {
				case frame := <-frames:
					sawPlan = sawPlan || frame.Plan != nil
					sawActivity = sawActivity || frame.Activity != nil
				default:
					break drainFrames
				}
			}
			if !sawPlan || !sawActivity {
				t.Fatalf("stream frames saw plan=%v activity=%v, want both progress types", sawPlan, sawActivity)
			}
		})
	}
}

func consoleProgressToolCall(name string) llm.Result {
	return llm.Result{ToolCalls: []llm.ToolCall{{
		ID:        "call_" + name,
		Name:      name,
		Arguments: map[string]any{},
	}}}
}

func newConsoleProgressPersistenceRuntime(
	t *testing.T,
	steps []consoleProgressPersistenceLLMStep,
	secondTool string,
	withGuard bool,
) (*consoleLocalRuntime, consoleLocalTaskJob, *domainjournal.Journal) {
	t.Helper()
	root := t.TempDir()
	journal, err := domainjournal.New(domainjournal.JournalOptions{
		Dir:           filepath.Join(root, "journal"),
		SyncEachWrite: true,
	})
	if err != nil {
		t.Fatalf("domainjournal.New() error = %v", err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		RootDir:    filepath.Join(root, "tasks"),
		Persist:    true,
		Journal:    journal,
		JournalDir: filepath.Join(root, "journal"),
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}

	client := &consoleProgressPersistenceClient{steps: steps}
	registry := tools.NewRegistry()
	if err := registry.Register(consoleProgressPersistenceTool{
		name:   "plan_create",
		result: `{"plan":{"steps":[{"step":"do work"},{"step":"finish"}]}}`,
	}); err != nil {
		t.Fatalf("Register(plan_create) error = %v", err)
	}
	if err := registry.Register(consoleProgressPersistenceTool{name: secondTool, result: "worked"}); err != nil {
		t.Fatalf("Register(%s) error = %v", secondTool, err)
	}

	fileCacheDir := filepath.Join(root, "cache")
	fileStateDir := filepath.Join(root, "state")
	previousFileStateDir, hadFileStateDir := viper.Get("file_state_dir"), viper.IsSet("file_state_dir")
	viper.Set("file_state_dir", fileStateDir)
	t.Cleanup(func() {
		if hadFileStateDir {
			viper.Set("file_state_dir", previousFileStateDir)
		} else {
			viper.Set("file_state_dir", nil)
		}
	})
	reader := viper.New()
	reader.Set("file_cache_dir", fileCacheDir)
	reader.Set("file_state_dir", fileStateDir)
	logger := slog.Default()
	route := llmutil.ResolvedRoute{ClientConfig: llmconfig.ClientConfig{
		Provider: "test",
		Model:    "test-model",
	}}
	commonDeps := depsutil.CommonDependencies{
		Logger:     func() (*slog.Logger, error) { return logger, nil },
		LogOptions: func() agent.LogOptions { return agent.LogOptions{} },
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return route, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
			return client, nil
		},
		Registry: func() *tools.Registry { return registry },
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), nil, nil
		},
	}
	if withGuard {
		approvalStore, err := guard.NewFileApprovalStore(
			filepath.Join(root, "guard", "approvals.json"),
			filepath.Join(root, "guard", ".locks"),
		)
		if err != nil {
			t.Fatalf("NewFileApprovalStore() error = %v", err)
		}
		sharedGuard := guard.New(guard.Config{
			Enabled:   true,
			Approvals: guard.ApprovalsConfig{Enabled: true},
		}, nil, approvalStore)
		commonDeps.Guard = func(*slog.Logger) (*guard.Guard, error) { return sharedGuard, nil }
	}
	engineTools := consoleEngineToolsConfigFromReader(reader)
	engineTools.PathRoots = pathroots.New("", fileCacheDir, fileStateDir)
	taskRuntime, err := taskruntime.Bootstrap(commonDeps, taskruntime.BootstrapOptions{
		AgentConfig: agent.Config{
			MaxSteps:        5,
			ParseRetries:    0,
			ToolRepeatLimit: 2,
		},
		EngineToolsConfig: &engineTools,
	})
	if err != nil {
		t.Fatalf("taskruntime.Bootstrap() error = %v", err)
	}
	generation := &consoleLocalRuntimeGeneration{
		reader:     reader,
		logger:     logger,
		commonDeps: commonDeps,
		bundle:     &consoleLocalRuntimeBundle{taskRuntime: taskRuntime},
	}
	generation.acquire()
	job := consoleLocalTaskJob{
		TaskID:          "task_progress_" + strings.ReplaceAll(t.Name(), "/", "_"),
		ConversationKey: "console:topic_progress",
		TopicID:         "topic_progress",
		Task:            "run with progress",
		Timeout:         time.Minute,
		CreatedAt:       time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC),
		Trigger:         daemonruntime.TaskTrigger{Source: "ui"},
		Generation:      generation,
	}
	if err := store.UpsertWithTrigger(daemonruntime.TaskInfo{
		ID:        job.TaskID,
		Status:    daemonruntime.TaskQueued,
		Task:      job.Task,
		Model:     "test-model",
		Timeout:   job.Timeout.String(),
		CreatedAt: job.CreatedAt,
		TopicID:   job.TopicID,
	}, job.Trigger, "Progress topic"); err != nil {
		t.Fatalf("UpsertWithTrigger() error = %v", err)
	}
	rt := &consoleLocalRuntime{
		store:                 store,
		streamHub:             newConsoleStreamHub(),
		generation:            generation,
		consoleExecutionState: newConsoleExecutionState(nil, nil),
	}
	t.Cleanup(rt.Close)
	return rt, job, journal
}

func consoleProgressTaskUpdates(t *testing.T, journal *domainjournal.Journal, taskID string) []daemonruntime.TaskInfo {
	t.Helper()
	var updates []daemonruntime.TaskInfo
	if err := journal.Replay(func(record domainjournal.Record) error {
		if record.Event.Domain != "task" || record.Event.Type != "task_update" {
			return nil
		}
		var payload struct {
			Task *daemonruntime.TaskInfo `json:"task"`
		}
		if err := json.Unmarshal(record.Event.Payload, &payload); err != nil {
			return err
		}
		if payload.Task != nil && payload.Task.ID == taskID {
			updates = append(updates, *payload.Task)
		}
		return nil
	}); err != nil {
		t.Fatalf("journal.Replay() error = %v", err)
	}
	return updates
}
