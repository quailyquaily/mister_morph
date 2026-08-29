package consolecmd

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
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
	usageClient, ok := client.(*llmstats.UsageClient)
	if !ok {
		t.Fatalf("CreateLLMClient() type = %T, want *llmstats.UsageClient", client)
	}
	if usageClient.Provider != "openai" || usageClient.APIBase != "https://example.test/v1" || usageClient.DefaultModel != "test-model" {
		t.Fatalf("chat usage metadata = %q/%q/%q", usageClient.Provider, usageClient.APIBase, usageClient.DefaultModel)
	}
	usageClient.Base = consoleUsagePathStubClient{}
	conversationKey := "console:captured-topic"
	ctx := topiccontext.WithScope(context.Background(), topiccontext.Scope{ConversationKey: conversationKey, Runtime: "console"})
	if _, err := usageClient.Chat(ctx, llm.Request{Model: "test-model", Scene: "console.loop"}); err != nil {
		t.Fatalf("UsageClient.Chat() error = %v", err)
	}
	appendConsoleUsagePathTestRecord(t, usageClient.Journal, "chat", "test-model")
	if err := usageClient.Close(); err != nil {
		t.Fatalf("UsageClient.Close() error = %v", err)
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

type consoleUsagePathStubClient struct{}

func (consoleUsagePathStubClient) Chat(context.Context, llm.Request) (llm.Result, error) {
	return llm.Result{Usage: llm.Usage{InputTokens: 13, TotalTokens: 13}}, nil
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
