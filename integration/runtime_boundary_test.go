package integration

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/contacts"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/quailyquaily/mistermorph/internal/topiccontext"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/memory"
	"github.com/spf13/viper"
)

func TestIntegrationRuntimePathsStayIsolatedFromGlobalViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	stateA := t.TempDir()
	cacheA := t.TempDir()
	stateB := t.TempDir()
	cacheB := t.TempDir()
	runtimeA := New(integrationRuntimeBoundaryConfig(stateA, cacheA, "model-alpha"))
	runtimeB := New(integrationRuntimeBoundaryConfig(stateB, cacheB, "model-beta"))

	viper.Set("file_state_dir", t.TempDir())
	viper.Set("file_cache_dir", t.TempDir())
	viper.Set("journal.dir_name", "global-journal")
	viper.Set("memory.dir_name", "global-memory")
	viper.Set("contacts.dir_name", "global-contacts")
	viper.Set("tasks.dir_name", "global-tasks")

	wantA := runtimepaths.FromReader(runtimeBoundaryPathReader(stateA, cacheA))
	wantB := runtimepaths.FromReader(runtimeBoundaryPathReader(stateB, cacheB))
	if !reflect.DeepEqual(runtimeA.snapshot().Paths, wantA) {
		t.Fatalf("runtimeA paths = %#v, want %#v", runtimeA.snapshot().Paths, wantA)
	}
	if !reflect.DeepEqual(runtimeB.snapshot().Paths, wantB) {
		t.Fatalf("runtimeB paths = %#v, want %#v", runtimeB.snapshot().Paths, wantB)
	}
	if reflect.DeepEqual(runtimeA.snapshot().Paths, runtimeB.snapshot().Paths) {
		t.Fatal("two Integration runtimes share RuntimePaths")
	}
	if !reflect.DeepEqual(runtimeA.sharedDependencies(runtimeA.snapshot()).RuntimePaths, wantA) {
		t.Fatal("runtimeA dependencies did not retain runtimeA paths")
	}
	if !reflect.DeepEqual(runtimeB.sharedDependencies(runtimeB.snapshot()).RuntimePaths, wantB) {
		t.Fatal("runtimeB dependencies did not retain runtimeB paths")
	}
}

