package llmutil

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/llm"
)

func TestRouteClientRetriesTimeoutWithoutFallback(t *testing.T) {
	for _, weighted := range []bool{false, true} {
		t.Run(fmt.Sprintf("weighted=%v", weighted), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cfg := llmconfig.ClientConfig{Model: "main"}
				route := ResolvedRoute{ClientConfig: cfg}
				if weighted {
					route.Candidates = []ResolvedCandidate{{ClientConfig: cfg, Weight: 1}}
				}
				var calls []time.Time
				client, err := BuildRouteClient(route, nil, func(llmconfig.ClientConfig, RuntimeValues) (llm.Client, error) {
					return &testLLMClient{chatFn: func(ctx context.Context, req llm.Request) (llm.Result, error) {
						if ctx.Err() != nil || req.Model != "main" || req.Messages[0].Content != "task" {
							t.Fatalf("retry changed request or reused expired context: %+v, %v", req, ctx.Err())
						}
						calls = append(calls, time.Now())
						if len(calls) < 6 {
							return llm.Result{}, context.DeadlineExceeded
						}
						return llm.Result{Text: "ok"}, nil
					}}, nil
				}, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				result, err := client.Chat(context.Background(), llm.Request{Model: "main", Messages: []llm.Message{{Role: "user", Content: "task"}}})
				if err != nil || result.Text != "ok" || len(calls) != 6 {
					t.Fatalf("result=%+v err=%v calls=%d", result, err, len(calls))
				}
				for i := 1; i < len(calls); i++ {
					limit := time.Second << (i - 1)
					if delay := calls[i].Sub(calls[i-1]); delay < limit/2 || delay > limit {
						t.Fatalf("retry %d delay=%s, want [%s,%s]", i, delay, limit/2, limit)
					}
				}
			})
		})
	}
}

func TestFallbackClientRetriesEachModelBeforeSwitching(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var models []string
		makeClient := func(model string) llm.Client {
			return &testLLMClient{chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
				if req.Model != model {
					t.Fatalf("model=%q, want %q", req.Model, model)
				}
				models = append(models, model)
				return llm.Result{}, context.DeadlineExceeded
			}}
		}
		client := NewFallbackClient(FallbackClientOptions{
			Primary:   makeClient("main"),
			Fallbacks: []FallbackCandidate{{Model: "backup", Client: makeClient("backup")}},
		})
		_, err := client.Chat(context.Background(), llm.Request{Model: "main"})
		want := []string{"main", "main", "main", "main", "main", "main", "backup", "backup", "backup", "backup", "backup", "backup"}
		if !errors.Is(err, context.DeadlineExceeded) || !reflect.DeepEqual(models, want) {
			t.Fatalf("models=%v err=%v", models, err)
		}
	})
}

func TestFallbackClientTimeoutRetryClassification(t *testing.T) {
	for _, tt := range []struct {
		name  string
		err   error
		calls int
	}{
		{"wrapped deadline", fmt.Errorf("request: %w", context.DeadlineExceeded), 6},
		{"network timeout", &net.DNSError{IsTimeout: true}, 6},
		{"timeout message", errors.New("upstream request timed out"), 6},
		{"408", errors.New("status 408: request timeout"), 6},
		{"504", errors.New("status 504: gateway timeout"), 6},
		{"subscription HTTP 408", errors.New("codex subscription inference failed with HTTP 408"), 6},
		{"subscription HTTP 504", errors.New("codex subscription inference failed with HTTP 504"), 6},
		{"503", errors.New("status 503: unavailable"), 1},
		{"400", errors.New("status 400: bad request"), 1},
		{"canceled", context.Canceled, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				client := NewFallbackClient(FallbackClientOptions{Primary: &testLLMClient{chatFn: func(context.Context, llm.Request) (llm.Result, error) {
					calls++
					return llm.Result{}, tt.err
				}}})
				_, err := client.Chat(context.Background(), llm.Request{})
				if !errors.Is(err, tt.err) || calls != tt.calls {
					t.Fatalf("err=%v calls=%d, want %d", err, calls, tt.calls)
				}
			})
		})
	}
}

