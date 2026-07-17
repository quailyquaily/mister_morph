package agent

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type contextCompactionTestClient struct {
	mu      sync.Mutex
	calls   []llm.Request
	handler func(int, llm.Request) (llm.Result, error)
}

func (c *contextCompactionTestClient) Chat(_ context.Context, req llm.Request) (llm.Result, error) {
	c.mu.Lock()
	index := len(c.calls)
	c.calls = append(c.calls, req)
	c.mu.Unlock()
	return c.handler(index, req)
}

func (c *contextCompactionTestClient) allCalls() []llm.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]llm.Request(nil), c.calls...)
}

func contextCompactionTestConfig() Config {
	return Config{
		MaxSteps: 5,
		ContextCompaction: ContextCompactionConfig{
			TriggerRatio:        0.80,
			OutputReserveTokens: 100,
		},
	}
}

func contextCompactionToolResult() llm.Result {
	return llm.Result{
		ToolCalls: []llm.ToolCall{{ID: "call_one", Name: "read_file", Arguments: map[string]any{}}},
		Usage:     llm.Usage{InputTokens: 800, OutputTokens: 10, TotalTokens: 810},
	}
}

func contextCompactionResult() llm.Result {
	return llm.Result{
		Text:  validCheckpointJSON,
		Usage: llm.Usage{InputTokens: 300, OutputTokens: 80, TotalTokens: 380},
	}
}

func newContextCompactionEngine(client llm.Client, cfg Config) *Engine {
	registry := tools.NewRegistry()
	registry.Register(&mockTool{name: "read_file", result: "file contents"})
	return New(client, registry, cfg, DefaultPromptSpec(), WithPromptBuilder(func(*tools.Registry, string) string {
		return "system"
	}))
}