func TestIntegrationUsageAndTopicContextStayInsideRuntimeSnapshot(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	stateA := t.TempDir()
	stateB := t.TempDir()
	globalState := t.TempDir()
	newBoundaryRuntime := func(stateDir, model string) *Runtime {
		return newRuntime(integrationRuntimeBoundaryConfig(stateDir, t.TempDir(), model), runtimeBuildDependencies{
			buildClient: func(llmconfig.ClientConfig, llmutil.RuntimeValues) (llm.Client, error) {
				return &stubIntegrationLLMClient{chatFn: func(context.Context, llm.Request) (llm.Result, error) {
					return llm.Result{Usage: llm.Usage{InputTokens: 21, TotalTokens: 21}}, nil
				}}, nil
			},
			buildImageClient: func(llmutil.RuntimeValues, *slog.Logger) (llm.ImageClient, error) {
				return &integrationLifecycleImageClient{}, nil
			},
		})
	}
	runtimeA := newBoundaryRuntime(stateA, "model-alpha")
	runtimeB := newBoundaryRuntime(stateB, "model-beta")

	viper.Set("file_state_dir", globalState)
	viper.Set("llm.provider", "global-provider")
	viper.Set("llm.model", "global-model")

	for _, tc := range []struct {
		name    string
		runtime *Runtime
		model   string
		state   string
	}{
		{name: "runtime A", runtime: runtimeA, model: "model-alpha", state: stateA},
		{name: "runtime B", runtime: runtimeB, model: "model-beta", state: stateB},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snap := tc.runtime.snapshot()
			deps := tc.runtime.sharedDependencies(snap)
			route, err := deps.ResolveLLMRoute(llmutil.RoutePurposeMainLoop)
			if err != nil {
				t.Fatalf("ResolveLLMRoute() error = %v", err)
			}
			client, err := deps.CreateLLMClient(route)
			if err != nil {
				t.Fatalf("CreateLLMClient() error = %v", err)
			}
			usageClient, ok := client.(*llmstats.UsageClient)
			if !ok {
				t.Fatalf("CreateLLMClient() type = %T, want *llmstats.UsageClient", client)
			}
			if usageClient.DefaultModel != tc.model {
				t.Fatalf("chat usage model = %q, want %q", usageClient.DefaultModel, tc.model)
			}
			ctx := topiccontext.WithScope(context.Background(), topiccontext.Scope{ConversationKey: "shared", Runtime: tc.name})
			if _, err := usageClient.Chat(ctx, llm.Request{Model: tc.model, Scene: "integration.loop"}); err != nil {
				t.Fatalf("UsageClient.Chat() error = %v", err)
			}
			if err := usageClient.Close(); err != nil {
				t.Fatalf("UsageClient.Close() error = %v", err)
			}

			item, found, err := topiccontext.NewStore(snap.Paths.TopicContextPath).Get("shared")
			if err != nil || !found || item.Model != tc.model || item.Runtime != tc.name {
				t.Fatalf("topic context = %#v, found=%v, err=%v", item, found, err)
			}

			imageClient, err := deps.CreateImageClient()
			if err != nil {
				t.Fatalf("CreateImageClient() error = %v", err)
			}
			imageUsageClient, ok := imageClient.(*llmstats.ImageUsageClient)
			if !ok {
				t.Fatalf("CreateImageClient() type = %T, want *llmstats.ImageUsageClient", imageClient)
			}
			if imageUsageClient.Provider != "openai" || imageUsageClient.DefaultModel != tc.model {
				t.Fatalf("image usage metadata = %q/%q, want openai/%s", imageUsageClient.Provider, imageUsageClient.DefaultModel, tc.model)
			}
			if _, err := imageUsageClient.GenerateImage(context.Background(), llm.ImageRequest{}); err != nil {
				t.Fatalf("GenerateImage() error = %v", err)
			}
			if err := imageUsageClient.Close(); err != nil {
				t.Fatalf("ImageUsageClient.Close() error = %v", err)
			}

			entries, err := os.ReadDir(snap.Paths.LLMUsageJournalDir)
			if err != nil || len(entries) == 0 {
				t.Fatalf("captured usage journal entries = %d, err=%v", len(entries), err)
			}
		})
	}

	globalPaths := runtimepaths.FromReader(viper.GetViper())
	if _, err := os.Stat(globalPaths.TopicContextPath); !os.IsNotExist(err) {
		t.Fatalf("global topic context path was used: %v", err)
	}
	if entries, err := os.ReadDir(globalPaths.LLMUsageJournalDir); err == nil && len(entries) != 0 {
		t.Fatalf("global usage journal entries = %d, want 0", len(entries))
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatalf("ReadDir(global usage journal) error = %v", err)
	}
}

