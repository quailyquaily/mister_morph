package llmutil

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"testing/synctest"

	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/llm"
)

type testLLMClient struct {
	chatFn func(context.Context, llm.Request) (llm.Result, error)
}

func (c *testLLMClient) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	if c == nil || c.chatFn == nil {
		return llm.Result{}, errors.New("chatFn not configured")
	}
	return c.chatFn(ctx, req)
}

func TestFallbackClientFallsBackOnTimeoutAndUsesFallbackModel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var primaryCalls int
		var fallbackCalls int

		client := NewFallbackClient(FallbackClientOptions{
			Primary: &testLLMClient{
				chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
					primaryCalls++
					if req.Model != "gpt-5.2" {
						t.Fatalf("primary model = %q, want gpt-5.2", req.Model)
					}
					return llm.Result{}, context.DeadlineExceeded
				},
			},
			PrimaryProfile: "default",
			PrimaryModel:   "gpt-5.2",
			Fallbacks: []FallbackCandidate{
				{
					Profile: "cheap",
					Model:   "gpt-4.1-mini",
					Client: &testLLMClient{
						chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
							fallbackCalls++
							if req.Model != "gpt-4.1-mini" {
								t.Fatalf("fallback model = %q, want gpt-4.1-mini", req.Model)
							}
							return llm.Result{Text: "ok"}, nil
						},
					},
				},
			},
		})

		result, err := client.Chat(context.Background(), llm.Request{Model: "gpt-5.2"})
		if err != nil {
			t.Fatalf("Chat() error = %v", err)
		}
		if result.Text != "ok" {
			t.Fatalf("result text = %q, want ok", result.Text)
		}
		if primaryCalls != 6 {
			t.Fatalf("primary calls = %d, want 6", primaryCalls)
		}
		if fallbackCalls != 1 {
			t.Fatalf("fallback calls = %d, want 1", fallbackCalls)
		}
	})
}

func TestFallbackClientFallsBackOnHTTP5xx(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var primaryCalls int
		var fallbackCalls int

		client := NewFallbackClient(FallbackClientOptions{
			Primary: &testLLMClient{
				chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
					primaryCalls++
					if req.Model != "gpt-5.2" {
						t.Fatalf("primary model = %q, want gpt-5.2", req.Model)
					}
					return llm.Result{}, errors.New("openai API request failed with status 503: service unavailable")
				},
			},
			PrimaryProfile: "default",
			PrimaryModel:   "gpt-5.2",
			Fallbacks: []FallbackCandidate{
				{
					Profile: "cheap",
					Model:   "gpt-4.1-mini",
					Client: &testLLMClient{
						chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
							fallbackCalls++
							if req.Model != "gpt-4.1-mini" {
								t.Fatalf("fallback model = %q, want gpt-4.1-mini", req.Model)
							}
							return llm.Result{Text: "ok"}, nil
						},
					},
				},
			},
		})

		result, err := client.Chat(context.Background(), llm.Request{Model: "gpt-5.2"})
		if err != nil {
			t.Fatalf("Chat() error = %v", err)
		}
		if result.Text != "ok" {
			t.Fatalf("result text = %q, want ok", result.Text)
		}
		if primaryCalls != 6 {
			t.Fatalf("primary calls = %d, want 6", primaryCalls)
		}
		if fallbackCalls != 1 {
			t.Fatalf("fallback calls = %d, want 1", fallbackCalls)
		}
	})
}

func TestFallbackClientFallsBackOnConfigured4xx(t *testing.T) {
	var primaryCalls int
	var fallbackCalls int

	client := NewFallbackClient(FallbackClientOptions{
		Primary: &testLLMClient{
			chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
				primaryCalls++
				if req.Model != "gpt-5.2" {
					t.Fatalf("primary model = %q, want gpt-5.2", req.Model)
				}
				return llm.Result{}, errors.New("openai API request failed with status 404: model not found")
			},
		},
		PrimaryProfile: "default",
		PrimaryModel:   "gpt-5.2",
		Fallbacks: []FallbackCandidate{
			{
				Profile: "cheap",
				Model:   "gpt-4.1-mini",
				Client: &testLLMClient{
					chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
						fallbackCalls++
						if req.Model != "gpt-4.1-mini" {
							t.Fatalf("fallback model = %q, want gpt-4.1-mini", req.Model)
						}
						return llm.Result{Text: "ok"}, nil
					},
				},
			},
		},
	})

	result, err := client.Chat(context.Background(), llm.Request{Model: "gpt-5.2"})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if result.Text != "ok" {
		t.Fatalf("result text = %q, want ok", result.Text)
	}
	if primaryCalls != 1 {
		t.Fatalf("primary calls = %d, want 1", primaryCalls)
	}
	if fallbackCalls != 1 {
		t.Fatalf("fallback calls = %d, want 1", fallbackCalls)
	}
}

