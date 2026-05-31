package llminspect

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/llm"
)

func TestPromptInspectorDumpWithMetadataIncludesAPIBaseModelAndScene(t *testing.T) {
	dir := t.TempDir()
	inspector, err := NewPromptInspector(Options{DumpDir: dir, Mode: "telegram", Task: "demo"})
	if err != nil {
		t.Fatalf("NewPromptInspector() error = %v", err)
	}
	defer func() { _ = inspector.Close() }()

	err = inspector.DumpWithMetadata(InspectMetadata{
		APIBase: "https://api.openai.com/v1",
		Model:   "gpt-5.2",
		Scene:   "telegram.loop",
	}, []llm.Message{{Role: "user", Content: "hello"}})
	if err != nil {
		t.Fatalf("DumpWithMetadata() error = %v", err)
	}

	got := readSingleDumpFile(t, dir)
	mustContainAll(t, got,
		"api_base: https://api.openai.com/v1",
		"model: gpt-5.2",
		"scene: `telegram.loop`",
	)
}

func TestRequestInspectorEventDumpIncludesAPIBaseModelAndScene(t *testing.T) {
	dir := t.TempDir()
	inspector, err := NewRequestInspector(Options{DumpDir: dir, Mode: "telegram", Task: "demo"})
	if err != nil {
		t.Fatalf("NewRequestInspector() error = %v", err)
	}
	defer func() { _ = inspector.Close() }()

	event := inspector.NewEvent(InspectMetadata{
		APIBase: "https://api.openai.com/v1",
		Model:   "gpt-5.2",
		Scene:   "telegram.loop",
	})
	event.Dump("openai.chat.request", `{"messages":[]}`)
	event.Dump("openai.chat.response", `{"id":"resp_1"}`)

	got := readSingleDumpFile(t, dir)
	mustContainAll(t, got,
		"===[ Event #1 ]===========================",
		"api_base: https://api.openai.com/v1",
		"model: gpt-5.2",
		"scene: `telegram.loop`",
		"---[ openai.chat.request #1-1 ]---------------------------",
		"---[ openai.chat.response #1-2 ]---------------------------",
	)
}

func TestRequestInspectorEventDumpNormalizesFencedJSONStrings(t *testing.T) {
	dir := t.TempDir()
	inspector, err := NewRequestInspector(Options{DumpDir: dir, Mode: "telegram", Task: "demo"})
	if err != nil {
		t.Fatalf("NewRequestInspector() error = %v", err)
	}
	defer func() { _ = inspector.Close() }()

	event := inspector.NewEvent(InspectMetadata{
		APIBase: "https://api.openai.com/v1",
		Model:   "gpt-5.2",
		Scene:   "telegram.loop",
	})
	event.Dump("openai.chat.response", `{"choices":[{"message":{"content":"`+"```json"+`\n{\"type\":\"final\",\"output\":\"ok\"}\n`+"```"+`"}}]}`)

	got := readSingleDumpFile(t, dir)
	if strings.Contains(got, "```json") {
		t.Fatalf("dump should not keep fenced JSON content:\n%s", got)
	}
	mustContainAll(t, got, `"content": "{\"type\":\"final\",\"output\":\"ok\"}"`)
}

func TestWrapClientInjectsRequestScopedDebugFn(t *testing.T) {
	dir := t.TempDir()
	inspector, err := NewRequestInspector(Options{DumpDir: dir, Mode: "telegram", Task: "demo"})
	if err != nil {
		t.Fatalf("NewRequestInspector() error = %v", err)
	}
	defer func() { _ = inspector.Close() }()

	base := fakeClient{chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
		if req.DebugFn == nil {
			t.Fatalf("expected request-scoped debug callback")
		}
		req.DebugFn("openai.chat.request", `{"messages":[]}`)
		req.DebugFn("openai.chat.response", `{"id":"resp_1"}`)
		return llm.Result{}, nil
	}}
	client := WrapClient(base, ClientOptions{
		RequestInspector: inspector,
		APIBase:          "https://api.openai.com/v1",
		Model:            "gpt-5.2",
	})

	called := false
	_, err = client.Chat(context.Background(), llm.Request{
		Scene: "telegram.loop",
		DebugFn: func(label, payload string) {
			called = label == "openai.chat.response" && payload == `{"id":"resp_1"}`
		},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if !called {
		t.Fatalf("expected original request debug callback to remain active")
	}

	got := readSingleDumpFile(t, dir)
	mustContainAll(t, got,
		"===[ Event #1 ]===========================",
		"api_base: https://api.openai.com/v1",
		"model: gpt-5.2",
		"scene: `telegram.loop`",
		"---[ openai.chat.request #1-1 ]---------------------------",
		"---[ openai.chat.response #1-2 ]---------------------------",
	)
}

type fakeClient struct {
	chatFn func(ctx context.Context, req llm.Request) (llm.Result, error)
}

func (f fakeClient) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	return f.chatFn(ctx, req)
}

func readSingleDumpFile(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", dir, err)
	}
	if len(entries) != 1 {
		t.Fatalf("dump file count = %d, want 1", len(entries))
	}
	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", entries[0].Name(), err)
	}
	return string(data)
}

func TestBuildFilename_NoPrefix(t *testing.T) {
	ts := time.Date(2026, 3, 21, 14, 30, 0, 0, time.UTC)
	got := buildFilename("", "prompt", "", ts, "")
	if got != "prompt_20260321_1430.md" {
		t.Fatalf("got %q, want prompt_20260321_1430.md", got)
	}
}

func TestBuildFilename_WithPrefix(t *testing.T) {
	ts := time.Date(2026, 3, 21, 14, 30, 0, 0, time.UTC)
	got := buildFilename("spawn", "prompt", "", ts, "")
	if got != "spawn-prompt_20260321_1430.md" {
		t.Fatalf("got %q, want spawn-prompt_20260321_1430.md", got)
	}
}

func TestBuildFilename_WithPrefixAndMode(t *testing.T) {
	ts := time.Date(2026, 3, 21, 14, 30, 0, 0, time.UTC)
	got := buildFilename("spawn", "request", "telegram", ts, "")
	if got != "spawn-request_telegram_20260321_1430.md" {
		t.Fatalf("got %q, want spawn-request_telegram_20260321_1430.md", got)
	}
}

func TestPromptInspector_PrefixedFilename(t *testing.T) {
	dir := t.TempDir()
	inspector, err := NewPromptInspector(Options{DumpDir: dir, Task: "demo", Prefix: "spawn"})
	if err != nil {
		t.Fatalf("NewPromptInspector() error = %v", err)
	}
	defer func() { _ = inspector.Close() }()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
	if !strings.HasPrefix(entries[0].Name(), "spawn-prompt_") {
		t.Fatalf("filename %q should start with spawn-prompt_", entries[0].Name())
	}
}

func TestRequestInspector_PrefixedFilename(t *testing.T) {
	dir := t.TempDir()
	inspector, err := NewRequestInspector(Options{DumpDir: dir, Task: "demo", Prefix: "spawn"})
	if err != nil {
		t.Fatalf("NewRequestInspector() error = %v", err)
	}
	defer func() { _ = inspector.Close() }()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
	if !strings.HasPrefix(entries[0].Name(), "spawn-request_") {
		t.Fatalf("filename %q should start with spawn-request_", entries[0].Name())
	}
}

func mustContainAll(t *testing.T, text string, parts ...string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(text, part) {
			t.Fatalf("output missing %q\nfull output:\n%s", part, text)
		}
	}
}