func TestIntegrationRuntimeKeepsPersonaAndHostLoggerIsolated(t *testing.T) {
	originalLogger := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(originalLogger)
		viper.Reset()
	})

	hostLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	slog.SetDefault(hostLogger)

	stateA := t.TempDir()
	stateB := t.TempDir()
	globalState := t.TempDir()
	writeIntegrationPersona(t, stateA, "persona-alpha")
	writeIntegrationPersona(t, stateB, "persona-beta")
	writeIntegrationPersona(t, globalState, "persona-global")

	var prompts []string
	buildClient := func(llmconfig.ClientConfig, llmutil.RuntimeValues) (llm.Client, error) {
		return &stubIntegrationLLMClient{chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
			var systemPrompt string
			for _, message := range req.Messages {
				if message.Role == "system" {
					systemPrompt = message.Content
					break
				}
			}
			prompts = append(prompts, systemPrompt)
			return llm.Result{Text: `{"type":"final","output":"ok"}`}, nil
		}}, nil
	}

	runtimeA := newRuntime(integrationRuntimeBoundaryConfig(stateA, t.TempDir(), "model-alpha"), runtimeBuildDependencies{buildClient: buildClient})
	runtimeB := newRuntime(integrationRuntimeBoundaryConfig(stateB, t.TempDir(), "model-beta"), runtimeBuildDependencies{buildClient: buildClient})
	viper.Set("file_state_dir", globalState)

	if _, _, err := runtimeA.RunTask(context.Background(), "alpha", agent.RunOptions{}); err != nil {
		t.Fatalf("runtimeA.RunTask() error = %v", err)
	}
	if slog.Default() != hostLogger {
		t.Fatal("runtimeA changed the host process default logger")
	}
	if _, _, err := runtimeB.RunTask(context.Background(), "beta", agent.RunOptions{}); err != nil {
		t.Fatalf("runtimeB.RunTask() error = %v", err)
	}
	if slog.Default() != hostLogger {
		t.Fatal("runtimeB changed the host process default logger")
	}

	if len(prompts) != 2 {
		t.Fatalf("captured prompts = %d, want 2", len(prompts))
	}
	if !strings.Contains(prompts[0], "persona-alpha") || strings.Contains(prompts[0], "persona-beta") || strings.Contains(prompts[0], "persona-global") {
		t.Fatalf("runtimeA persona prompt is not isolated: %q", prompts[0])
	}
	if !strings.Contains(prompts[1], "persona-beta") || strings.Contains(prompts[1], "persona-alpha") || strings.Contains(prompts[1], "persona-global") {
		t.Fatalf("runtimeB persona prompt is not isolated: %q", prompts[1])
	}
}

