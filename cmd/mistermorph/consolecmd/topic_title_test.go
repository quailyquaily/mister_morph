package consolecmd

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/spf13/viper"
)

type topicNamingClientFunc func(context.Context, llm.Request) (llm.Result, error)

func (f topicNamingClientFunc) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	return f(ctx, req)
}

func TestConsoleTopicNamingStartsBeforeTaskFinishes(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rt := &consoleLocalRuntime{store: store, consoleExecutionState: newConsoleExecutionState(nil, nil)}
	namingStarted, releaseNaming := make(chan struct{}), make(chan struct{})
	workerStarted, releaseWorker := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(releaseWorker); rt.consoleExecutionState.close() })
	reader := viper.New()
	generation := &consoleLocalRuntimeGeneration{reader: reader}
	generation.commonDeps = depsutil.CommonDependencies{
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return llmutil.ResolvedRoute{ClientConfig: llmconfig.ClientConfig{Model: "test-model", RequestTimeout: 90 * time.Second}}, nil
		},
		CreateLLMClient: func(route llmutil.ResolvedRoute) (llm.Client, error) {
			if route.ClientConfig.RequestTimeout != 90*time.Second {
				t.Error("lost configured request timeout")
			}
			return topicNamingClientFunc(func(ctx context.Context, req llm.Request) (llm.Result, error) {
				if _, ok := ctx.Deadline(); ok {
					t.Error("naming must not impose its old fixed total timeout")
				}
				if req.Scene != "console.topic_title" || !strings.Contains(req.Messages[1].Content, "debug memory leak") || strings.Contains(req.Messages[1].Content, "Final output") {
					t.Errorf("request = %+v", req)
				}
				close(namingStarted)
				select {
				case <-releaseNaming:
				case <-ctx.Done():
					return llm.Result{}, ctx.Err()
				}
				return llm.Result{Text: `{"title":"Memory leak","icon":"code"}`}, nil
			}), nil
		},
	}
	rt.runner = runtimecore.NewConversationRunner[string, consoleLocalTaskJob](rt.workersCtx, make(chan struct{}, 1), 1,
		func(_ context.Context, _ string, job consoleLocalTaskJob) {
			close(workerStarted)
			<-releaseWorker
			job.Generation.release()
		},
		runtimecore.ConversationRunnerOptions[string, consoleLocalTaskJob]{})
	generation.acquire()
	job, _, err := rt.acceptTask(generation, "debug memory leak", "", "", time.Minute, "", "", "", nil, daemonruntime.TaskTrigger{Source: "ui"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.addPendingJob(job); err != nil {
		t.Fatal(err)
	}
	if err := rt.handleConsoleBusInbound(context.Background(), busruntime.BusMessage{Channel: busruntime.ChannelConsole, Direction: busruntime.DirectionInbound, CorrelationID: job.TaskID}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-namingStarted:
	case <-time.After(time.Second):
		t.Fatal("naming did not start while task was pending")
	}
	select {
	case <-workerStarted:
	case <-time.After(time.Second):
		t.Fatal("naming blocked task execution")
	}
	close(releaseNaming)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		topic, _ := store.GetTopic(job.TopicID)
		if topic.LLMTitleGeneratedAt != nil {
			if topic.Title != "Memory leak" || topic.Icon != "code" {
				t.Fatalf("topic = %+v", topic)
			}
			_, _, err := rt.acceptTask(generation, "another question", "", "", time.Minute, job.TopicID, "", "", nil, daemonruntime.TaskTrigger{})
			if err != nil {
				t.Fatal(err)
			}
			topic, _ = store.GetTopic(job.TopicID)
			if topic.Title != "Memory leak" {
				t.Fatal("follow-up overwrote generated title")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("title was not stored before task finished")
}

func TestConsoleTopicNamingResponse(t *testing.T) {
	for _, tt := range []struct {
		name, response, wantTitle, wantIcon string
		fail                                bool
	}{
		{"valid", `{"title":"Fix bug","icon":"code"}`, "Fix bug", "code", false},
		{"greeting", `{"title":"你好","icon":"hand-waving"}`, "你好", "hand-waving", false},
		{"pets", `{"title":"养猫指南","icon":"paw-print"}`, "养猫指南", "paw-print", false},
		{"baby", `{"title":"婴儿睡眠","icon":"baby"}`, "婴儿睡眠", "baby", false},
		{"unknown icon", `{"title":"Research","icon":"<script>"}`, "Research", "chat", false},
		{"plain text", "A title", "", "", true},
		{"empty title", `{"title":"","icon":"code"}`, "", "", true},
		{"request failure", "", "", "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			generation := &consoleLocalRuntimeGeneration{commonDeps: depsutil.CommonDependencies{
				ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
					return llmutil.ResolvedRoute{ClientConfig: llmconfig.ClientConfig{Model: "test"}}, nil
				},
				CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
					return topicNamingClientFunc(func(_ context.Context, req llm.Request) (llm.Result, error) {
						if !strings.Contains(req.Messages[0].Content, "greeting") {
							t.Error("missing no-topic instruction")
						}
						if !strings.Contains(req.Messages[0].Content, "use hand-waving") || !strings.Contains(req.Messages[0].Content, "Pets") || !strings.Contains(req.Messages[0].Content, "Babies") {
							t.Error("missing greeting icon instruction or theme descriptions")
						}
						if tt.name == "request failure" {
							return llm.Result{}, errors.New("offline")
						}
						return llm.Result{Text: tt.response}, nil
					}), nil
				},
			}}
			got, err := (&consoleLocalRuntime{}).generateTopicTitle(context.Background(), generation, "hello")
			if (err != nil) != tt.fail {
				t.Fatalf("error = %v", err)
			}
			if !tt.fail && (got.Title != tt.wantTitle || got.Icon != tt.wantIcon) {
				t.Fatalf("got = %+v", got)
			}
		})
	}
}

