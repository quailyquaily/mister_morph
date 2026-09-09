package consolecmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/llm"
)

func TestConsoleRegenerateTopicTitle(t *testing.T) {
	for _, mode := range []string{"success", "failed", "manual", "deleted", "empty", "reserved", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			store, _ := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{})
			topic, _ := store.CreateTopic("old name")
			if err := store.SetTopicTitle(topic.ID, "old name"); err != nil {
				t.Fatal(err)
			}
			if mode != "empty" {
				if err := store.Upsert(daemonruntime.TaskInfo{ID: "first", TopicID: topic.ID, Task: "Hello", CreatedAt: time.Unix(1, 0), Status: daemonruntime.TaskDone, Result: map[string]any{"output": "Hello back", "reasoning": "private reasoning"}}); err != nil {
					t.Fatal(err)
				}
				if err := store.Upsert(daemonruntime.TaskInfo{ID: "recent", TopicID: topic.ID, Task: "Discuss baby sleep", CreatedAt: time.Unix(2, 0), Status: daemonruntime.TaskDone, Result: map[string]any{"output": "Sleep routine", "steps": "tool logs"}}); err != nil {
					t.Fatal(err)
				}
			}
			rt := &consoleLocalRuntime{store: store}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			generation := &consoleLocalRuntimeGeneration{commonDeps: depsutil.CommonDependencies{
				ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
					return llmutil.ResolvedRoute{ClientConfig: llmconfig.ClientConfig{Model: "test", RequestTimeout: 90 * time.Second}}, nil
				},
				CreateLLMClient: func(route llmutil.ResolvedRoute) (llm.Client, error) {
					if route.ClientConfig.RequestTimeout != 90*time.Second {
						t.Error("lost configured timeout")
					}
					return topicNamingClientFunc(func(ctx context.Context, req llm.Request) (llm.Result, error) {
						calls++
						if _, ok := ctx.Deadline(); ok {
							t.Error("unexpected fixed naming deadline")
						}
						text := req.Messages[1].Content
						for _, want := range []string{"Hello", "Discuss baby sleep", "Sleep routine"} {
							if !strings.Contains(text, want) {
								t.Errorf("missing %q in %s", want, text)
							}
						}
						for _, bad := range []string{"private reasoning", "tool logs", "old name"} {
							if strings.Contains(text, bad) {
								t.Errorf("included %q", bad)
							}
						}
						if !strings.Contains(req.Messages[0].Content, "recent substantive") {
							t.Error("missing topic drift instruction")
						}
						switch mode {
						case "failed":
							return llm.Result{}, errors.New("offline")
						case "manual":
							if err := store.SetTopicTitle(topic.ID, "mine"); err != nil {
								t.Fatal(err)
							}
						case "deleted":
							if _, err := store.DeleteTopic(topic.ID); err != nil {
								t.Fatal(err)
							}
						case "cancelled":
							cancel()
						}
						return llm.Result{Text: `{"title":"Baby sleep","icon":"baby"}`}, nil
					}), nil
				},
			}}
			id := topic.ID
			if mode == "reserved" {
				id = daemonruntime.ConsoleDefaultTopicID
			}
			got, err := rt.regenerateTopicTitle(ctx, generation, id)
			if mode == "success" {
				if err != nil || got.Title != "Baby sleep" || got.Icon != "baby" {
					t.Fatalf("topic=%+v error=%v", got, err)
				}
			} else {
				if err == nil {
					t.Fatal("expected error")
				}
				stored, _ := store.GetTopic(topic.ID)
				want := "old name"
				if mode == "manual" {
					want = "mine"
				}
				if stored.Title != want {
					t.Fatalf("failure overwrote name: %+v", stored)
				}
			}
			if (mode == "empty" || mode == "reserved") && calls != 0 {
				t.Fatal("called LLM without a conversation")
			}
			if refs := consoleGenerationRefs(generation); refs != 0 {
				t.Fatalf("leaked generation refs: %d", refs)
			}
		})
	}
}

func TestConsoleRegenerateTopicTitleRejectsDuplicate(t *testing.T) {
	store, _ := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{})
	topic, _ := store.CreateTopic("seed")
	if err := store.Upsert(daemonruntime.TaskInfo{ID: "task", TopicID: topic.ID, Task: "Pets"}); err != nil {
		t.Fatal(err)
	}
	rt := &consoleLocalRuntime{store: store}
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	generation := &consoleLocalRuntimeGeneration{commonDeps: depsutil.CommonDependencies{
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return llmutil.ResolvedRoute{ClientConfig: llmconfig.ClientConfig{Model: "test"}}, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
			return topicNamingClientFunc(func(context.Context, llm.Request) (llm.Result, error) {
				close(started)
				<-release
				return llm.Result{Text: `{"title":"Pets","icon":"paw-print"}`}, nil
			}), nil
		},
	}}
	go func() { _, err := rt.regenerateTopicTitle(context.Background(), generation, topic.ID); done <- err }()
	t.Cleanup(func() {
		close(release)
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("generation did not start")
	}
	if _, err := rt.regenerateTopicTitle(context.Background(), generation, topic.ID); !errors.Is(err, daemonruntime.ErrTopicTitleBusy) {
		t.Fatalf("duplicate error = %v", err)
	}
	got, _ := store.GetTopic(topic.ID)
	if got.Title != "seed" {
		t.Fatal("changed title while generating")
	}
}

func TestConsoleTopicTitleInputIsBoundedAndExcludesNonReplies(t *testing.T) {
	tasks := make([]daemonruntime.TaskInfo, 7)
	for i := range tasks {
		tasks[i] = daemonruntime.TaskInfo{Task: strings.Repeat("猫", 2000), Status: daemonruntime.TaskDone, Result: map[string]any{"output": strings.Repeat("犬", 2000)}}
	}
	text := consoleTopicTitleInput(tasks)
	if utf8.RuneCountInString(text) > 8000 {
		t.Fatalf("input length = %d", utf8.RuneCountInString(text))
	}
	tasks = []daemonruntime.TaskInfo{
		{Task: "Question", Status: daemonruntime.TaskFailed, Error: "error", Result: map[string]any{"output": "partial"}},
		{Task: "Follow up", Status: daemonruntime.TaskDone, SteerTargetTaskID: "task", Result: map[string]any{"output": "steering ack"}},
	}
	text = consoleTopicTitleInput(tasks)
	if strings.Contains(text, "partial") || strings.Contains(text, "steering ack") || strings.Contains(text, "error") {
		t.Fatal(text)
	}
}