func TestRunCompactsBeforeNextMainCallAtThreshold(t *testing.T) {
	client := &contextCompactionTestClient{handler: func(index int, req llm.Request) (llm.Result, error) {
		switch index {
		case 0:
			return contextCompactionToolResult(), nil
		case 1:
			if req.Scene != "chat.context_compact" {
				t.Fatalf("compaction scene = %q", req.Scene)
			}
			if len(req.Tools) != 0 || !req.ForceJSON {
				t.Fatalf("compaction request tools/json = %d/%v", len(req.Tools), req.ForceJSON)
			}
			return contextCompactionResult(), nil
		case 2:
			return finalResponse("done"), nil
		default:
			return llm.Result{}, fmt.Errorf("unexpected call %d", index)
		}
	}}
	store := newRunLocalCheckpointStore()
	sink := &recordingEventSink{}
	ctx := WithEventSinkContext(context.Background(), sink)
	final, runCtx, err := newContextCompactionEngine(client, contextCompactionTestConfig()).Run(ctx, "old task", RunOptions{
		Model:                  "test-model",
		Scene:                  "chat.loop",
		ContextWindowTokens:    1000,
		ContextCheckpointStore: store,
		CurrentMessageBoundary: "current-boundary",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if final == nil || final.Output != "done" {
		t.Fatalf("final = %#v", final)
	}
	if runCtx.Metrics.LLMRounds != 3 || runCtx.Metrics.TotalTokens != 1190 {
		t.Fatalf("metrics = %+v, want 3 rounds and 1190 tokens", runCtx.Metrics)
	}
	calls := client.allCalls()
	if len(calls) != 3 {
		t.Fatalf("calls = %d, want 3", len(calls))
	}
	if requestContains(calls, 2, "old task") {
		t.Fatalf("post-compaction request still contains compacted task: %#v", calls[2].Messages)
	}
	if !requestContains(calls, 2, `"kind":"context_checkpoint"`) {
		t.Fatalf("post-compaction request has no checkpoint: %#v", calls[2].Messages)
	}
	checkpoint, ok, err := store.Load(context.Background())
	if err != nil || !ok {
		t.Fatalf("load checkpoint = ok:%v err:%v", ok, err)
	}
	if checkpoint.CoveredThrough != "current-boundary" || checkpoint.Revision != 1 || checkpoint.CompactionCount != 1 {
		t.Fatalf("checkpoint = %+v", checkpoint)
	}
	if !eventsContainKind(sink.all(), EventKindContextCompactionDone) {
		t.Fatalf("events = %#v, want context compaction done", sink.all())
	}
}

func TestRunDoesNotCompactBelowThreshold(t *testing.T) {
	client := &contextCompactionTestClient{handler: func(index int, req llm.Request) (llm.Result, error) {
		switch index {
		case 0:
			result := contextCompactionToolResult()
			result.Usage = llm.Usage{InputTokens: 100, OutputTokens: 10, TotalTokens: 110}
			return result, nil
		case 1:
			if strings.Contains(req.Scene, "context_compact") {
				t.Fatalf("unexpected compaction request: %+v", req)
			}
			return finalResponse("done"), nil
		default:
			return llm.Result{}, fmt.Errorf("unexpected call %d", index)
		}
	}}
	_, _, err := newContextCompactionEngine(client, contextCompactionTestConfig()).Run(context.Background(), "task", RunOptions{
		ContextWindowTokens: 1000,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if calls := client.allCalls(); len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
}

func TestRunContextCompactionOnlyCompactsFullSafePrefix(t *testing.T) {
	client := &contextCompactionTestClient{handler: func(index int, req llm.Request) (llm.Result, error) {
		if index != 0 {
			return llm.Result{}, fmt.Errorf("unexpected call %d", index)
		}
		if req.Scene != "chat.context_compact" {
			t.Fatalf("request scene = %q, want manual compaction", req.Scene)
		}
		if requestContains([]llm.Request{req}, 0, "/ctx compact") {
			t.Fatalf("manual command entered compaction payload: %#v", req.Messages)
		}
		for _, want := range []string{"oldest history", "middle history", "latest history"} {
			if !requestContains([]llm.Request{req}, 0, want) {
				t.Fatalf("compaction payload is missing %q: %#v", want, req.Messages)
			}
		}
		return contextCompactionResult(), nil
	}}
	store := newRunLocalCheckpointStore()
	history := []llm.Message{
		{Role: "user", Content: "oldest history"},
		{Role: "assistant", Content: "middle history"},
		{Role: "user", Content: "latest history"},
	}
	final, runCtx, err := newContextCompactionEngine(client, contextCompactionTestConfig()).Run(context.Background(), "/ctx compact", RunOptions{
		Model:                  "test-model",
		Scene:                  "chat.loop",
		History:                history,
		HistoryBoundaries:      []string{"history-1", "history-2", "history-3"},
		CurrentMessageBoundary: "manual-command",
		ContextCheckpointStore: store,
		ContextCompactionOnly:  true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if final == nil || final.Output != "Context compacted." || !final.IsLightweight {
		t.Fatalf("final = %#v", final)
	}
	if runCtx == nil || runCtx.Metrics.LLMRounds != 1 {
		t.Fatalf("run context = %#v, want one checkpoint LLM round", runCtx)
	}
	if calls := client.allCalls(); len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	checkpoint, found, err := store.Load(context.Background())
	if err != nil || !found {
		t.Fatalf("Load() found = %v, error = %v", found, err)
	}
	if checkpoint.CoveredThrough != "history-3" {
		t.Fatalf("covered through = %q, want history-3", checkpoint.CoveredThrough)
	}
}

func TestRunContextCompactionOnlyRespectsDisabledConfig(t *testing.T) {
	enabled := false
	cfg := contextCompactionTestConfig()
	cfg.ContextCompaction.Enabled = &enabled
	client := &contextCompactionTestClient{handler: func(index int, req llm.Request) (llm.Result, error) {
		return llm.Result{}, fmt.Errorf("unexpected request %d: %#v", index, req)
	}}
	_, _, err := newContextCompactionEngine(client, cfg).Run(context.Background(), "/ctx compact", RunOptions{
		History:               []llm.Message{{Role: "user", Content: "old history"}},
		ContextCompactionOnly: true,
	})
	if !errors.Is(err, ErrContextCompactionDisabled) {
		t.Fatalf("Run() error = %v, want ErrContextCompactionDisabled", err)
	}
	if calls := client.allCalls(); len(calls) != 0 {
		t.Fatalf("calls = %d, want 0", len(calls))
	}
}

func TestRunActiveCompactionFailureKeepsOriginalMessages(t *testing.T) {
	client := &contextCompactionTestClient{handler: func(index int, req llm.Request) (llm.Result, error) {
		switch index {
		case 0:
			return contextCompactionToolResult(), nil
		case 1:
			return llm.Result{Text: `{"summary":`}, nil
		case 2:
			if !requestContains([]llm.Request{req}, 0, "old task") {
				t.Fatalf("original task was removed after failed compaction: %#v", req.Messages)
			}
			return finalResponse("done"), nil
		default:
			return llm.Result{}, fmt.Errorf("unexpected call %d", index)
		}
	}}
	sink := &recordingEventSink{}
	ctx := WithEventSinkContext(context.Background(), sink)
	_, _, err := newContextCompactionEngine(client, contextCompactionTestConfig()).Run(ctx, "old task", RunOptions{ContextWindowTokens: 1000})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !eventsContainKind(sink.all(), EventKindContextCompactionFailed) {
		t.Fatalf("events = %#v, want context compaction failed", sink.all())
	}
}

func TestRunReportsActiveCompactionFailureWithContextLengthError(t *testing.T) {
	providerErr := llm.MarkContextLengthError(errors.New("maximum context length exceeded"))
	client := &contextCompactionTestClient{handler: func(index int, _ llm.Request) (llm.Result, error) {
		switch index {
		case 0:
			return contextCompactionToolResult(), nil
		case 1:
			return llm.Result{Text: `{"summary":`}, nil
		case 2:
			return llm.Result{}, providerErr
		default:
			return llm.Result{}, fmt.Errorf("unexpected call %d", index)
		}
	}}
	_, _, err := newContextCompactionEngine(client, contextCompactionTestConfig()).Run(context.Background(), "old task", RunOptions{
		ContextWindowTokens: 1000,
	})
	if !errors.Is(err, providerErr) {
		t.Fatalf("Run() error = %v, want provider context error", err)
	}
	if !strings.Contains(err.Error(), "context compaction failed") {
		t.Fatalf("Run() error = %v, want compaction failure reason", err)
	}
	if calls := client.allCalls(); len(calls) != 3 {
		t.Fatalf("calls = %d, want 3", len(calls))
	}
}

func TestRunRetriesContextLengthErrorOnceAfterCompaction(t *testing.T) {
	client := &contextCompactionTestClient{handler: func(index int, req llm.Request) (llm.Result, error) {
		switch index {
		case 0:
			return llm.Result{}, llm.MarkContextLengthError(errors.New("too long"))
		case 1:
			return contextCompactionResult(), nil
		case 2:
			return finalResponse("done"), nil
		default:
			return llm.Result{}, fmt.Errorf("unexpected call %d", index)
		}
	}}
	history := []llm.Message{{Role: "user", Content: strings.Repeat("old history ", 20)}}
	final, _, err := newContextCompactionEngine(client, contextCompactionTestConfig()).Run(context.Background(), "current task", RunOptions{
		Scene:               "telegram.loop",
		History:             history,
		HistoryBoundaries:   []string{"old-boundary"},
		ContextWindowTokens: 1000,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if final == nil || final.Output != "done" {
		t.Fatalf("final = %#v", final)
	}
	if calls := client.allCalls(); len(calls) != 3 || calls[1].Scene != "telegram.context_compact" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestRunDoesNotRetryContextLengthErrorTwice(t *testing.T) {
	providerErr := llm.MarkContextLengthError(errors.New("still too long"))
	client := &contextCompactionTestClient{handler: func(index int, _ llm.Request) (llm.Result, error) {
		switch index {
		case 0:
			return llm.Result{}, providerErr
		case 1:
			return contextCompactionResult(), nil
		case 2:
			return llm.Result{}, providerErr
		default:
			return llm.Result{}, fmt.Errorf("unexpected call %d", index)
		}
	}}
	_, _, err := newContextCompactionEngine(client, contextCompactionTestConfig()).Run(context.Background(), "current", RunOptions{
		History:             []llm.Message{{Role: "user", Content: "old"}},
		ContextWindowTokens: 1000,
	})
	if !errors.Is(err, providerErr) {
		t.Fatalf("run error = %v, want provider error", err)
	}
	if calls := client.allCalls(); len(calls) != 3 {
		t.Fatalf("calls = %d, want 3", len(calls))
	}
}

func TestRunLoadsPersistedCheckpointOnNextRun(t *testing.T) {
	store := newRunLocalCheckpointStore()
	checkpointContent, err := parseCheckpointContent([]byte(validCheckpointJSON), checkpointValidationOptions{})
	if err != nil {
		t.Fatalf("parse checkpoint: %v", err)
	}
	message, err := buildCheckpointMessage(checkpointContent)
	if err != nil {
		t.Fatalf("build checkpoint: %v", err)
	}
	if err := store.Save(context.Background(), 0, ContextCheckpoint{Version: 1, Revision: 1, Message: message, CoveredThrough: "old-boundary"}); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}
	client := newMockClient(finalResponse("done"))
	_, _, err = New(client, tools.NewRegistry(), contextCompactionTestConfig(), DefaultPromptSpec()).Run(context.Background(), "new task", RunOptions{
		ContextCheckpointStore: store,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	calls := client.allCalls()
	if len(calls) != 1 || !requestContains(calls, 0, `"kind":"context_checkpoint"`) {
		t.Fatalf("request does not contain loaded checkpoint: %#v", calls)
	}
}

func TestRunDisableContextCompactionOverridesDefault(t *testing.T) {
	client := &contextCompactionTestClient{handler: func(index int, req llm.Request) (llm.Result, error) {
		if strings.Contains(req.Scene, "context_compact") {
			t.Fatalf("unexpected compaction request: %+v", req)
		}
		if index == 0 {
			return contextCompactionToolResult(), nil
		}
		return finalResponse("done"), nil
	}}
	_, _, err := newContextCompactionEngine(client, contextCompactionTestConfig()).Run(context.Background(), "awareness task", RunOptions{
		ContextWindowTokens:      1000,
		DisableContextCompaction: true,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if calls := client.allCalls(); len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
}

func TestRunUsesContextWindowFromCurrentProfile(t *testing.T) {
	client := &contextCompactionTestClient{handler: func(index int, req llm.Request) (llm.Result, error) {
		switch index {
		case 0:
			if strings.Contains(req.Scene, "context_compact") {
				t.Fatalf("large-window run compacted unexpectedly")
			}
			return finalResponse("large"), nil
		case 1:
			if req.Scene != "chat.context_compact" {
				t.Fatalf("small-window first request scene = %q, want compaction", req.Scene)
			}
			return contextCompactionResult(), nil
		case 2:
			return finalResponse("small"), nil
		default:
			return llm.Result{}, fmt.Errorf("unexpected call %d", index)
		}
	}}
	engine := newContextCompactionEngine(client, contextCompactionTestConfig())
	history := []llm.Message{
		{Role: "user", Content: strings.Repeat("first profile history ", 80)},
		{Role: "assistant", Content: strings.Repeat("assistant history ", 80)},
	}
	if _, _, err := engine.Run(context.Background(), "current", RunOptions{
		Model:               "large-profile-model",
		Scene:               "chat.loop",
		History:             history,
		ContextWindowTokens: 10000,
	}); err != nil {
		t.Fatalf("large-window run: %v", err)
	}
	if _, _, err := engine.Run(context.Background(), "current", RunOptions{
		Model:               "small-profile-model",
		Scene:               "chat.loop",
		History:             history,
		ContextWindowTokens: 1000,
	}); err != nil {
		t.Fatalf("small-window run: %v", err)
	}
	if calls := client.allCalls(); len(calls) != 3 {
		t.Fatalf("calls = %d, want 3", len(calls))
	}
}

func TestContextCompactionUsageIsWrittenToUsageJournal(t *testing.T) {
	baseClient := &contextCompactionTestClient{handler: func(index int, _ llm.Request) (llm.Result, error) {
		switch index {
		case 0:
			return contextCompactionToolResult(), nil
		case 1:
			return contextCompactionResult(), nil
		case 2:
			return finalResponse("done"), nil
		default:
			return llm.Result{}, fmt.Errorf("unexpected call %d", index)
		}
	}}
	journalDir := t.TempDir()
	usageClient := llmstats.WrapClient(baseClient, llmstats.ClientOptions{
		Provider:     "test",
		DefaultModel: "test-model",
		JournalDir:   journalDir,
	}).(*llmstats.UsageClient)
	engine := newContextCompactionEngine(usageClient, contextCompactionTestConfig())
	if _, _, err := engine.Run(context.Background(), "old task", RunOptions{
		Model:               "test-model",
		Scene:               "chat.loop",
		ContextWindowTokens: 1000,
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := usageClient.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	entries, err := os.ReadDir(journalDir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("journal files = %d, want 1", len(entries))
	}
	raw, err := os.ReadFile(filepath.Join(journalDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(raw), `"scene":"chat.context_compact"`) {
		t.Fatalf("usage journal does not contain compaction scene: %s", raw)
	}
}

func TestRunCompactionSendsRealImagePartAndPersistsReference(t *testing.T) {
	rawImage := []byte("stable-image-data")
	hash := sha256.Sum256(rawImage)
	wantPath := fmt.Sprintf("workspace_dir/.mistermorph/context-images/%x.png", hash)
	wantBase64 := base64.StdEncoding.EncodeToString(rawImage)
	client := &contextCompactionTestClient{handler: func(index int, req llm.Request) (llm.Result, error) {
		switch index {
		case 0:
			if req.Scene != "chat.context_compact" {
				t.Fatalf("first request scene = %q, want compaction", req.Scene)
			}
			if len(req.Messages) != 2 || len(req.Messages[1].Parts) < 2 {
				t.Fatalf("compaction messages = %#v", req.Messages)
			}
			if !strings.Contains(req.Messages[1].Parts[0].Text, wantPath) {
				t.Fatalf("serialized payload has no file reference: %s", req.Messages[1].Parts[0].Text)
			}
			if strings.Contains(req.Messages[1].Parts[0].Text, wantBase64) {
				t.Fatal("serialized payload still contains image base64")
			}
			foundImagePart := false
			for _, part := range req.Messages[1].Parts[1:] {
				if part.Type == llm.PartTypeImageBase64 && part.DataBase64 == wantBase64 {
					foundImagePart = true
				}
			}
			if !foundImagePart {
				t.Fatalf("compaction request has no real image part: %#v", req.Messages[1].Parts)
			}
			return llm.Result{Text: fmt.Sprintf(`{"summary":"image inspected","user_intent":["inspect image"],"references":{"files":[%q],"directories":[],"urls":[]},"progress":{"completed":[],"in_progress":[],"pending":[]},"intermediate_results":[]}`, wantPath)}, nil
		case 1:
			return finalResponse("done"), nil
		default:
			return llm.Result{}, fmt.Errorf("unexpected call %d", index)
		}
	}}
	store := newRunLocalCheckpointStore()
	workspaceDir := t.TempDir()
	ctx := pathroots.WithWorkspaceDir(context.Background(), workspaceDir)
	history := []llm.Message{
		{
			Role:    "user",
			Content: "inspect image",
			Parts: []llm.Part{
				{Type: llm.PartTypeText, Text: "inspect image"},
				{Type: llm.PartTypeImageBase64, MIMEType: "image/png", DataBase64: wantBase64},
			},
		},
		{Role: "assistant", Content: strings.Repeat("working on image ", 20)},
	}
	if _, _, err := newContextCompactionEngine(client, contextCompactionTestConfig()).Run(ctx, "continue", RunOptions{
		Model:                  "test-model",
		Scene:                  "chat.loop",
		History:                history,
		ContextWindowTokens:    300,
		ContextCheckpointStore: store,
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	checkpoint, found, err := store.Load(context.Background())
	if err != nil || !found {
		t.Fatalf("Load() found = %v, error = %v", found, err)
	}
	if !strings.Contains(checkpoint.Message.Content, wantPath) || strings.Contains(checkpoint.Message.Content, wantBase64) {
		t.Fatalf("checkpoint content = %s", checkpoint.Message.Content)
	}
}

func TestForceConclusionUsesContextCompaction(t *testing.T) {
	client := &contextCompactionTestClient{handler: func(index int, req llm.Request) (llm.Result, error) {
		switch index {
		case 0:
			return contextCompactionToolResult(), nil
		case 1:
			if req.Scene != "chat.context_compact" {
				t.Fatalf("force-conclusion second request scene = %q, want compaction", req.Scene)
			}
			return contextCompactionResult(), nil
		case 2:
			if !requestContains([]llm.Request{req}, 0, "Provide your final output NOW") {
				t.Fatalf("force-conclusion instruction missing: %#v", req.Messages)
			}
			return finalResponse("done"), nil
		default:
			return llm.Result{}, fmt.Errorf("unexpected call %d", index)
		}
	}}
	cfg := contextCompactionTestConfig()
	cfg.MaxSteps = 1
	final, _, err := newContextCompactionEngine(client, cfg).Run(context.Background(), "task", RunOptions{
		Model:               "test-model",
		Scene:               "chat.loop",
		ContextWindowTokens: 1000,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if final == nil || final.Output != "done" {
		t.Fatalf("final = %#v", final)
	}
}