func TestIntegrationRuntimeStateStoresStayIsolatedFromGlobalViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	stateA := t.TempDir()
	stateB := t.TempDir()
	globalState := t.TempDir()
	cfgA := integrationRuntimeBoundaryConfig(stateA, t.TempDir(), "model-alpha")
	cfgA.Set("tasks.persistence_targets", []string{"telegram"})
	cfgB := integrationRuntimeBoundaryConfig(stateB, t.TempDir(), "model-beta")
	cfgB.Set("tasks.persistence_targets", []string{"telegram"})
	runtimeA := New(cfgA)
	runtimeB := New(cfgB)

	viper.Set("file_state_dir", globalState)
	viper.Set("file_cache_dir", t.TempDir())
	viper.Set("journal.dir_name", "global-journal")
	viper.Set("memory.dir_name", "global-memory")
	viper.Set("contacts.dir_name", "global-contacts")
	viper.Set("tasks.dir_name", "global-tasks")

	depsA := runtimeA.sharedDependencies(runtimeA.snapshot())
	depsB := runtimeB.sharedDependencies(runtimeB.snapshot())
	commonA := runtimeA.telegramDependencies(runtimeA.snapshot()).CommonDependencies
	commonB := runtimeB.telegramDependencies(runtimeB.snapshot()).CommonDependencies
	taskA, err := daemonruntime.NewTaskViewForTarget("telegram", 10, daemonruntime.TaskViewConfig{
		PersistenceTargets: depsA.TaskPersistenceTargets,
		TasksDir:           depsA.RuntimePaths.TasksDir,
		JournalDir:         depsA.RuntimePaths.JournalDir,
		RotateMaxBytes:     depsA.TaskRotateMaxBytes,
	})
	if err != nil {
		t.Fatalf("runtimeA task store error = %v", err)
	}
	taskB, err := daemonruntime.NewTaskViewForTarget("telegram", 10, daemonruntime.TaskViewConfig{
		PersistenceTargets: depsB.TaskPersistenceTargets,
		TasksDir:           depsB.RuntimePaths.TasksDir,
		JournalDir:         depsB.RuntimePaths.JournalDir,
		RotateMaxBytes:     depsB.TaskRotateMaxBytes,
	})
	if err != nil {
		t.Fatalf("runtimeB task store error = %v", err)
	}
	if err := taskA.Upsert(daemonruntime.TaskInfo{ID: "shared-task", Task: "alpha", Status: daemonruntime.TaskDone}); err != nil {
		t.Fatalf("runtimeA task upsert error = %v", err)
	}
	if err := taskB.Upsert(daemonruntime.TaskInfo{ID: "shared-task", Task: "beta", Status: daemonruntime.TaskDone}); err != nil {
		t.Fatalf("runtimeB task upsert error = %v", err)
	}
	if got, ok := taskA.Get("shared-task"); !ok || got.Task != "alpha" {
		t.Fatalf("runtimeA task = %#v, exists=%v", got, ok)
	}
	if got, ok := taskB.Get("shared-task"); !ok || got.Task != "beta" {
		t.Fatalf("runtimeB task = %#v, exists=%v", got, ok)
	}

	contactsA := contacts.NewFileStore(commonA.RuntimePaths.ContactsDir)
	contactsB := contacts.NewFileStore(commonB.RuntimePaths.ContactsDir)
	if err := contactsA.PutContact(context.Background(), contacts.Contact{ContactID: "shared-contact", Kind: contacts.KindHuman, Channel: contacts.ChannelTelegram, ContactNickname: "alpha"}); err != nil {
		t.Fatalf("runtimeA contact write error = %v", err)
	}
	if err := contactsB.PutContact(context.Background(), contacts.Contact{ContactID: "shared-contact", Kind: contacts.KindHuman, Channel: contacts.ChannelTelegram, ContactNickname: "beta"}); err != nil {
		t.Fatalf("runtimeB contact write error = %v", err)
	}
	if got, ok, err := contactsA.GetContact(context.Background(), "shared-contact"); err != nil || !ok || got.ContactNickname != "alpha" {
		t.Fatalf("runtimeA contact = %#v, exists=%v, error=%v", got, ok, err)
	}
	if got, ok, err := contactsB.GetContact(context.Background(), "shared-contact"); err != nil || !ok || got.ContactNickname != "beta" {
		t.Fatalf("runtimeB contact = %#v, exists=%v, error=%v", got, ok, err)
	}

	workspaceA := workspace.NewStore(commonA.RuntimePaths.WorkspaceAttachmentsPath)
	workspaceB := workspace.NewStore(commonB.RuntimePaths.WorkspaceAttachmentsPath)
	if _, _, err := workspaceA.Set("shared-topic", workspace.Attachment{WorkspaceDir: "alpha-workspace"}); err != nil {
		t.Fatalf("runtimeA workspace write error = %v", err)
	}
	if _, _, err := workspaceB.Set("shared-topic", workspace.Attachment{WorkspaceDir: "beta-workspace"}); err != nil {
		t.Fatalf("runtimeB workspace write error = %v", err)
	}
	if got, ok, err := workspaceA.Get("shared-topic"); err != nil || !ok || got.WorkspaceDir != "alpha-workspace" {
		t.Fatalf("runtimeA workspace = %#v, exists=%v, error=%v", got, ok, err)
	}
	if got, ok, err := workspaceB.Get("shared-topic"); err != nil || !ok || got.WorkspaceDir != "beta-workspace" {
		t.Fatalf("runtimeB workspace = %#v, exists=%v, error=%v", got, ok, err)
	}

	now := time.Now().UTC()
	writeIntegrationMemory(t, commonA.RuntimePaths.MemoryDir, now, "alpha-memory")
	writeIntegrationMemory(t, commonB.RuntimePaths.MemoryDir, now, "beta-memory")
	memoryA, err := runtimecore.NewMemoryRuntime(commonA, runtimecore.MemoryRuntimeOptions{Enabled: true, ShortTermDays: 7})
	if err != nil {
		t.Fatalf("runtimeA memory error = %v", err)
	}
	defer memoryA.Cleanup()
	memoryB, err := runtimecore.NewMemoryRuntime(commonB, runtimecore.MemoryRuntimeOptions{Enabled: true, ShortTermDays: 7})
	if err != nil {
		t.Fatalf("runtimeB memory error = %v", err)
	}
	defer memoryB.Cleanup()
	injectionA, err := memoryA.Orchestrator.PrepareInjection(memoryruntime.PrepareInjectionRequest{SubjectID: "shared", RequestContext: memory.ContextPrivate, MaxItems: 10})
	if err != nil {
		t.Fatalf("runtimeA memory injection error = %v", err)
	}
	injectionB, err := memoryB.Orchestrator.PrepareInjection(memoryruntime.PrepareInjectionRequest{SubjectID: "shared", RequestContext: memory.ContextPrivate, MaxItems: 10})
	if err != nil {
		t.Fatalf("runtimeB memory injection error = %v", err)
	}
	if !strings.Contains(injectionA, "alpha-memory") || strings.Contains(injectionA, "beta-memory") {
		t.Fatalf("runtimeA memory is not isolated: %q", injectionA)
	}
	if !strings.Contains(injectionB, "beta-memory") || strings.Contains(injectionB, "alpha-memory") {
		t.Fatalf("runtimeB memory is not isolated: %q", injectionB)
	}

	if _, err := os.Stat(filepath.Join(globalState, "global-tasks")); !os.IsNotExist(err) {
		t.Fatalf("global task path was used: %v", err)
	}
	if _, err := os.Stat(filepath.Join(globalState, "global-contacts")); !os.IsNotExist(err) {
		t.Fatalf("global contacts path was used: %v", err)
	}
	if _, err := os.Stat(filepath.Join(globalState, "workspace_attachments.json")); !os.IsNotExist(err) {
		t.Fatalf("global workspace path was used: %v", err)
	}
}

