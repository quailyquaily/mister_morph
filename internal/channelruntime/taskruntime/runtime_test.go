package taskruntime

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/contextcheckpoint"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type stubTaskRuntimeClient struct {
	requests []llm.Request
	result   llm.Result
}

type stubTaskRuntimeImageClient struct{}

func (c *stubTaskRuntimeClient) Chat(_ context.Context, req llm.Request) (llm.Result, error) {
	c.requests = append(c.requests, req)
	if c.result.Text == "" && c.result.JSON == nil && len(c.result.ToolCalls) == 0 && len(c.result.Parts) == 0 {
		return llm.Result{Text: `{"type":"final","output":"ok"}`}, nil
	}
	return c.result, nil
}

func (stubTaskRuntimeImageClient) GenerateImage(context.Context, llm.ImageRequest) (llm.ImageResult, error) {
	return llm.ImageResult{}, nil
}

func (stubTaskRuntimeImageClient) EditImage(context.Context, llm.ImageEditRequest) (llm.ImageResult, error) {
	return llm.ImageResult{}, nil
}

type approvalResumeClient struct {
	requests []llm.Request
}

func (c *approvalResumeClient) Chat(_ context.Context, req llm.Request) (llm.Result, error) {
	c.requests = append(c.requests, req)
	if len(c.requests) == 1 {
		return llm.Result{ToolCalls: []llm.ToolCall{
			{
				ID:   "call_bash",
				Name: "bash",
				Arguments: map[string]any{
					"cmd": "echo ok",
				},
			},
		}}, nil
	}
	return llm.Result{Text: `{"type":"final","output":"done"}`}, nil
}

type approvalResumeTool struct {
	calls int
}

func (t *approvalResumeTool) Name() string            { return "bash" }
func (t *approvalResumeTool) Description() string     { return "test bash" }
func (t *approvalResumeTool) ParameterSchema() string { return "{}" }
func (t *approvalResumeTool) Execute(context.Context, map[string]any) (string, error) {
	t.calls++
	return "tool ok", nil
}

type approvalResumeCompactionClient struct {
	t        *testing.T
	requests []llm.Request
}

