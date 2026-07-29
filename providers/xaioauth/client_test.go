package xaioauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/quailyquaily/mistermorph/internal/testhttp"
	"github.com/quailyquaily/mistermorph/internal/xaiauth"
	"github.com/quailyquaily/mistermorph/llm"
)

const testOAuthScope = "openid offline_access model.request"

func TestNewCopiesTemperature(t *testing.T) {
	temperature := 0.2
	client := New(Config{Temperature: &temperature})
	temperature = 0.9

	if client.cfg.Temperature == nil || *client.cfg.Temperature != 0.2 {
		t.Fatalf("temperature = %v, want an independent 0.2 copy", client.cfg.Temperature)
	}
}

func TestClientUsesFixedResponsesEndpointAndMapsImageInput(t *testing.T) {
	now := time.Date(2026, 7, 29, 4, 0, 0, 0, time.UTC)
	stateDir := writeUsableToken(t, now, "access-secret", "refresh-secret")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host != "api.x.ai" || r.URL.Path != "/v1/responses" {
			t.Fatalf("request URL = %s", r.URL.String())
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-secret" {
			t.Fatalf("authorization = %q", got)
		}
		for _, name := range []string{"Proxy-Authorization", "X-API-Key", "API-Key"} {
			if got := r.Header.Get(name); got != "" {
				t.Fatalf("%s = %q, want empty", name, got)
			}
		}
		if got := r.Header.Get("X-Test"); got != "kept" {
			t.Fatalf("X-Test = %q", got)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		body := string(raw)
		if !strings.Contains(body, `"model":"grok-4.5"`) {
			t.Fatalf("request has wrong model: %s", body)
		}
		if !strings.Contains(body, `"type":"input_image"`) ||
			!strings.Contains(body, `data:image/png;base64,QUJD`) {
			t.Fatalf("request has no image input: %s", body)
		}
		writeCompletedResponse(t, w, "ok")
	})
	testhttp.WithDefaultTransport(t, handler)

	client := New(Config{
		StateDir: stateDir,
		Model:    "grok-4.5",
		Headers: map[string]string{
			"Authorization":       "Bearer attacker",
			"Proxy-Authorization": "Basic attacker",
			"X-API-Key":           "attacker-key",
			"API-Key":             "attacker-key",
			"X-Test":              "kept",
		},
		OAuth: xaiauth.OAuthConfig{
			ClientID: "mistermorph-test-client",
			Scope:    testOAuthScope,
			Now:      func() time.Time { return now },
		},
	})
	result, err := client.Chat(context.Background(), llm.Request{
		Messages: []llm.Message{{
			Role: "user",
			Parts: []llm.Part{
				{Type: llm.PartTypeText, Text: "describe this"},
				{Type: llm.PartTypeImageBase64, MIMEType: "image/png", DataBase64: "QUJD"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if result.Text != "ok" || result.Usage.TotalTokens != 3 {
		t.Fatalf("result = %+v", result)
	}
	if result.Usage.Cost != nil {
		t.Fatalf("subscription usage must not have monetary cost: %+v", result.Usage.Cost)
	}
}

func TestClientRetriesUnauthorizedOnlyOnce(t *testing.T) {
	now := time.Date(2026, 7, 29, 4, 30, 0, 0, time.UTC)
	stateDir := writeUsableToken(t, now, "access-old", "refresh-old")
	var inferenceCount atomic.Int32
	var refreshCount atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeJSON(w, map[string]any{
				"issuer":                        xaiauth.DefaultIssuer,
				"device_authorization_endpoint": xaiauth.DefaultIssuer + "/oauth2/device/code",
				"token_endpoint":                xaiauth.DefaultIssuer + "/oauth2/token",
			})
		case "/oauth2/token":
			refreshCount.Add(1)
			writeJSON(w, map[string]any{
				"access_token":  "access-new",
				"refresh_token": "refresh-new",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		case "/v1/responses":
			inferenceCount.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			writeJSON(w, map[string]any{
				"error": map[string]any{
					"message": "expired",
					"type":    "invalid_request_error",
					"code":    "invalid_token",
				},
			})
		default:
			http.NotFound(w, r)
		}
	})
	testhttp.WithDefaultTransport(t, handler)

	client := New(Config{
		StateDir: stateDir,
		OAuth: xaiauth.OAuthConfig{
			ClientID:   "mistermorph-test-client",
			Scope:      testOAuthScope,
			HTTPClient: testhttp.NewClient(handler),
			Now:        func() time.Time { return now },
		},
	})
	_, err := client.Chat(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
	})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Chat() error = %v, want ErrUnauthorized", err)
	}
	if got := inferenceCount.Load(); got != 2 {
		t.Fatalf("inference count = %d, want 2", got)
	}
	if got := refreshCount.Load(); got != 1 {
		t.Fatalf("refresh count = %d, want 1", got)
	}
}

func TestClientRequestTimeoutIncludesTokenRefresh(t *testing.T) {
	now := time.Date(2026, 7, 29, 4, 45, 0, 0, time.UTC)
	stateDir := t.TempDir()
	if err := xaiauth.WriteToken(stateDir, xaiauth.Token{
		AccessToken:  "access-old",
		RefreshToken: "refresh-old",
		TokenType:    "Bearer",
		ExpiresAt:    now.Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeJSON(w, map[string]any{
				"issuer":                        xaiauth.DefaultIssuer,
				"device_authorization_endpoint": xaiauth.DefaultIssuer + "/oauth2/device/code",
				"token_endpoint":                xaiauth.DefaultIssuer + "/oauth2/token",
			})
		case "/oauth2/token":
			select {
			case <-r.Context().Done():
				return
			case <-time.After(150 * time.Millisecond):
				writeJSON(w, map[string]any{
					"access_token":  "access-new",
					"refresh_token": "refresh-new",
					"token_type":    "Bearer",
					"expires_in":    3600,
				})
			}
		case "/v1/responses":
			writeCompletedResponse(t, w, "late")
		default:
			http.NotFound(w, r)
		}
	})
	testhttp.WithDefaultTransport(t, handler)

	client := New(Config{
		StateDir:       stateDir,
		RequestTimeout: 20 * time.Millisecond,
		OAuth: xaiauth.OAuthConfig{
			ClientID:   "mistermorph-test-client",
			Scope:      testOAuthScope,
			HTTPClient: testhttp.NewClient(handler),
			Now:        func() time.Time { return now },
		},
	})
	start := time.Now()
	_, err := client.Chat(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Chat() error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed >= 100*time.Millisecond {
		t.Fatalf("Chat() elapsed = %s, want refresh bounded by request timeout", elapsed)
	}
}

func TestClientRefreshesOnceAfterUnauthorized(t *testing.T) {
	now := time.Date(2026, 7, 29, 5, 0, 0, 0, time.UTC)
	stateDir := writeUsableToken(t, now, "access-old", "refresh-old")
	var inferenceCount atomic.Int32
	var refreshCount atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeJSON(w, map[string]any{
				"issuer":                        xaiauth.DefaultIssuer,
				"device_authorization_endpoint": xaiauth.DefaultIssuer + "/oauth2/device/code",
				"token_endpoint":                xaiauth.DefaultIssuer + "/oauth2/token",
				"revocation_endpoint":           xaiauth.DefaultIssuer + "/oauth2/revoke",
			})
		case "/oauth2/token":
			refreshCount.Add(1)
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("refresh_token") != "refresh-old" {
				t.Fatalf("refresh token = %q", r.Form.Get("refresh_token"))
			}
			writeJSON(w, map[string]any{
				"access_token":  "access-new",
				"refresh_token": "refresh-new",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		case "/v1/responses":
			inferenceCount.Add(1)
			switch r.Header.Get("Authorization") {
			case "Bearer access-old":
				w.WriteHeader(http.StatusUnauthorized)
				writeJSON(w, map[string]any{
					"error": map[string]any{
						"message": "expired",
						"type":    "invalid_request_error",
						"code":    "invalid_token",
					},
				})
			case "Bearer access-new":
				writeCompletedResponse(t, w, "refreshed")
			default:
				t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
			}
		default:
			http.NotFound(w, r)
		}
	})
	testhttp.WithDefaultTransport(t, handler)

	client := New(Config{
		StateDir: stateDir,
		Model:    "grok-4.5",
		OAuth: xaiauth.OAuthConfig{
			ClientID:   "mistermorph-test-client",
			Scope:      testOAuthScope,
			HTTPClient: testhttp.NewClient(handler),
			Now:        func() time.Time { return now },
		},
	})
	result, err := client.Chat(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if result.Text != "refreshed" {
		t.Fatalf("text = %q", result.Text)
	}
	if got := inferenceCount.Load(); got != 2 {
		t.Fatalf("inference count = %d, want 2", got)
	}
	if got := refreshCount.Load(); got != 1 {
		t.Fatalf("refresh count = %d, want 1", got)
	}
}

func TestClientMapsForbiddenToEntitlementError(t *testing.T) {
	now := time.Date(2026, 7, 29, 6, 0, 0, 0, time.UTC)
	stateDir := writeUsableToken(t, now, "access-secret", "refresh-secret")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		writeJSON(w, map[string]any{
			"error": map[string]any{
				"message": "not entitled",
				"type":    "permission_error",
				"code":    "forbidden",
			},
		})
	})
	testhttp.WithDefaultTransport(t, handler)

	client := New(Config{
		StateDir: stateDir,
		OAuth: xaiauth.OAuthConfig{
			ClientID: "mistermorph-test-client",
			Scope:    testOAuthScope,
			Now:      func() time.Time { return now },
		},
	})
	_, err := client.Chat(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
	})
	if !errors.Is(err, ErrEntitlement) {
		t.Fatalf("Chat() error = %v, want ErrEntitlement", err)
	}
}