func TestFallbackClientStopsWhenTaskEnds(t *testing.T) {
	for _, phase := range []string{"before request", "during request", "during backoff"} {
		t.Run(phase, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if phase == "before request" {
					cancel()
				}
				calls, fallbackCalls := 0, 0
				client := NewFallbackClient(FallbackClientOptions{
					Primary: &testLLMClient{chatFn: func(context.Context, llm.Request) (llm.Result, error) {
						calls++
						if phase == "during request" {
							cancel()
						} else if phase == "during backoff" {
							timer := time.AfterFunc(100*time.Millisecond, cancel)
							t.Cleanup(func() { timer.Stop() })
						}
						return llm.Result{}, context.DeadlineExceeded
					}},
					Fallbacks: []FallbackCandidate{{Client: &testLLMClient{chatFn: func(context.Context, llm.Request) (llm.Result, error) {
						fallbackCalls++
						return llm.Result{}, nil
					}}}},
				})
				start := time.Now()
				_, err := client.Chat(ctx, llm.Request{})
				wantCalls := 1
				if phase == "before request" {
					wantCalls = 0
				}
				if !errors.Is(err, context.Canceled) || calls != wantCalls || fallbackCalls != 0 || time.Since(start) > 100*time.Millisecond {
					t.Fatalf("err=%v calls=%d fallback=%d elapsed=%v", err, calls, fallbackCalls, time.Since(start))
				}
			})
		})
	}
}

func TestFallbackClientTimeoutRetrySeparatesStreams(t *testing.T) {
	for _, alreadyDone := range []bool{false, true} {
		t.Run(fmt.Sprintf("alreadyDone=%v", alreadyDone), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				var buffer strings.Builder
				var streams []string
				client := NewFallbackClient(FallbackClientOptions{Primary: &testLLMClient{chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
					calls++
					text := "partial"
					if calls == 2 {
						text = "complete"
					}
					if err := req.OnStream(llm.StreamEvent{Delta: text}); err != nil {
						return llm.Result{}, err
					}
					if buffer.String() != text {
						t.Fatalf("stream was buffered or mixed with previous attempt: %q", buffer.String())
					}
					if calls == 2 || alreadyDone {
						if err := req.OnStream(llm.StreamEvent{Done: true}); err != nil {
							return llm.Result{}, err
						}
					}
					if calls == 1 {
						return llm.Result{}, context.DeadlineExceeded
					}
					return llm.Result{Text: "complete"}, nil
				}}})
				result, err := client.Chat(context.Background(), llm.Request{OnStream: func(event llm.StreamEvent) error {
					buffer.WriteString(event.Delta)
					if event.Done {
						streams = append(streams, buffer.String())
						buffer.Reset()
					}
					return nil
				}})
				if err != nil || result.Text != "complete" || !reflect.DeepEqual(streams, []string{"partial", "complete"}) {
					t.Fatalf("result=%+v err=%v streams=%v", result, err, streams)
				}
			})
		})
	}
}

func TestFallbackClientTaskDeadlineInterruptsBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		calls := 0
		primary := &testLLMClient{chatFn: func(context.Context, llm.Request) (llm.Result, error) {
			calls++
			return llm.Result{}, context.DeadlineExceeded
		}}
		client := NewFallbackClient(FallbackClientOptions{Primary: primary, Fallbacks: []FallbackCandidate{{Client: primary}}})
		start := time.Now()
		_, err := client.Chat(ctx, llm.Request{})
		if !errors.Is(err, context.DeadlineExceeded) || calls != 1 || time.Since(start) != 100*time.Millisecond {
			t.Fatalf("err=%v calls=%d elapsed=%s", err, calls, time.Since(start))
		}
	})
}
