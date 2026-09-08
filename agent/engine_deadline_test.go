package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type deadlineTestClient func(context.Context, llm.Request) (llm.Result, error)

func (f deadlineTestClient) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	return f(ctx, req)
}

type deadlineIgnoringTool struct{ slowTool }

func (t *deadlineIgnoringTool) Execute(context.Context, map[string]any) (string, error) {
	time.Sleep(time.Hour)
	return "late result", nil
}

func TestConcurrentToolCompletionRecordedBeforeBatchFinishes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reg := tools.NewRegistry()
		reg.Register(&slowTool{name: "slow", delay: time.Second, result: "slow result", parallel: true})
		reg.Register(&slowTool{name: "fast", delay: 10 * time.Millisecond, result: "fast result", parallel: true})
		start := time.Now()
		var completedAt time.Duration
		var recorded bool
		engine := New(newMockClient(multiToolCallResponse("slow", "fast"), finalResponse("ok")), reg, baseCfg(), DefaultPromptSpec(),
			WithOnToolCallDone(func(ctx *Context, tc ToolCall, observation string, err error) {
				if tc.Name == "fast" {
					completedAt = time.Since(start)
					for _, step := range ctx.Steps {
						if step.Action == "fast" && step.Observation == "fast result" && step.Error == nil {
							recorded = true
						}
					}
				}
			}))
		_, _, err := engine.Run(context.Background(), "review both", RunOptions{})
		if err != nil || !recorded || completedAt != 10*time.Millisecond {
			t.Fatalf("err=%v recorded=%v fast completed at %s", err, recorded, completedAt)
		}
	})
}

func TestDeadlineForceConclusionPreservesConcurrentResults(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reg := tools.NewRegistry()
		reg.Register(&deadlineIgnoringTool{slowTool: slowTool{name: "stuck", parallel: true}})
		reg.Register(&slowTool{name: "slow", delay: time.Hour, parallel: true})
		for _, name := range []string{"first", "second", "third"} {
			reg.Register(&slowTool{name: name, delay: 10 * time.Millisecond, result: name + " result", parallel: true})
		}
		calls := 0
		client := deadlineTestClient(func(ctx context.Context, req llm.Request) (llm.Result, error) {
			calls++
			if calls == 1 {
				return multiToolCallResponse("stuck", "slow", "first", "second", "third"), nil
			}
			if ctx.Err() != nil || len(req.Tools) != 0 {
				return llm.Result{}, fmt.Errorf("invalid conclusion context/tools: %v/%d", ctx.Err(), len(req.Tools))
			}
			if _, ok := ctx.Deadline(); ok {
				return llm.Result{}, errors.New("engine must leave request timeout to the client")
			}
			var results []llm.Message
			for _, msg := range req.Messages {
				if msg.Role == "tool" {
					results = append(results, msg)
				}
			}
			if len(results) != 5 {
				return llm.Result{}, fmt.Errorf("got %d tool results", len(results))
			}
			for i, msg := range results {
				if msg.ToolCallID != fmt.Sprintf("call_%d", i) {
					return llm.Result{}, errors.New("tool messages lost original order")
				}
			}
			for i, name := range []string{"first", "second", "third"} {
				if results[i+2].Content != name+" result" {
					return llm.Result{}, errors.New("completed output missing")
				}
			}
			if !strings.Contains(req.Messages[len(req.Messages)-1].Content, "deadline") {
				return llm.Result{}, errors.New("wrong conclusion reason")
			}
			return finalResponse("Three reviews completed; two did not return before the deadline."), nil
		})
		engine := New(client, reg, baseCfg(), DefaultPromptSpec())
		sink := &contextAwareEventSink{}
		ctx, cancel := context.WithTimeout(WithEventSinkContext(context.Background(), sink), 100*time.Millisecond)
		defer cancel()
		start := time.Now()
		final, state, err := engine.Run(ctx, "review five", RunOptions{})
		elapsed := time.Since(start)
		// Let an uncooperative worker finish, then check it cannot overwrite results.
		time.Sleep(time.Hour)
		if err != nil || final == nil || final.Output != "Three reviews completed; two did not return before the deadline." || calls != 2 || elapsed > time.Minute {
			t.Fatalf("final=%+v err=%v calls=%d elapsed=%s", final, err, calls, elapsed)
		}
		if len(state.Steps) != 5 {
			t.Fatalf("steps=%+v", state.Steps)
		}
		completed, canceled := 0, 0
		for _, step := range state.Steps {
			if step.Error == nil {
				completed++
			} else if errors.Is(step.Error, context.DeadlineExceeded) {
				canceled++
			}
		}
		if completed != 3 || canceled != 2 {
			t.Fatalf("completed=%d canceled=%d", completed, canceled)
		}
		doneEvents := 0
		for _, event := range sink.all() {
			if event.Kind == EventKindToolDone {
				doneEvents++
			}
		}
		if doneEvents != 5 || !eventsContainKind(sink.all(), EventKindTurnDone) {
			t.Fatalf("events=%+v", sink.all())
		}
	})
}