func TestRunTreatsCtxCompactAsControlTask(t *testing.T) {
	client := &stubTaskRuntimeClient{result: llm.Result{
		Text:  `{"summary":"older conversation","user_intent":["continue the conversation"],"references":{"files":[],"directories":[],"urls":[]},"progress":{"completed":[],"in_progress":[],"pending":[]},"intermediate_results":[]}`,
		Usage: llm.Usage{InputTokens: 120, OutputTokens: 40, TotalTokens: 160},
	}}
	route := llmutil.ResolvedRoute{ClientConfig: llmconfig.ClientConfig{
		Provider:            "test",
		Model:               "test-model",
		ContextWindowTokens: 1000,
	}}
	rt, err := Bootstrap(depsutil.CommonDependencies{
		Logger:          func() (*slog.Logger, error) { return slog.Default(), nil },
		LogOptions:      func() agent.LogOptions { return agent.LogOptions{} },
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) { return route, nil },
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) { return client, nil },
		Registry:        func() *tools.Registry { return tools.NewRegistry() },
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), nil, nil
		},
	}, BootstrapOptions{AgentConfig: agent.Config{MaxSteps: 2}})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	store, err := contextcheckpoint.NewFileStore(t.TempDir(), "manual-compact")
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	memoryRecords := 0
	result, err := rt.Run(context.Background(), RunRequest{
		Task:                   "/ctx compact",
		Scene:                  "test.loop",
		History:                []llm.Message{{Role: "user", Content: "old one"}, {Role: "assistant", Content: "old two"}},
		HistoryBoundaries:      []string{"old-1", "old-2"},
		CurrentMessageBoundary: "manual-command",
		ContextCheckpointStore: store,
		Memory: MemoryHooks{
			SubjectID: "test",
			Record: func(*agent.Final, string) error {
				memoryRecords++
				return nil
			},
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Final == nil || result.Final.Output != "Context compacted." {
		t.Fatalf("final = %#v", result.Final)
	}
	if len(client.requests) != 1 || client.requests[0].Scene != "test.context_compact" {
		t.Fatalf("requests = %#v", client.requests)
	}
	if memoryRecords != 0 {
		t.Fatalf("memory records = %d, want 0", memoryRecords)
	}
	checkpoint, found, err := store.Load(context.Background())
	if err != nil || !found {
		t.Fatalf("Load() found = %v, error = %v", found, err)
	}
	if checkpoint.CoveredThrough != "old-2" {
		t.Fatalf("covered through = %q, want old-2", checkpoint.CoveredThrough)
	}
}

func (c *approvalResumeCompactionClient) Chat(_ context.Context, req llm.Request) (llm.Result, error) {
	c.requests = append(c.requests, req)
	switch len(c.requests) {
	case 1:
		return llm.Result{
			ToolCalls: []llm.ToolCall{{
				ID:        "call_bash",
				Name:      "bash",
				Arguments: map[string]any{"cmd": "echo ok"},
			}},
			Usage: llm.Usage{InputTokens: 800, OutputTokens: 10, TotalTokens: 810},
		}, nil
	case 2:
		if req.Scene != "test.context_compact" {
			c.t.Fatalf("second request scene = %q, want context compaction", req.Scene)
		}
		return llm.Result{Text: `{"summary":"paused run","user_intent":["run bash"],"references":{"files":[],"directories":[],"urls":[]},"progress":{"completed":[],"in_progress":["resume approved tool"],"pending":[]},"intermediate_results":[]}`}, nil
	case 3:
		return llm.Result{Text: `{"type":"final","output":"done"}`}, nil
	default:
		c.t.Fatalf("unexpected request count %d", len(c.requests))
		return llm.Result{}, nil
	}
}

func TestResumeContinuesApprovedPendingTool(t *testing.T) {
	client := &approvalResumeClient{}
	tool := &approvalResumeTool{}
	approvalStore, err := guard.NewFileApprovalStore(
		filepath.Join(t.TempDir(), "guard", "guard_approvals.json"),
		filepath.Join(t.TempDir(), ".locks"),
	)
	if err != nil {
		t.Fatalf("NewFileApprovalStore() error = %v", err)
	}
	g := guard.New(guard.Config{
		Enabled: true,
		Approvals: guard.ApprovalsConfig{
			Enabled: true,
		},
	}, nil, approvalStore)
	route := llmutil.ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{
			Provider: "test",
			Model:    "test-model",
		},
	}
	rt, err := Bootstrap(depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			return slog.Default(), nil
		},
		LogOptions: func() agent.LogOptions {
			return agent.LogOptions{}
		},
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return route, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
			return client, nil
		},
		Registry: func() *tools.Registry {
			reg := tools.NewRegistry()
			reg.Register(tool)
			return reg
		},
		Guard: func(*slog.Logger) *guard.Guard {
			return g
		},
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), nil, nil
		},
	}, BootstrapOptions{
		AgentConfig: agent.Config{MaxSteps: 4, ParseRetries: 0, ToolRepeatLimit: 2},
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	req := RunRequest{Task: "run bash", Scene: "test.loop"}
	pending, err := rt.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	out, ok := pending.Final.Output.(agent.PendingOutput)
	if !ok {
		t.Fatalf("pending output = %#v, want agent.PendingOutput", pending.Final.Output)
	}
	if strings.TrimSpace(out.ApprovalRequestID) == "" {
		t.Fatal("ApprovalRequestID is empty")
	}
	if tool.calls != 0 {
		t.Fatalf("tool calls = %d, want 0 before approval", tool.calls)
	}
	if err := g.ResolveApproval(context.Background(), out.ApprovalRequestID, guard.ApprovalApproved, "tester", "ok"); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}

	resumed, err := rt.Resume(context.Background(), out.ApprovalRequestID, req)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if strings.TrimSpace(depsutil.FormatFinalOutput(resumed.Final)) != "done" {
		t.Fatalf("resumed final = %#v, want done", resumed.Final)
	}
	if tool.calls != 1 {
		t.Fatalf("tool calls = %d, want 1 after resume", tool.calls)
	}
	if len(client.requests) != 2 {
		t.Fatalf("client requests = %d, want 2", len(client.requests))
	}
}