func TestClientPreservesToolCallAndToolResultAcrossTurns(t *testing.T) {
	now := time.Date(2026, 7, 29, 7, 0, 0, 0, time.UTC)
	stateDir := writeUsableToken(t, now, "access-secret", "refresh-secret")
	var requests atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		body := string(raw)
		switch requests.Add(1) {
		case 1:
			if !strings.Contains(body, `"type":"function"`) ||
				!strings.Contains(body, `"name":"weather"`) {
				t.Fatalf("first request has no function tool: %s", body)
			}
			writeCompletedToolCallResponse(t, w)
		case 2:
			for _, want := range []string{
				`"type":"function_call"`,
				`"call_id":"call_weather_1"`,
				`"type":"function_call_output"`,
				`"output":"sunny"`,
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("second request missing %s: %s", want, body)
				}
			}
			writeCompletedResponse(t, w, "It is sunny.")
		default:
			t.Fatalf("unexpected request %d", requests.Load())
		}
	})
	testhttp.WithDefaultTransport(t, handler)

	client := New(Config{
		StateDir: stateDir,
		OAuth: xaiauth.OAuthConfig{
			ClientID: "mistermorph-test-client",
			Scope:    testOAuthScope,
			Now:      func() time.Time { return now },
		},
	})
	user := llm.Message{Role: "user", Content: "How is the weather?"}
	first, err := client.Chat(context.Background(), llm.Request{
		Messages: []llm.Message{user},
		Tools: []llm.Tool{{
			Name:           "weather",
			Description:    "Read weather",
			ParametersJSON: `{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`,
		}},
	})
	if err != nil {
		t.Fatalf("first Chat() error = %v", err)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].ID != "call_weather_1" ||
		first.ToolCalls[0].Name != "weather" || first.ToolCalls[0].RawArguments != `{"city":"Tokyo"}` {
		t.Fatalf("first tool calls = %#v", first.ToolCalls)
	}

	second, err := client.Chat(context.Background(), llm.Request{
		Messages: []llm.Message{
			user,
			{Role: "assistant", ToolCalls: first.ToolCalls},
			{Role: "tool", ToolCallID: first.ToolCalls[0].ID, Content: "sunny"},
		},
		Tools: []llm.Tool{{
			Name:           "weather",
			Description:    "Read weather",
			ParametersJSON: `{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`,
		}},
	})
	if err != nil {
		t.Fatalf("second Chat() error = %v", err)
	}
	if second.Text != "It is sunny." {
		t.Fatalf("second text = %q", second.Text)
	}
}