func TestDeadlineConclusionOnlyForParentTaskExpiry(t *testing.T) {
	for _, mode := range []string{"deadline", "request timeout", "user cancel", "subtask deadline", "summary fails", "summary times out"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if mode == "subtask deadline" {
					ctx = WithSubtaskDepth(ctx, 1)
				}
				calls := 0
				client := deadlineTestClient(func(callCtx context.Context, req llm.Request) (llm.Result, error) {
					calls++
					if calls == 1 {
						if mode == "request timeout" {
							return llm.Result{}, context.DeadlineExceeded
						}
						if mode == "user cancel" {
							cancel()
						}
						<-callCtx.Done()
						return llm.Result{}, callCtx.Err()
					}
					if callCtx.Err() != nil {
						return llm.Result{}, callCtx.Err()
					}
					if mode == "summary fails" {
						return llm.Result{}, errors.New("upstream unavailable")
					}
					if mode == "summary times out" {
						var cancel context.CancelFunc
						callCtx, cancel = context.WithTimeout(callCtx, 90*time.Second)
						defer cancel()
						<-callCtx.Done()
						return llm.Result{}, callCtx.Err()
					}
					return finalResponse("partial summary"), nil
				})
				engine := New(client, tools.NewRegistry(), baseCfg(), DefaultPromptSpec())
				start := time.Now()
				final, _, err := engine.Run(ctx, "work", RunOptions{})
				if mode == "request timeout" || mode == "user cancel" || mode == "subtask deadline" {
					if err == nil || calls != 1 {
						t.Fatalf("err=%v calls=%d", err, calls)
					}
					return
				}
				wantElapsed := time.Second
				if mode == "summary times out" {
					wantElapsed += 90 * time.Second
				}
				if err != nil || final == nil || calls != 2 || time.Since(start) != wantElapsed {
					t.Fatalf("final=%+v err=%v calls=%d elapsed=%s", final, err, calls, time.Since(start))
				}
				if strings.HasPrefix(mode, "summary ") && !strings.Contains(fmt.Sprint(final.Output), "deadline") {
					t.Fatalf("missing deadline in fallback: %+v", final)
				}
			})
		})
	}
}

func TestCanceledToolBatchDoesNotConsumeSuccessfulRepeatLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reg := tools.NewRegistry()
		reg.Register(&deadlineIgnoringTool{slowTool: slowTool{name: "stuck"}})
		cfg := baseCfg()
		cfg.ToolRepeatLimit = 1
		cfg.ToolCallTimeout = 10 * time.Millisecond
		engine := New(newMockClient(multiToolCallResponse("stuck"), multiToolCallResponse("stuck"), finalResponse("partial")), reg, cfg, DefaultPromptSpec())
		_, state, err := engine.Run(context.Background(), "try twice", RunOptions{})
		if err != nil || len(state.Steps) != 2 {
			t.Fatalf("state=%+v err=%v", state, err)
		}
		for _, step := range state.Steps {
			if !errors.Is(step.Error, context.DeadlineExceeded) {
				t.Errorf("step error=%v, want deadline, not repeat limit", step.Error)
			}
		}
		time.Sleep(time.Hour)
	})
}

