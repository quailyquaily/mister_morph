package consolecmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/quailyquaily/mistermorph/internal/testhttp"
	"github.com/quailyquaily/mistermorph/internal/topiccontext"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/spf13/viper"
)

func TestConsoleLocalRuntimeUsageStatsUseCapturedJournalPath(t *testing.T) {
	globalStateDir := t.TempDir()
	capturedStateDir := t.TempDir()
	viper.Set("file_state_dir", globalStateDir)
	t.Cleanup(viper.Reset)

	reader := consoleUsagePathTestReader(capturedStateDir)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	snapshot, err := buildConsoleLocalRuntimeConfigSnapshot(logger, nil, reader)
	if err != nil {
		t.Fatalf("buildConsoleLocalRuntimeConfigSnapshot() error = %v", err)
	}

	assertConsoleUsageJournalPath(t, snapshot.commonDeps, reader, runtimepaths.FromReader(reader), runtimepaths.FromReader(viper.GetViper()))
}

func TestManagedRuntimeUsageStatsUseCapturedJournalPath(t *testing.T) {
	globalStateDir := t.TempDir()
	capturedStateDir := t.TempDir()
	viper.Set("file_state_dir", globalStateDir)
	t.Cleanup(viper.Reset)

	reader := consoleUsagePathTestReader(capturedStateDir)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps, cleanup, err := buildManagedRuntimeDepsFromReader(logger, reader)
	if err != nil {
		t.Fatalf("buildManagedRuntimeDepsFromReader() error = %v", err)
	}
	t.Cleanup(cleanup)

	assertConsoleUsageJournalPath(t, deps, reader, runtimepaths.FromReader(reader), runtimepaths.FromReader(viper.GetViper()))
}

func TestConsoleTopicMetadataUsesCapturedTopicContextPath(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	capturedPath := filepath.Join(t.TempDir(), "topic_context.json")
	globalState := t.TempDir()
	viper.Set("file_state_dir", globalState)
	conversationKey := buildConsoleConversationKey("topic-a")
	if err := topiccontext.NewStore(capturedPath).UpdateFromSample(topiccontext.Scope{
		ConversationKey: conversationKey,
		Runtime:         "captured",
	}, topiccontext.UsageSample{
		Model:       "captured-model",
		InputTokens: 10,
		UpdatedAt:   time.Now(),
	}); err != nil {
		t.Fatalf("UpdateFromSample(captured) error = %v", err)
	}
	globalPath := runtimepaths.FromReader(viper.GetViper()).TopicContextPath
	if err := topiccontext.NewStore(globalPath).UpdateFromSample(topiccontext.Scope{
		ConversationKey: conversationKey,
		Runtime:         "global",
	}, topiccontext.UsageSample{
		Model:       "global-model",
		InputTokens: 20,
		UpdatedAt:   time.Now(),
	}); err != nil {
		t.Fatalf("UpdateFromSample(global) error = %v", err)
	}

	runtime := &consoleLocalRuntime{
		workspaceStore: workspace.NewStore(filepath.Join(t.TempDir(), "workspace_attachments.json")),
	}
	metadata, err := runtime.topicMetadataForTopic(context.Background(), "topic-a", capturedPath, "")
	if err != nil {
		t.Fatalf("topicMetadataForTopic() error = %v", err)
	}
	if !metadata.Context.Available || metadata.Context.Model != "captured-model" {
		t.Fatalf("metadata context = %#v, want captured-model", metadata.Context)
	}
}

func consoleUsagePathTestReader(stateDir string) *viper.Viper {
	reader := viper.New()
	configdefaults.Apply(reader)
	reader.Set("file_state_dir", stateDir)
	reader.Set("file_cache_dir", stateDir)
	reader.Set("llm.provider", "openai")
	reader.Set("llm.endpoint", "https://example.test/v1")
	reader.Set("llm.api_key", "test-key")
	reader.Set("llm.model", "test-model")
	reader.Set("llm.image.provider", "openai")
	reader.Set("llm.image.endpoint", "https://example.test/v1")
	reader.Set("llm.image.api_key", "test-key")
	reader.Set("llm.image.model", "test-image-model")
	reader.Set("guard.enabled", false)
	return reader
}