func TestConsoleTopicNamingKeepsExistingTitleOnFailureOrManualEdit(t *testing.T) {
	for _, mode := range []string{"failed", "edited-during-request", "edited-before-request", "once", "not-new"} {
		t.Run(mode, func(t *testing.T) {
			store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{})
			if err != nil {
				t.Fatal(err)
			}
			topic, err := store.CreateTopic("seed")
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			generation := &consoleLocalRuntimeGeneration{commonDeps: depsutil.CommonDependencies{
				ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
					return llmutil.ResolvedRoute{ClientConfig: llmconfig.ClientConfig{Model: "test"}}, nil
				},
				CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
					return topicNamingClientFunc(func(context.Context, llm.Request) (llm.Result, error) {
						calls.Add(1)
						if mode == "failed" {
							return llm.Result{}, errors.New("offline")
						}
						if mode == "edited-during-request" {
							if err := store.SetTopicTitle(topic.ID, "my title"); err != nil {
								return llm.Result{}, err
							}
						}
						return llm.Result{Text: `{"title":"generated","icon":"code"}`}, nil
					}), nil
				},
			}}
			if mode == "edited-before-request" {
				if err := store.SetTopicTitle(topic.ID, "my title"); err != nil {
					t.Fatal(err)
				}
			}
			rt := &consoleLocalRuntime{store: store}
			job := consoleLocalTaskJob{TopicID: topic.ID, Task: "seed", AutoRenameTopic: mode != "not-new", Generation: generation}
			rt.maybeRefreshTopicTitle(job)
			deadline := time.Now().Add(time.Second)
			for consoleGenerationRefs(generation) > 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if consoleGenerationRefs(generation) > 0 {
				t.Fatal("naming did not release its generation")
			}
			if mode == "once" {
				rt.maybeRefreshTopicTitle(job)
			}
			got, _ := store.GetTopic(topic.ID)
			wantTitle, wantCalls := "seed", int32(1)
			if strings.HasPrefix(mode, "edited-") {
				wantTitle = "my title"
			}
			if mode == "once" {
				wantTitle = "generated"
			}
			if mode == "not-new" || mode == "edited-before-request" {
				wantCalls = 0
			}
			if got.Title != wantTitle || calls.Load() != wantCalls {
				t.Fatalf("topic=%+v calls=%d", got, calls.Load())
			}
		})
	}
}