func TestClientStreamsTextAndUsageWithoutMonetaryCost(t *testing.T) {
	now := time.Date(2026, 7, 29, 7, 30, 0, 0, time.UTC)
	stateDir := writeUsableToken(t, now, "access-secret", "refresh-secret")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSEJSON(t, w, map[string]any{
			"type":            "response.output_text.delta",
			"output_index":    0,
			"content_index":   0,
			"sequence_number": 1,
			"item_id":         "msg_1",
			"delta":           "streamed",
			"logprobs":        []any{},
		})
		writeCompletedResponse(t, w, "streamed")
	})
	testhttp.WithDefaultTransport(t, handler)

	client := New(Config{
		StateDir: stateDir,
		OAuth: xaiauth.OAuthConfig{
			ClientID: "mistermorph-test-client",
			Scope:    testOAuthScope,
			Now:      func() time.Time { return now },
		},
	})
	var events []llm.StreamEvent
	result, err := client.Chat(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
		OnStream: func(event llm.StreamEvent) error {
			events = append(events, event)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if result.Text != "streamed" {
		t.Fatalf("result text = %q", result.Text)
	}
	if len(events) < 2 || events[0].Delta != "streamed" {
		t.Fatalf("stream events = %#v", events)
	}
	final := events[len(events)-1]
	if !final.Done || final.Usage == nil || final.Usage.TotalTokens != 3 {
		t.Fatalf("final stream event = %#v", final)
	}
	if final.Usage.Cost != nil {
		t.Fatalf("subscription stream usage must not have monetary cost: %#v", final.Usage.Cost)
	}
}

func TestClientNotLoggedInErrorExplainsHowToLogin(t *testing.T) {
	client := New(Config{
		StateDir: t.TempDir(),
		OAuth: xaiauth.OAuthConfig{
			ClientID: "mistermorph-test-client",
			Scope:    testOAuthScope,
		},
	})
	_, err := client.Chat(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
	})
	if !errors.Is(err, xaiauth.ErrNotLoggedIn) {
		t.Fatalf("Chat() error = %v, want ErrNotLoggedIn", err)
	}
	if !strings.Contains(err.Error(), "mistermorph auth xai login") {
		t.Fatalf("Chat() error does not explain how to login: %v", err)
	}
}