func assertConsoleUsageJournalPath(
	t *testing.T,
	deps depsutil.CommonDependencies,
	reader *viper.Viper,
	capturedPaths runtimepaths.Paths,
	globalPaths runtimepaths.Paths,
) {
	t.Helper()
	testhttp.WithDefaultTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.String() != "https://example.test/v1/chat/completions" {
			t.Errorf("request URL = %q", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":13,"total_tokens":13}}`)
	}))
	values, err := llmutil.RuntimeValuesFromReader(reader)
	if err != nil {
		t.Fatalf("RuntimeValuesFromReader() error = %v", err)
	}
	route, err := llmutil.ResolveRoute(values, llmutil.RoutePurposeMainLoop)
	if err != nil {
		t.Fatalf("ResolveRoute() error = %v", err)
	}
	client, err := deps.CreateLLMClient(route)
	if err != nil {
		t.Fatalf("CreateLLMClient() error = %v", err)
	}
	conversationKey := "console:captured-topic"
	ctx := topiccontext.WithScope(context.Background(), topiccontext.Scope{ConversationKey: conversationKey, Runtime: "console"})
	if _, err := client.Chat(ctx, llm.Request{Scene: "console.loop", Messages: []llm.Message{{Role: "user", Content: "test"}}}); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	closer, ok := client.(io.Closer)
	if !ok {
		t.Fatalf("CreateLLMClient() type %T does not implement io.Closer", client)
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	imageClient, err := deps.CreateImageClient()
	if err != nil {
		t.Fatalf("CreateImageClient() error = %v", err)
	}
	imageUsageClient, ok := imageClient.(*llmstats.ImageUsageClient)
	if !ok {
		t.Fatalf("CreateImageClient() type = %T, want *llmstats.ImageUsageClient", imageClient)
	}
	if imageUsageClient.Provider != "openai" || imageUsageClient.APIBase != "https://example.test/v1" || imageUsageClient.DefaultModel != "test-image-model" {
		t.Fatalf("image usage metadata = %q/%q/%q", imageUsageClient.Provider, imageUsageClient.APIBase, imageUsageClient.DefaultModel)
	}
	appendConsoleUsagePathTestRecord(t, imageUsageClient.Journal, "image_generate", "test-image-model")
	if err := imageUsageClient.Close(); err != nil {
		t.Fatalf("ImageUsageClient.Close() error = %v", err)
	}

	entries, err := os.ReadDir(capturedPaths.LLMUsageJournalDir)
	if err != nil {
		t.Fatalf("ReadDir(captured journal) error = %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("captured usage journal is empty")
	}
	foundChat := false
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(capturedPaths.LLMUsageJournalDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		for {
			var record llmstats.RequestRecord
			if err := decoder.Decode(&record); err == io.EOF {
				break
			} else if err != nil {
				t.Fatal(err)
			}
			if record.Operation != "chat" {
				continue
			}
			foundChat = true
			if record.Provider != "openai" || record.APIBase != "https://example.test/v1" || record.Model != "test-model" || record.InputTokens != 13 {
				t.Fatalf("chat usage record = %+v", record)
			}
		}
	}
	if !foundChat {
		t.Fatal("captured chat usage record is missing")
	}
	globalEntries, err := os.ReadDir(globalPaths.LLMUsageJournalDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("ReadDir(global journal) error = %v", err)
	}
	if len(globalEntries) != 0 {
		t.Fatalf("global usage journal entries = %d, want 0", len(globalEntries))
	}
	topicItem, ok, err := topiccontext.NewStore(capturedPaths.TopicContextPath).Get(conversationKey)
	if err != nil || !ok || topicItem.Model != "test-model" || topicItem.Runtime != "console" {
		t.Fatalf("captured topic context = %#v, ok=%v, err=%v", topicItem, ok, err)
	}
	if _, err := os.Stat(globalPaths.TopicContextPath); !os.IsNotExist(err) {
		t.Fatalf("global topic context path was used: %v", err)
	}
}

func appendConsoleUsagePathTestRecord(t *testing.T, journal *llmstats.Journal, operation, model string) {
	t.Helper()
	if journal == nil {
		t.Fatal("usage journal is nil")
	}
	_, err := journal.Append(llmstats.RequestRecord{
		TS:        time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		Provider:  "openai",
		APIBase:   "https://example.test/v1",
		Model:     model,
		Operation: operation,
	})
	if err != nil {
		t.Fatalf("Journal.Append() error = %v", err)
	}
}