func TestResumePreservesContextCompactionState(t *testing.T) {
	client := &approvalResumeCompactionClient{t: t}
	tool := &approvalResumeTool{}
	root := t.TempDir()
	approvalStore, err := guard.NewFileApprovalStore(
		filepath.Join(root, "guard", "guard_approvals.json"),
		filepath.Join(root, ".locks"),
	)
	if err != nil {
		t.Fatalf("NewFileApprovalStore() error = %v", err)
	}
	g := guard.New(guard.Config{
		Enabled: true,
		Approvals: guard.ApprovalsConfig{
			Enabled: true,
		},
	}, nil, approvalStore)
	route := llmutil.ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{
			Provider:            "test",
			Model:               "test-model",
			ContextWindowTokens: 1000,
		},
	}
	rt, err := Bootstrap(depsutil.CommonDependencies{
		Logger:     func() (*slog.Logger, error) { return slog.Default(), nil },
		LogOptions: func() agent.LogOptions { return agent.LogOptions{} },
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return route, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
			return client, nil
		},
		Registry: func() *tools.Registry {
			reg := tools.NewRegistry()
			reg.Register(tool)
			return reg
		},
		Guard: func(*slog.Logger) *guard.Guard { return g },
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), nil, nil
		},
	}, BootstrapOptions{
		AgentConfig: agent.Config{
			MaxSteps:        4,
			ParseRetries:    0,
			ToolRepeatLimit: 2,
			ContextCompaction: agent.ContextCompactionConfig{
				TriggerRatio:        0.80,
				OutputReserveTokens: 100,
			},
		},
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	checkpointStore, err := contextcheckpoint.NewFileStore(root, "resume-conversation")
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	req := RunRequest{
		Task:                   "run bash",
		Scene:                  "test.loop",
		ContextCheckpointStore: checkpointStore,
		CurrentMessageBoundary: "current-message",
	}
	pending, err := rt.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	out, ok := pending.Final.Output.(agent.PendingOutput)
	if !ok {
		t.Fatalf("pending output = %#v, want agent.PendingOutput", pending.Final.Output)
	}
	if err := g.ResolveApproval(context.Background(), out.ApprovalRequestID, guard.ApprovalApproved, "tester", "ok"); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}

	resumed, err := rt.Resume(context.Background(), out.ApprovalRequestID, req)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if strings.TrimSpace(depsutil.FormatFinalOutput(resumed.Final)) != "done" {
		t.Fatalf("resumed final = %#v, want done", resumed.Final)
	}
	if len(client.requests) != 3 {
		t.Fatalf("client requests = %d, want 3", len(client.requests))
	}
	checkpoint, found, err := checkpointStore.Load(context.Background())
	if err != nil || !found {
		t.Fatalf("Load() found = %v, error = %v", found, err)
	}
	if checkpoint.CoveredThrough != "current-message" {
		t.Fatalf("covered through = %q, want current-message", checkpoint.CoveredThrough)
	}
}

func TestBootstrapReusesMainClientForSamePlanProfile(t *testing.T) {
	var createCalls int
	route := llmutil.ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{
			Provider: "openai",
			Model:    "gpt-5.2",
		},
	}
	rt, err := Bootstrap(depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			return slog.Default(), nil
		},
		LogOptions: func() agent.LogOptions {
			return agent.LogOptions{}
		},
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return route, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
			createCalls++
			return &stubTaskRuntimeClient{}, nil
		},
		Registry: func() *tools.Registry {
			return tools.NewRegistry()
		},
		PromptSpec: func(_ context.Context, _ *slog.Logger, _ agent.LogOptions, _ string, _ llm.Client, _ string, _ []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), nil, nil
		},
	}, BootstrapOptions{
		AgentConfig: agent.Config{MaxSteps: 2, ParseRetries: 0, ToolRepeatLimit: 2},
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if createCalls != 1 {
		t.Fatalf("CreateLLMClient calls = %d, want 1", createCalls)
	}
	if rt.BootstrapMainClient != rt.PlanClient {
		t.Fatal("PlanClient should reuse BootstrapMainClient for same profile")
	}
}