func TestSanitizeInferenceErrorIncludesSafeActionableDetails(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		headers    http.Header
		want       []string
	}{
		{
			name:       "entitlement",
			statusCode: http.StatusForbidden,
			want:       []string{"entitlement", "region", "team policy"},
		},
		{
			name:       "rate limit",
			statusCode: http.StatusTooManyRequests,
			headers:    http.Header{"Retry-After": []string{"12"}},
			want:       []string{"HTTP 429", "retry after 12s"},
		},
		{
			name:       "model unavailable",
			statusCode: http.StatusNotFound,
			want:       []string{"grok-special", "available model"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := sanitizeInferenceResultError(llm.Result{}, &openai.Error{
				StatusCode: tt.statusCode,
				Request:    httptest.NewRequest(http.MethodPost, xaiauth.DefaultAPIBase+"/responses", nil),
				Response: &http.Response{
					StatusCode: tt.statusCode,
					Header:     tt.headers,
				},
			}, "grok-special")
			if err == nil {
				t.Fatal("sanitizeInferenceResultError() expected error")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error = %q, want %q", err, want)
				}
			}
		})
	}
}

func writeUsableToken(t *testing.T, now time.Time, accessToken, refreshToken string) string {
	t.Helper()
	stateDir := t.TempDir()
	if err := xaiauth.WriteToken(stateDir, xaiauth.Token{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresAt:    now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	return stateDir
}

func writeCompletedResponse(t *testing.T, w http.ResponseWriter, text string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	payload := map[string]any{
		"type":            "response.completed",
		"sequence_number": 1,
		"response": map[string]any{
			"id":                  "resp_123",
			"model":               "grok-4.5",
			"object":              "response",
			"parallel_tool_calls": true,
			"status":              "completed",
			"output": []any{map[string]any{
				"id":     "msg_1",
				"type":   "message",
				"role":   "assistant",
				"status": "completed",
				"content": []any{map[string]any{
					"type":        "output_text",
					"text":        text,
					"annotations": []any{},
				}},
			}},
			"usage": map[string]any{
				"input_tokens":         2,
				"input_tokens_details": map[string]any{},
				"output_tokens":        1,
				"output_tokens_details": map[string]any{
					"reasoning_tokens": 0,
				},
				"total_tokens": 3,
			},
			"text": map[string]any{
				"format": map[string]any{"type": "text"},
			},
		},
	}
	writeSSEJSON(t, w, payload)
}

func writeCompletedToolCallResponse(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	payload := map[string]any{
		"type":            "response.completed",
		"sequence_number": 1,
		"response": map[string]any{
			"id":                  "resp_tool_1",
			"model":               "grok-4.5",
			"object":              "response",
			"parallel_tool_calls": true,
			"status":              "completed",
			"output": []any{map[string]any{
				"id":        "fc_1",
				"type":      "function_call",
				"call_id":   "call_weather_1",
				"name":      "weather",
				"arguments": `{"city":"Tokyo"}`,
				"status":    "completed",
			}},
			"usage": map[string]any{
				"input_tokens":         8,
				"input_tokens_details": map[string]any{},
				"output_tokens":        3,
				"output_tokens_details": map[string]any{
					"reasoning_tokens": 0,
				},
				"total_tokens": 11,
			},
			"text": map[string]any{
				"format": map[string]any{"type": "text"},
			},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(append(append([]byte("data: "), raw...), []byte("\n\n")...)); err != nil {
		t.Fatal(err)
	}
}

func writeSSEJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(append(append([]byte("data: "), raw...), []byte("\n\n")...)); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
