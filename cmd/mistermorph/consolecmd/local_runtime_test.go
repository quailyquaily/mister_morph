package consolecmd

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/contacts"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/viper"
)

func TestConsoleLocalRuntimeAuthTokenUsesServerTokenWhenConfigured(t *testing.T) {
	oldToken := viper.GetString("server.auth_token")
	t.Cleanup(func() {
		viper.Set("server.auth_token", oldToken)
	})
	viper.Set("server.auth_token", "dev-token")

	token, err := consoleLocalRuntimeAuthToken()
	if err != nil {
		t.Fatalf("consoleLocalRuntimeAuthToken() error = %v", err)
	}
	if token != "dev-token" {
		t.Fatalf("token = %q, want %q", token, "dev-token")
	}
}

func TestConsoleLocalRuntimeAuthTokenGeneratesInternalTokenWhenServerTokenMissing(t *testing.T) {
	oldToken := viper.GetString("server.auth_token")
	t.Cleanup(func() {
		viper.Set("server.auth_token", oldToken)
	})
	viper.Set("server.auth_token", "")

	token, err := consoleLocalRuntimeAuthToken()
	if err != nil {
		t.Fatalf("consoleLocalRuntimeAuthToken() error = %v", err)
	}
	if strings.TrimSpace(token) == "" {
		t.Fatal("token is empty")
	}
	if token == "dev-token" {
		t.Fatalf("token = %q, want generated token", token)
	}
}

func TestConsoleLLMCredentialsWarning_OpenAIWithoutAPIKey(t *testing.T) {
	route := llmutil.ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{
			Provider: "openai",
			Model:    "gpt-5.2",
		},
	}
	warning := consoleLLMCredentialsWarning(route)
	if !strings.Contains(warning, "MISTER_MORPH_LLM_API_KEY") {
		t.Fatalf("warning = %q, want env hint", warning)
	}
}

func TestConsoleLLMCredentialsWarning_OpenAIWithAPIKey(t *testing.T) {
	route := llmutil.ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{
			Provider: "openai",
			APIKey:   "dev-key",
			Model:    "gpt-5.2",
		},
	}
	if warning := consoleLLMCredentialsWarning(route); warning != "" {
		t.Fatalf("warning = %q, want empty", warning)
	}
}

func TestConsoleLLMCredentialsWarning_BedrockSkipsWarning(t *testing.T) {
	route := llmutil.ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{
			Provider: "bedrock",
			Model:    "anthropic.claude-3-7-sonnet",
		},
	}
	if warning := consoleLLMCredentialsWarning(route); warning != "" {
		t.Fatalf("warning = %q, want empty", warning)
	}
}

type stubConsoleLLMClient struct {
	requests []llm.Request
}

func (c *stubConsoleLLMClient) Chat(_ context.Context, req llm.Request) (llm.Result, error) {
	c.requests = append(c.requests, req)
	return llm.Result{Text: `{"type":"final","reasoning":"done","output":"ok"}`}, nil
}

func TestConsoleRunTaskUsesConfiguredClient(t *testing.T) {
	client := &stubConsoleLLMClient{}
	resolvedRoute := llmutil.ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{
			Provider: "openai",
			Model:    "gpt-5.2",
		},
	}
	execRuntime, err := taskruntime.Bootstrap(depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			return slog.Default(), nil
		},
		LogOptions: func() agent.LogOptions {
			return agent.LogOptions{}
		},
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return resolvedRoute, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
			return client, nil
		},
		Registry: func() *tools.Registry {
			return tools.NewRegistry()
		},
		RuntimeToolsConfig: toolsutil.RuntimeToolsRegisterConfig{},
		PromptSpec: func(_ context.Context, _ *slog.Logger, _ agent.LogOptions, _ string, _ llm.Client, _ string, _ []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), nil, nil
		},
	}, taskruntime.BootstrapOptions{
		AgentConfig:          agent.Config{MaxSteps: 3, ParseRetries: 1, ToolRepeatLimit: 3},
		DefaultModelFallback: "gpt-5.2",
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	rt := &consoleLocalRuntime{
		bundle: &consoleLocalRuntimeBundle{
			taskRuntime:  execRuntime,
			defaultModel: "gpt-5.2",
		},
	}

	final, runCtx, err := rt.runTask(context.Background(), buildConsoleConversationKey("test"), consoleLocalTaskJob{
		TaskID:  "task_console_1",
		TopicID: "test",
		Task:    "ping",
		Model:   "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("runTask() error = %v", err)
	}
	if final == nil {
		t.Fatal("final is nil")
	}
	if runCtx == nil {
		t.Fatal("runCtx is nil")
	}
	if len(client.requests) != 1 {
		t.Fatalf("client requests = %d, want 1", len(client.requests))
	}
	if client.requests[0].Model != "gpt-5.4" {
		t.Fatalf("request model = %q, want %q", client.requests[0].Model, "gpt-5.4")
	}
	if client.requests[0].Scene != "console.loop" {
		t.Fatalf("request scene = %q, want %q", client.requests[0].Scene, "console.loop")
	}
}

func TestEnqueueTaskCreatesTopicWhenMissingTopicID(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		HeartbeatTopicID: "_heartbeat",
		Persist:          false,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}

	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt := &consoleLocalRuntime{
		store: store,
		runner: runtimecore.NewConversationRunner[string, consoleLocalTaskJob](
			workerCtx,
			nil,
			1,
			func(context.Context, string, consoleLocalTaskJob) {},
		),
	}

	resp, err := rt.enqueueTask(
		context.Background(),
		"Create a topic title from this task prompt for the new conversation",
		"gpt-5.2",
		time.Minute,
		"",
		"",
		daemonruntime.TaskTrigger{Source: "ui", Event: "chat_submit"},
	)
	if err != nil {
		t.Fatalf("enqueueTask() error = %v", err)
	}
	if resp.ID == "" {
		t.Fatal("resp.ID is empty")
	}
	if resp.TopicID == "" {
		t.Fatal("resp.TopicID is empty")
	}
	if resp.TopicID == daemonruntime.ConsoleDefaultTopicID {
		t.Fatalf("resp.TopicID = %q, want generated topic id", resp.TopicID)
	}
	topicUUID, err := uuid.Parse(resp.TopicID)
	if err != nil {
		t.Fatalf("uuid.Parse(resp.TopicID) error = %v", err)
	}
	if topicUUID.Version() != uuid.Version(7) {
		t.Fatalf("topic UUID version = %d, want 7", topicUUID.Version())
	}

	task, ok := store.Get(resp.ID)
	if !ok || task == nil {
		t.Fatalf("store.Get(%q) missing task", resp.ID)
	}
	if task.TopicID != resp.TopicID {
		t.Fatalf("task.TopicID = %q, want %q", task.TopicID, resp.TopicID)
	}

	topics := store.ListTopics()
	if len(topics) < 1 {
		t.Fatalf("len(topics) = %d, want at least 1", len(topics))
	}
	foundGenerated := false
	for _, topic := range topics {
		if topic.ID != resp.TopicID {
			continue
		}
		foundGenerated = true
		if strings.TrimSpace(topic.Title) == "" {
			t.Fatalf("generated topic %q has empty title", topic.ID)
		}
	}
	if !foundGenerated {
		t.Fatalf("generated topic %q not found in topic list", resp.TopicID)
	}
}