func writeIntegrationMemory(t *testing.T, dir string, now time.Time, content string) {
	t.Helper()
	manager := memory.NewManager(dir, 7)
	if _, err := manager.UpdateShortTerm(now, memory.SessionDraft{SummaryItems: []string{content}}, memory.WriteMeta{SessionID: "shared"}); err != nil {
		t.Fatalf("write memory %q: %v", content, err)
	}
}

func integrationRuntimeBoundaryConfig(stateDir, cacheDir, model string) Config {
	cfg := DefaultConfig()
	cfg.Features.Guard = false
	cfg.Features.PlanTool = false
	cfg.Features.Skills = true
	cfg.Set("file_state_dir", stateDir)
	cfg.Set("file_cache_dir", cacheDir)
	cfg.Set("llm.provider", "openai")
	cfg.Set("llm.model", model)
	return cfg
}

func runtimeBoundaryPathReader(stateDir, cacheDir string) *viper.Viper {
	reader := viper.New()
	ApplyViperDefaults(reader)
	reader.Set("file_state_dir", stateDir)
	reader.Set("file_cache_dir", cacheDir)
	return reader
}

func writeIntegrationPersona(t *testing.T, stateDir, name string) {
	t.Helper()
	personaDir := filepath.Join(stateDir, "persona")
	if err := os.MkdirAll(personaDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(persona) error = %v", err)
	}
	identity := "name: " + name + "\ncreature: test\nvibe: calm\nemoji: test\n"
	if err := os.WriteFile(filepath.Join(personaDir, "identity.yaml"), []byte(identity), 0o600); err != nil {
		t.Fatalf("WriteFile(identity) error = %v", err)
	}
	soul := "# soul\n\n## Core Truths\n- " + name + "\n\n## Boundaries\n- safe\n\n## Vibe\n- calm\n"
	if err := os.WriteFile(filepath.Join(personaDir, "soul.md"), []byte(soul), 0o600); err != nil {
		t.Fatalf("WriteFile(soul) error = %v", err)
	}
}
