package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

func TestForceConclusionReceivesTriggerReason(t *testing.T) {
	for _, tt := range []struct {
		reason string
		prompt string
	}{
		{"max_steps", "You have reached the maximum number of steps."},
		{"token_budget", "You have exceeded the token budget."},
		{"parse_retries_exhausted", "Your responses could not be parsed and the response-format retry limit has been exhausted."},
		{"task_deadline_exceeded", "The parent task deadline has expired."},
	} {
		t.Run(tt.reason, func(t *testing.T) {
			cfg := baseCfg()
			cfg.MaxSteps = 1
			cfg.ParseRetries = 1
			ctx := context.Background()
			wantCalls := 2
			switch tt.reason {
			case "token_budget":
				cfg.MaxTokenBudget = 1
			case "parse_retries_exhausted":
				cfg.ParseRetries = 0
			case "task_deadline_exceeded":
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
				wantCalls = 1
			}
			calls := 0
			client := deadlineTestClient(func(callCtx context.Context, req llm.Request) (llm.Result, error) {
				calls++
				if calls < wantCalls {
					return llm.Result{Text: "not JSON", Usage: llm.Usage{TotalTokens: 2}}, nil
				}
				if callCtx.Err() != nil || len(req.Tools) != 0 {
					t.Errorf("summary context/tools = %v/%d", callCtx.Err(), len(req.Tools))
				}
				prompt := req.Messages[len(req.Messages)-1]
				if prompt.Role != "user" || !strings.HasPrefix(prompt.Content, tt.prompt) {
					t.Errorf("summary prompt = %+v, want reason %q", prompt, tt.prompt)
				}
				return finalResponse("summary"), nil
			})
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			engine := New(client, tools.NewRegistry(), cfg, DefaultPromptSpec(), WithLogger(logger))
			final, _, err := engine.Run(ctx, "work", RunOptions{})
			if err != nil || final == nil || final.Output != "summary" || calls != wantCalls {
				t.Fatalf("final=%+v err=%v calls=%d, want %d", final, err, calls, wantCalls)
			}
			found := false
			for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
				var entry map[string]any
				if err := json.Unmarshal(line, &entry); err != nil {
					t.Fatal(err)
				}
				if entry["msg"] == "force_conclusion" {
					found = true
					if entry["reason"] != tt.reason {
						t.Errorf("logged reason = %v, want %q", entry["reason"], tt.reason)
					}
				}
			}
			if !found {
				t.Fatal("missing force_conclusion log")
			}
		})
	}
}