func TestFallbackClientRetriesUnmatchedErrorsBeforeEachFallback(t *testing.T) {
	for _, requestErr := range []error{errors.New("HTTP 400: bad request"), io.EOF, errors.New("unknown provider failure")} {
		t.Run(requestErr.Error(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				counts := map[string]int{}
				failing := &testLLMClient{chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
					counts[req.Model]++
					if req.Model == "backup" && counts["main"] != 6 {
						t.Fatal("switched profile before exhausting retries")
					}
					return llm.Result{}, requestErr
				}}
				client := NewFallbackClient(FallbackClientOptions{
					Primary: failing,
					Fallbacks: []FallbackCandidate{
						{Model: "backup", Client: failing},
						{Model: "last", Client: &testLLMClient{chatFn: func(context.Context, llm.Request) (llm.Result, error) {
							counts["last"]++
							return llm.Result{Text: "ok"}, nil
						}}},
					},
				})
				result, err := client.Chat(context.Background(), llm.Request{Model: "main"})
				if err != nil || result.Text != "ok" || counts["main"] != 6 || counts["backup"] != 6 || counts["last"] != 1 {
					t.Fatalf("result=%+v err=%v counts=%v", result, err, counts)
				}
			})
		})
	}
}

func TestFallbackEligibleReason(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		want   string
		wantOK bool
	}{
		{name: "timeout", err: context.DeadlineExceeded, want: "timeout", wantOK: true},
		{name: "401", err: errors.New("openai API request failed with status 401: unauthorized"), want: "status_401", wantOK: true},
		{name: "403", err: errors.New("openai API request failed with status 403: forbidden"), want: "status_403", wantOK: true},
		{name: "404", err: errors.New("openai API request failed with status 404: not found"), want: "status_404", wantOK: true},
		{name: "408", err: errors.New("openai API request failed with status 408: request timeout"), want: "status_408", wantOK: true},
		{name: "415", err: errors.New("openai API request failed with status 415: unsupported media type"), want: "status_415", wantOK: true},
		{name: "422", err: errors.New("openai API request failed with status 422: unprocessable entity"), want: "status_422", wantOK: true},
		{name: "429", err: errors.New("openai API request failed with status 429: too many requests"), want: "status_429", wantOK: true},
		{name: "529", err: errors.New("openai API request failed with status 529: overloaded"), want: "status_529", wantOK: true},
		{name: "503", err: errors.New("openai API request failed with status 503: service unavailable"), want: "status_503", wantOK: true},
		{name: "500 status code", err: errors.New("upstream API request failed with status code 500"), want: "status_500", wantOK: true},
		{name: "400", err: errors.New("openai API request failed with status 400: bad request"), want: "status_400", wantOK: true},
		{name: "EOF", err: io.EOF, want: "request_error", wantOK: true},
		{name: "unmatched", err: errors.New("unknown provider failure"), want: "request_error", wantOK: true},
		{name: "nil", want: "", wantOK: false},
		{name: "canceled", err: context.Canceled, want: "", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := fallbackEligibleReason(tt.err)
			if ok != tt.wantOK {
				t.Fatalf("fallbackEligibleReason() ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("fallbackEligibleReason() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildRouteClientBuildsPrimaryAndFallbackClients(t *testing.T) {
	buildCalls := make([]string, 0, 2)
	wrapCalls := make([]string, 0, 2)

	route := ResolvedRoute{
		Profile: "default",
		ClientConfig: llmconfig.ClientConfig{
			Provider: "openai",
			Model:    "gpt-5.2",
		},
		Fallbacks: []ResolvedFallback{
			{
				Profile: "cheap",
				ClientConfig: llmconfig.ClientConfig{
					Provider: "openai",
					Model:    "gpt-4.1-mini",
				},
			},
		},
	}
	override := llmconfig.ClientConfig{
		Provider: "openai",
		Model:    "gpt-5.2-override",
	}

	client, err := BuildRouteClient(
		route,
		&override,
		func(cfg llmconfig.ClientConfig, _ RuntimeValues) (llm.Client, error) {
			buildCalls = append(buildCalls, cfg.Model)
			switch cfg.Model {
			case "gpt-5.2-override":
				return &testLLMClient{
					chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
						if req.Model != "gpt-5.2-override" {
							t.Fatalf("primary request model = %q, want gpt-5.2-override", req.Model)
						}
						return llm.Result{}, errors.New("openai API request failed with status 429: too many requests")
					},
				}, nil
			case "gpt-4.1-mini":
				return &testLLMClient{
					chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
						return llm.Result{Text: req.Model}, nil
					},
				}, nil
			default:
				t.Fatalf("unexpected build model %q", cfg.Model)
				return nil, nil
			}
		},
		func(client llm.Client, cfg llmconfig.ClientConfig, _ string) llm.Client {
			wrapCalls = append(wrapCalls, cfg.Model)
			return client
		},
		nil,
	)
	if err != nil {
		t.Fatalf("BuildRouteClient() error = %v", err)
	}

	result, err := client.Chat(context.Background(), llm.Request{Model: "gpt-5.2-override"})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if result.Text != "gpt-4.1-mini" {
		t.Fatalf("result text = %q, want gpt-4.1-mini", result.Text)
	}
	if len(buildCalls) != 2 {
		t.Fatalf("build calls = %d, want 2", len(buildCalls))
	}
	if buildCalls[0] != "gpt-5.2-override" || buildCalls[1] != "gpt-4.1-mini" {
		t.Fatalf("build calls = %#v, want override then fallback", buildCalls)
	}
	if len(wrapCalls) != 2 {
		t.Fatalf("wrap calls = %d, want 2", len(wrapCalls))
	}
}

func TestBuildRouteClientWeightedCandidatesStickPerRun(t *testing.T) {
	client, err := BuildRouteClient(
		ResolvedRoute{
			Identity: "candidates=default=1,cheap=1",
			Candidates: []ResolvedCandidate{
				{
					Profile: "default",
					ClientConfig: llmconfig.ClientConfig{
						Provider: "openai",
						Model:    "gpt-5.2",
					},
					Weight: 1,
				},
				{
					Profile: "cheap",
					ClientConfig: llmconfig.ClientConfig{
						Provider: "openai",
						Model:    "gpt-4.1-mini",
					},
					Weight: 1,
				},
			},
		},
		nil,
		func(cfg llmconfig.ClientConfig, _ RuntimeValues) (llm.Client, error) {
			return &testLLMClient{
				chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
					return llm.Result{Text: req.Model}, nil
				},
			}, nil
		},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("BuildRouteClient() error = %v", err)
	}

	ctxA := llmstats.WithRunID(context.Background(), "run-a")
	first, err := client.Chat(ctxA, llm.Request{Scene: "runtime.loop"})
	if err != nil {
		t.Fatalf("first Chat() error = %v", err)
	}
	second, err := client.Chat(ctxA, llm.Request{Scene: "runtime.loop"})
	if err != nil {
		t.Fatalf("second Chat() error = %v", err)
	}
	if first.Text != second.Text {
		t.Fatalf("same run selected different models: first=%q second=%q", first.Text, second.Text)
	}
	if first.Text != "gpt-5.2" && first.Text != "gpt-4.1-mini" {
		t.Fatalf("unexpected selected model %q", first.Text)
	}

	foundDifferent := false
	for i := 0; i < 128; i++ {
		ctx := llmstats.WithRunID(context.Background(), fmt.Sprintf("run-%d", i))
		got, err := client.Chat(ctx, llm.Request{Scene: "runtime.loop"})
		if err != nil {
			t.Fatalf("Chat(run-%d) error = %v", i, err)
		}
		if got.Text != first.Text {
			foundDifferent = true
			break
		}
	}
	if !foundDifferent {
		t.Fatal("weighted candidates never selected an alternate profile across sampled run IDs")
	}
}

func TestBuildRouteClientWeightedCandidatesThenRouteFallbacks(t *testing.T) {
	requestModels := make([]string, 0, 3)
	client, err := BuildRouteClient(
		ResolvedRoute{
			Identity: "candidates=default=1,cheap=1|fallbacks=reasoning",
			Candidates: []ResolvedCandidate{
				{
					Profile: "default",
					ClientConfig: llmconfig.ClientConfig{
						Provider: "openai",
						Model:    "gpt-5.2",
					},
					Weight: 1,
				},
				{
					Profile: "cheap",
					ClientConfig: llmconfig.ClientConfig{
						Provider: "openai",
						Model:    "gpt-4.1-mini",
					},
					Weight: 1,
				},
			},
			Fallbacks: []ResolvedFallback{
				{
					Profile: "reasoning",
					ClientConfig: llmconfig.ClientConfig{
						Provider: "xai",
						Model:    "grok-4.1-fast-reasoning",
					},
				},
			},
		},
		nil,
		func(cfg llmconfig.ClientConfig, _ RuntimeValues) (llm.Client, error) {
			switch cfg.Model {
			case "gpt-5.2", "gpt-4.1-mini":
				return &testLLMClient{
					chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
						requestModels = append(requestModels, req.Model)
						return llm.Result{}, errors.New("openai API request failed with status 429: too many requests")
					},
				}, nil
			case "grok-4.1-fast-reasoning":
				return &testLLMClient{
					chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
						requestModels = append(requestModels, req.Model)
						return llm.Result{Text: req.Model}, nil
					},
				}, nil
			default:
				t.Fatalf("unexpected build model %q", cfg.Model)
				return nil, nil
			}
		},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("BuildRouteClient() error = %v", err)
	}

	result, err := client.Chat(llmstats.WithRunID(context.Background(), "route-fallback"), llm.Request{Scene: "runtime.loop"})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if result.Text != "grok-4.1-fast-reasoning" {
		t.Fatalf("result text = %q, want grok-4.1-fast-reasoning", result.Text)
	}
	if len(requestModels) != 3 {
		t.Fatalf("request models = %#v, want 3 attempts", requestModels)
	}
	if requestModels[2] != "grok-4.1-fast-reasoning" {
		t.Fatalf("final fallback model = %q, want grok-4.1-fast-reasoning", requestModels[2])
	}
	if requestModels[0] == requestModels[1] {
		t.Fatalf("candidate retry order = %#v, want primary then alternate candidate", requestModels)
	}
}
