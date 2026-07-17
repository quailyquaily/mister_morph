package awareness

import (
	"context"
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
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

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

	topics := store.ListTopics()
	found := false
	for _, topic := range topics {
		if topic.ID == daemonruntime.ConsoleAwarenessTopicID {
			found = true
			if topic.Title != daemonruntime.ConsoleAwarenessTopicTitle {
				t.Fatalf("awareness topic title = %q, want %q", topic.Title, daemonruntime.ConsoleAwarenessTopicTitle)
			}
		}
	}
	if !found {
		t.Fatalf("awareness topic missing from topics: %#v", topics)
	}
}