func TestDeadlineConclusionGuardsResultsAndPreservesPartialPlan(t *testing.T) {
	for _, mode := range []string{"summary", "fallback", "summary timeout"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				reg := tools.NewRegistry()
				reg.Register(&slowTool{name: "review", delay: time.Millisecond, result: "review result; password=" + finalSecret, parallel: true})
				reg.Register(&slowTool{name: "pending", delay: time.Hour, parallel: true})
				calls := 0
				client := deadlineTestClient(func(ctx context.Context, req llm.Request) (llm.Result, error) {
					calls++
					switch calls {
					case 1:
						return llm.Result{Text: `{"type":"plan","steps":[{"step":"first review","status":"in_progress"},{"step":"second review","status":"pending"}]}`}, nil
					case 2:
						return multiToolCallResponse("review", "pending"), nil
					}
					for _, msg := range req.Messages {
						if strings.Contains(msg.Content, finalSecret) {
							t.Error("summary request contains unredacted tool output")
						}
					}
					if mode == "fallback" {
						return llm.Result{}, errors.New("summary unavailable")
					}
					if mode == "summary timeout" {
						var cancel context.CancelFunc
						ctx, cancel = context.WithTimeout(ctx, 90*time.Second)
						defer cancel()
						<-ctx.Done()
						return llm.Result{}, ctx.Err()
					}
					return llm.Result{Text: fmt.Sprintf(`{"type":"final","output":"partial; password=%s","plan":{"steps":[{"step":"first review","status":"completed"},{"step":"second review","status":"completed"}]}}`, finalSecret)}, nil
				})
				audit := &captureActionAuditSink{}
				engine := New(client, reg, baseCfg(), DefaultPromptSpec(), WithGuard(approvalGuard(nil, audit)))
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()
				final, state, err := engine.Run(ctx, "review", RunOptions{})
				if err != nil || final == nil || calls != 3 {
					t.Fatalf("final=%+v calls=%d err=%v", final, calls, err)
				}
				assertFinalHasNoSecret(t, final, state)
				if !audit.hasAction(guard.ActionOutputPublish) {
					t.Fatal("missing final output guard")
				}
				if final.Plan == nil || len(final.Plan.Steps) != 2 || final.Plan.Steps[1].Status == PlanStatusCompleted {
					t.Fatalf("unfinished plan was marked complete: %+v", final.Plan)
				}
				if mode != "summary" && !strings.Contains(fmt.Sprint(final.Output), "review result") {
					t.Fatalf("fallback lost completed result: %+v", final)
				}
			})
		})
	}
}

func TestDeadlineConclusionPreservesRawPartialPlan(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		<-ctx.Done()
		state := &engineLoopState{agentCtx: NewContext("review", 3)}
		state.agentCtx.Plan = &Plan{Steps: []PlanStep{{Step: "review", Status: PlanStatusInProgress}}}
		engine := New(newMockClient(llm.Result{Text: `{"type":"final","output":"partial","evidence":"available","plan":{"steps":[{"step":"review","status":"completed"}]}}`}), tools.NewRegistry(), baseCfg(), DefaultPromptSpec())
		_, agentCtx, err := engine.forceConclusion(ctx, state, forceConclusionTaskDeadline, nil)
		if err != nil {
			t.Fatal(err)
		}
		var raw struct {
			Plan     *Plan  `json:"plan"`
			Evidence string `json:"evidence"`
		}
		if err := json.Unmarshal(agentCtx.RawFinalAnswer, &raw); err != nil {
			t.Fatal(err)
		}
		if raw.Plan == nil || raw.Plan.Steps[0].Status != PlanStatusInProgress || raw.Evidence != "available" {
			t.Fatalf("raw final did not preserve partial plan/evidence: %s", agentCtx.RawFinalAnswer)
		}
	})
}

func TestForceConclusionHonorsExplicitCancellation(t *testing.T) {
	for _, duringRequest := range []bool{false, true} {
		t.Run(fmt.Sprint(duringRequest), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			client := deadlineTestClient(func(context.Context, llm.Request) (llm.Result, error) {
				calls++
				cancel()
				return llm.Result{}, context.Canceled
			})
			if !duringRequest {
				cancel()
			}
			engine := New(client, tools.NewRegistry(), baseCfg(), DefaultPromptSpec())
			final, _, err := engine.forceConclusion(ctx, &engineLoopState{agentCtx: NewContext("review", 1)}, forceConclusionMaxSteps, nil)
			if !errors.Is(err, context.Canceled) || final != nil || (!duringRequest && calls != 0) {
				t.Fatalf("final=%+v err=%v calls=%d", final, err, calls)
			}
		})
	}
}

func TestDeadlineConclusionUsesClientRequestTimeout(t *testing.T) {
	for _, requestTimeout := range []time.Duration{30 * time.Second, 120 * time.Second} {
		t.Run(requestTimeout.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client := deadlineTestClient(func(ctx context.Context, req llm.Request) (llm.Result, error) {
					// Providers apply the selected profile's timeout to each request.
					ctx, cancel := context.WithTimeout(ctx, requestTimeout)
					defer cancel()
					select {
					case <-time.After(75 * time.Second):
						return finalResponse("partial review summary"), nil
					case <-ctx.Done():
						return llm.Result{}, ctx.Err()
					}
				})
				engine := New(client, tools.NewRegistry(), baseCfg(), DefaultPromptSpec())
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				<-ctx.Done()
				start := time.Now()
				final, _, err := engine.forceConclusion(ctx, &engineLoopState{agentCtx: NewContext("review", 1)}, forceConclusionTaskDeadline, nil)
				if err != nil || final == nil || time.Since(start) != min(requestTimeout, 75*time.Second) {
					t.Fatalf("final=%+v err=%v elapsed=%s", final, err, time.Since(start))
				}
				if requestTimeout > 75*time.Second && final.Output != "partial review summary" {
					t.Fatalf("summary was cut short: %+v", final)
				}
			})
		})
	}
}