func TestSubmitTaskUsesConsoleBus(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		HeartbeatTopicID: "_heartbeat",
		Persist:          false,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	inprocBus, err := busruntime.StartInproc(busruntime.BootstrapOptions{
		MaxInFlight: 8,
		Logger:      logger,
		Component:   "console_test",
	})
	if err != nil {
		t.Fatalf("StartInproc() error = %v", err)
	}
	defer func() { _ = inprocBus.Close() }()

	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gotJob := make(chan consoleLocalTaskJob, 1)
	rt := &consoleLocalRuntime{
		logger:         logger,
		store:          store,
		bus:            inprocBus,
		contactsSvc:    contacts.NewService(contacts.NewFileStore(t.TempDir())),
		defaultTimeout: time.Minute,
		runner: runtimecore.NewConversationRunner[string, consoleLocalTaskJob](
			workerCtx,
			make(chan struct{}, 1),
			1,
			func(_ context.Context, _ string, job consoleLocalTaskJob) {
				gotJob <- job
			},
		),
	}
	if err := inprocBus.Subscribe(busruntime.TopicChatMessage, rt.handleConsoleBusMessage); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	resp, err := rt.submitTask(context.Background(), daemonruntime.SubmitTaskRequest{
		Task: "hello from console bus",
	})
	if err != nil {
		t.Fatalf("submitTask() error = %v", err)
	}
	if resp.ID == "" {
		t.Fatal("resp.ID is empty")
	}
	if resp.TopicID == "" {
		t.Fatal("resp.TopicID is empty")
	}

	select {
	case job := <-gotJob:
		if job.TaskID != resp.ID {
			t.Fatalf("job.TaskID = %q, want %q", job.TaskID, resp.ID)
		}
		if job.TopicID != resp.TopicID {
			t.Fatalf("job.TopicID = %q, want %q", job.TopicID, resp.TopicID)
		}
		if job.ConversationKey != buildConsoleConversationKey(resp.TopicID) {
			t.Fatalf("job.ConversationKey = %q, want %q", job.ConversationKey, buildConsoleConversationKey(resp.TopicID))
		}
		if job.Trigger.Source != "ui" || job.Trigger.Event != "chat_submit" {
			t.Fatalf("job.Trigger = %+v, want ui/chat_submit", job.Trigger)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for console bus job")
	}

	task, ok := store.Get(resp.ID)
	if !ok || task == nil {
		t.Fatalf("store.Get(%q) missing task", resp.ID)
	}
	if task.Status != daemonruntime.TaskQueued {
		t.Fatalf("task.Status = %q, want %q", task.Status, daemonruntime.TaskQueued)
	}

	contact, ok, err := rt.contactsSvc.GetContact(context.Background(), "console:user")
	if err != nil {
		t.Fatalf("GetContact(console:user) error = %v", err)
	}
	if !ok {
		t.Fatalf("GetContact(console:user) expected ok=true")
	}
	if contact.Channel != contacts.ChannelConsole {
		t.Fatalf("contact.Channel = %q, want %q", contact.Channel, contacts.ChannelConsole)
	}
}

func TestSanitizeConsoleTopicTitle(t *testing.T) {
	got := sanitizeConsoleTopicTitle("  \"Quarterly sync status and follow-up items.\"  ")
	want := "Quarterly sync status and follow-up items"
	if got != want {
		t.Fatalf("sanitizeConsoleTopicTitle() = %q, want %q", got, want)
	}
}