func TestRunAppliesPromptAugmentAndMemoryHooks(t *testing.T) {
	client := &stubTaskRuntimeClient{}
	route := llmutil.ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{
			Provider: "openai",
			Model:    "gpt-5.2",
		},
	}
	rt, err := Bootstrap(depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			return slog.Default(), nil
		},
		LogOptions: func() agent.LogOptions {
			return agent.LogOptions{}
		},
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return route, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
			return client, nil
		},
		Registry: func() *tools.Registry {
			return tools.NewRegistry()
		},
		PromptSpec: func(_ context.Context, _ *slog.Logger, _ agent.LogOptions, _ string, _ llm.Client, _ string, _ []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), nil, nil
		},
		PromptAugment: func(spec *agent.PromptSpec, _ *tools.Registry) {
			spec.Blocks = append(spec.Blocks, agent.PromptBlock{Content: "integration block"})
		},
	}, BootstrapOptions{
		AgentConfig: agent.Config{MaxSteps: 2, ParseRetries: 0, ToolRepeatLimit: 2},
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	var (
		prepareCalls int
		recordCalls  int
		notifyCalls  int
	)
	result, err := rt.Run(context.Background(), RunRequest{
		Task:  "ping",
		Model: "gpt-5.4",
		Scene: "test.loop",
		PromptAugment: func(spec *agent.PromptSpec, _ *tools.Registry) {
			spec.Blocks = append(spec.Blocks, agent.PromptBlock{Content: "channel block"})
		},
		Memory: MemoryHooks{
			Source:            "test",
			SubjectID:         "test:main",
			InjectionEnabled:  true,
			InjectionMaxItems: 3,
			PrepareInjection: func(maxItems int) (string, error) {
				prepareCalls++
				if maxItems != 3 {
					t.Fatalf("PrepareInjection maxItems = %d, want 3", maxItems)
				}
				return "memory snapshot", nil
			},
			ShouldRecord: func(*agent.Final) bool {
				return true
			},
			Record: func(_ *agent.Final, finalOutput string) error {
				recordCalls++
				if strings.TrimSpace(finalOutput) != "ok" {
					t.Fatalf("finalOutput = %q, want ok", finalOutput)
				}
				return nil
			},
			NotifyRecorded: func() {
				notifyCalls++
			},
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Final == nil {
		t.Fatal("Run() final is nil")
	}
	if prepareCalls != 1 {
		t.Fatalf("PrepareInjection calls = %d, want 1", prepareCalls)
	}
	if recordCalls != 1 {
		t.Fatalf("Record calls = %d, want 1", recordCalls)
	}
	if notifyCalls != 1 {
		t.Fatalf("NotifyRecorded calls = %d, want 1", notifyCalls)
	}
	if len(client.requests) != 1 {
		t.Fatalf("client requests = %d, want 1", len(client.requests))
	}
	if client.requests[0].Model != "gpt-5.4" {
		t.Fatalf("request model = %q, want gpt-5.4", client.requests[0].Model)
	}
	if client.requests[0].Scene != "test.loop" {
		t.Fatalf("request scene = %q, want test.loop", client.requests[0].Scene)
	}
	msgs := client.requests[0].Messages
	if len(msgs) != 4 {
		t.Fatalf("messages len = %d, want 4", len(msgs))
	}
	systemPrompt := msgs[0].Content
	if !strings.Contains(systemPrompt, "channel block") {
		t.Fatalf("system prompt missing prompt augment block: %q", systemPrompt)
	}
	if strings.Contains(systemPrompt, "memory snapshot") {
		t.Fatalf("system prompt should not contain memory snapshot: %q", systemPrompt)
	}
	if !strings.Contains(systemPrompt, "integration block") {
		t.Fatalf("system prompt missing common prompt augment block: %q", systemPrompt)
	}
	if !strings.Contains(msgs[1].Content, "mister_morph_meta") {
		t.Fatalf("messages[1] = %q, want injected meta", msgs[1].Content)
	}
	if msgs[2].Role != "user" || !strings.Contains(msgs[2].Content, "[[ Runtime Memory ]]") {
		t.Fatalf("messages[2] = %#v, want runtime memory message", msgs[2])
	}
	if !strings.Contains(msgs[2].Content, "memory snapshot") {
		t.Fatalf("messages[2] = %q, want memory snapshot", msgs[2].Content)
	}
	if msgs[3].Content != "ping" {
		t.Fatalf("messages[3] = %q, want task", msgs[3].Content)
	}
}

func TestRunThinkCommandUsesThinkRouteAndXHighReasoning(t *testing.T) {
	client := &stubTaskRuntimeClient{}
	mainRoute := llmutil.ResolvedRoute{
		Purpose: llmutil.RoutePurposeMainLoop,
		Profile: llmutil.RouteProfileDefault,
		ClientConfig: llmconfig.ClientConfig{
			Provider: "openai",
			Model:    "main-model",
		},
	}
	thinkRoute := llmutil.ResolvedRoute{
		Purpose: llmutil.RoutePurposeThink,
		Profile: "reasoning",
		Values: llmutil.RuntimeValues{
			ReasoningEffortRaw: "low",
		},
		ClientConfig: llmconfig.ClientConfig{
			Provider: "openai",
			Model:    "think-model",
		},
	}
	var created []llmutil.ResolvedRoute
	rt, err := Bootstrap(depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			return slog.Default(), nil
		},
		LogOptions: func() agent.LogOptions {
			return agent.LogOptions{}
		},
		ResolveLLMRoute: func(purpose string) (llmutil.ResolvedRoute, error) {
			if purpose == llmutil.RoutePurposeThink {
				return thinkRoute, nil
			}
			return mainRoute, nil
		},
		CreateLLMClient: func(route llmutil.ResolvedRoute) (llm.Client, error) {
			created = append(created, route)
			return client, nil
		},
		Registry: func() *tools.Registry {
			return tools.NewRegistry()
		},
		PromptSpec: func(_ context.Context, _ *slog.Logger, _ agent.LogOptions, _ string, _ llm.Client, _ string, _ []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), nil, nil
		},
	}, BootstrapOptions{
		AgentConfig: agent.Config{MaxSteps: 2, ParseRetries: 0, ToolRepeatLimit: 2},
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	_, err = rt.Run(context.Background(), RunRequest{
		Task:  "/think solve hard problem",
		Model: "main-model",
		Scene: "test.loop",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(created) < 2 {
		t.Fatalf("created routes = %d, want at least bootstrap and run", len(created))
	}
	runRoute := created[len(created)-1]
	if runRoute.Purpose != llmutil.RoutePurposeThink {
		t.Fatalf("run route purpose = %q, want think", runRoute.Purpose)
	}
	if runRoute.Values.ReasoningEffortRaw != llmutil.ReasoningEffortXHigh {
		t.Fatalf("run reasoning effort = %q, want xhigh", runRoute.Values.ReasoningEffortRaw)
	}
	if len(client.requests) != 1 {
		t.Fatalf("client requests = %d, want 1", len(client.requests))
	}
	if client.requests[0].Model != "think-model" {
		t.Fatalf("request model = %q, want think-model", client.requests[0].Model)
	}
	msgs := client.requests[0].Messages
	if got := msgs[len(msgs)-1].Content; got != "solve hard problem" {
		t.Fatalf("task message = %q, want stripped think task", got)
	}
}

func TestImageToolRegistrationTaskIncludesCurrentMessageText(t *testing.T) {
	got := imageToolRegistrationTask("console", &llm.Message{
		Role:    "user",
		Content: `{"current_message":{"text":"你试试画一张日出。"}}`,
		Parts: []llm.Part{
			{Type: llm.PartTypeText, Text: "再亮一点"},
			{Type: llm.PartTypeImageBase64, DataBase64: "ignored"},
		},
	})
	if !strings.Contains(got, "console") {
		t.Fatalf("registration task = %q, want task text", got)
	}
	if !strings.Contains(got, "画一张日出") {
		t.Fatalf("registration task = %q, want current message content", got)
	}
	if !strings.Contains(got, "再亮一点") {
		t.Fatalf("registration task = %q, want text part", got)
	}
	if strings.Contains(got, "ignored") {
		t.Fatalf("registration task should not include image data: %q", got)
	}
}

func TestRunRegistersImageToolsFromCurrentMessageIntent(t *testing.T) {
	client := &stubTaskRuntimeClient{}
	route := llmutil.ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{
			Provider: "codex",
			Model:    "gpt-5.5",
		},
	}
	rt, err := Bootstrap(depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			return slog.Default(), nil
		},
		LogOptions: func() agent.LogOptions {
			return agent.LogOptions{}
		},
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return route, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
			return client, nil
		},
		CreateImageClient: func() (llm.ImageClient, error) {
			return stubTaskRuntimeImageClient{}, nil
		},
		Registry: func() *tools.Registry {
			return tools.NewRegistry()
		},
		RuntimeToolsConfig: toolsutil.RuntimeToolsRegisterConfig{
			Image: toolsutil.ImageToolsRegisterConfig{
				GenerateEnabled: true,
				EditEnabled:     true,
				FileCacheDir:    t.TempDir(),
				Configured:      true,
				Provider:        "openai",
				Model:           "gpt-image-2",
			},
		},
		PromptSpec: func(_ context.Context, _ *slog.Logger, _ agent.LogOptions, _ string, _ llm.Client, _ string, _ []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), nil, nil
		},
	}, BootstrapOptions{
		AgentConfig: agent.Config{MaxSteps: 2, ParseRetries: 0, ToolRepeatLimit: 2},
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	_, err = rt.Run(context.Background(), RunRequest{
		Task:  "console",
		Model: "gpt-5.5",
		CurrentMessage: &llm.Message{
			Role:    "user",
			Content: `{"current_message":{"text":"你试试画一张日出。"}}`,
		},
		ImageToolScope:     "console:topic",
		ImageToolRetention: toolsutil.ImageToolRetentionSticky,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("client requests = %d, want 1", len(client.requests))
	}
	if !requestHasTool(client.requests[0], "image_generate") {
		t.Fatalf("image_generate not registered; tools = %#v", toolNames(client.requests[0]))
	}
	if !requestHasTool(client.requests[0], "image_edit") {
		t.Fatalf("image_edit not registered; tools = %#v", toolNames(client.requests[0]))
	}
}

func requestHasTool(req llm.Request, name string) bool {
	for _, tool := range req.Tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func toolNames(req llm.Request) []string {
	out := make([]string, 0, len(req.Tools))
	for _, tool := range req.Tools {
		out = append(out, tool.Name)
	}
	return out
}

func TestBootstrapLeavesMainModelEmptyWhenRouteModelMissing(t *testing.T) {
	rt, err := Bootstrap(depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			return slog.Default(), nil
		},
		LogOptions: func() agent.LogOptions {
			return agent.LogOptions{}
		},
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return llmutil.ResolvedRoute{
				ClientConfig: llmconfig.ClientConfig{
					Provider: "openai",
					Model:    "",
				},
			}, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
			return &stubTaskRuntimeClient{}, nil
		},
		Registry: func() *tools.Registry {
			return tools.NewRegistry()
		},
		PromptSpec: func(_ context.Context, _ *slog.Logger, _ agent.LogOptions, _ string, _ llm.Client, _ string, _ []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), nil, nil
		},
	}, BootstrapOptions{
		AgentConfig: agent.Config{MaxSteps: 2, ParseRetries: 0, ToolRepeatLimit: 2},
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if rt.BootstrapMainModel != "" {
		t.Fatalf("BootstrapMainModel = %q, want empty", rt.BootstrapMainModel)
	}
}

func TestRunResolvesMainModelLate(t *testing.T) {
	client := &stubTaskRuntimeClient{}
	currentModel := "gpt-5.2"
	rt, err := Bootstrap(depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			return slog.Default(), nil
		},
		LogOptions: func() agent.LogOptions {
			return agent.LogOptions{}
		},
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return llmutil.ResolvedRoute{
				ClientConfig: llmconfig.ClientConfig{
					Provider: "openai",
					Model:    currentModel,
				},
			}, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
			return client, nil
		},
		Registry: func() *tools.Registry {
			return tools.NewRegistry()
		},
		PromptSpec: func(_ context.Context, _ *slog.Logger, _ agent.LogOptions, _ string, _ llm.Client, model string, _ []string) (agent.PromptSpec, []string, error) {
			if strings.TrimSpace(model) == "" {
				t.Fatal("PromptSpec received empty model")
			}
			return agent.DefaultPromptSpec(), nil, nil
		},
	}, BootstrapOptions{
		AgentConfig: agent.Config{MaxSteps: 2, ParseRetries: 0, ToolRepeatLimit: 2},
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	if _, err := rt.Run(context.Background(), RunRequest{Task: "first"}); err != nil {
		t.Fatalf("Run(first) error = %v", err)
	}
	currentModel = "gpt-4.1-mini"
	if _, err := rt.Run(context.Background(), RunRequest{Task: "second"}); err != nil {
		t.Fatalf("Run(second) error = %v", err)
	}
	if len(client.requests) != 2 {
		t.Fatalf("client requests = %d, want 2", len(client.requests))
	}
	if got := client.requests[0].Model; got != "gpt-5.2" {
		t.Fatalf("first request model = %q, want gpt-5.2", got)
	}
	if got := client.requests[1].Model; got != "gpt-4.1-mini" {
		t.Fatalf("second request model = %q, want gpt-4.1-mini", got)
	}
}
