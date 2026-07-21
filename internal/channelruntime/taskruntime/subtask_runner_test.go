package taskruntime

import (
	"context"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type stubAllowedSubtaskTool struct {
	name string
}

func (t stubAllowedSubtaskTool) Name() string            { return t.name }
func (t stubAllowedSubtaskTool) Description() string     { return "stub" }
func (t stubAllowedSubtaskTool) ParameterSchema() string { return `{"type":"object"}` }
func (t stubAllowedSubtaskTool) Execute(context.Context, map[string]any) (string, error) {
	return "ok", nil
}

type subtaskRouteSnapshotParentClient struct {
	requests      []llm.Request
	spawnModel    string
	requestedTool string
}

func (c *subtaskRouteSnapshotParentClient) Chat(_ context.Context, req llm.Request) (llm.Result, error) {
	c.requests = append(c.requests, req)
	if len(c.requests) > 1 {
		return llm.Result{Text: `{"type":"final","output":"parent done"}`}, nil
	}
	arguments := map[string]any{
		"task":  "run child task",
		"tools": []any{c.requestedTool},
	}
	if strings.TrimSpace(c.spawnModel) != "" {
		arguments["model"] = c.spawnModel
	}
	return llm.Result{ToolCalls: []llm.ToolCall{{
		ID:        "call_spawn",
		Name:      "spawn",
		Arguments: arguments,
	}}}, nil
}

func TestPreparedEngineSpawnUsesParentConcreteRouteSnapshot(t *testing.T) {
	weightedRoute := llmutil.ResolvedRoute{
		Purpose:  llmutil.RoutePurposeMainLoop,
		Identity: "candidates=weighted-a=1,weighted-b=1|fallbacks=backup",
		Candidates: []llmutil.ResolvedCandidate{
			{
				Profile: "weighted-a",
				Source:  llmutil.ProfileSourceConfig,
				Weight:  1,
				ClientConfig: llmconfig.ClientConfig{
					Provider: "weighted-provider-a",
					Endpoint: "https://weighted-a.example.test/v1",
					Model:    "weighted-model-a",
				},
			},
			{
				Profile: "weighted-b",
				Source:  llmutil.ProfileSourceConfig,
				Weight:  1,
				ClientConfig: llmconfig.ClientConfig{
					Provider: "weighted-provider-b",
					Endpoint: "https://weighted-b.example.test/v1",
					Model:    "weighted-model-b",
				},
			},
		},
		Fallbacks: []llmutil.ResolvedFallback{{
			Profile: "backup",
			Source:  llmutil.ProfileSourceConfig,
			ClientConfig: llmconfig.ClientConfig{
				Provider: "backup-provider",
				Endpoint: "https://backup.example.test/v1",
				Model:    "backup-model",
			},
		}},
	}
	thinkRoute := llmutil.ResolvedRoute{
		Purpose:  llmutil.RoutePurposeThink,
		Identity: "profile=reasoning",
		Profile:  "reasoning",
		Source:   llmutil.ProfileSourceConfig,
		ClientConfig: llmconfig.ClientConfig{
			Provider: "think-provider",
			Endpoint: "https://think.example.test/v1",
			Model:    "think-model",
		},
	}
	profileRoute := llmutil.ResolvedRoute{
		Purpose:  llmutil.RoutePurposeMainLoop,
		Identity: "profile=selected",
		Profile:  "selected",
		Source:   llmutil.ProfileSourceConfig,
		ClientConfig: llmconfig.ClientConfig{
			Provider: "profile-provider",
			Endpoint: "https://profile.example.test/v1",
			Model:    "profile-model",
		},
	}
	driftRoute := llmutil.ResolvedRoute{
		Purpose:  llmutil.RoutePurposeMainLoop,
		Identity: "profile=drifted",
		Profile:  "drifted",
		Source:   llmutil.ProfileSourceConfig,
		ClientConfig: llmconfig.ClientConfig{
			Provider: "drift-provider",
			Endpoint: "https://drift.example.test/v1",
			Model:    "drift-model",
		},
	}

	tests := []struct {
		name          string
		task          string
		profile       string
		runID         string
		spawnModel    string
		resolvedRoute llmutil.ResolvedRoute
		wantRoute     llmutil.ResolvedRoute
	}{
		{
			name:          "weighted candidate",
			task:          "run parent task",
			runID:         "parent-weighted-route",
			resolvedRoute: weightedRoute,
			wantRoute:     llmutil.SelectRouteCandidate(weightedRoute, "parent-weighted-route"),
		},
		{
			name:          "think route",
			task:          "/think run parent task",
			runID:         "parent-think-route",
			resolvedRoute: thinkRoute,
			wantRoute:     llmutil.ResolvedRouteWithReasoningEffort(thinkRoute, llmutil.ReasoningEffortXHigh),
		},
		{
			name:          "profile override",
			task:          "run parent task",
			profile:       "selected",
			runID:         "parent-profile-route",
			resolvedRoute: profileRoute,
			wantRoute:     profileRoute,
		},
		{
			name:          "explicit child model",
			task:          "run parent task",
			profile:       "selected",
			runID:         "parent-profile-model-override",
			spawnModel:    "child-model-override",
			resolvedRoute: profileRoute,
			wantRoute:     profileRoute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parentClient := &subtaskRouteSnapshotParentClient{
				spawnModel:    tt.spawnModel,
				requestedTool: "allowed_tool",
			}
			childClient := &stubTaskRuntimeClient{result: llm.Result{Text: `{"type":"final","output":"child done"}`}}
			createdRoutes := make([]llmutil.ResolvedRoute, 0, 2)
			resolveCalls := 0
			deps := depsutil.CommonDependencies{
				Logger:     func() (*slog.Logger, error) { return slog.Default(), nil },
				LogOptions: func() agent.LogOptions { return agent.LogOptions{} },
				ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
					resolveCalls++
					if tt.profile == "" && resolveCalls == 1 {
						return tt.resolvedRoute, nil
					}
					return driftRoute, nil
				},
				ResolveLLMRouteWithProfile: func(_ string, profile string) (llmutil.ResolvedRoute, error) {
					if profile != tt.profile {
						t.Fatalf("resolved profile = %q, want %q", profile, tt.profile)
					}
					return tt.resolvedRoute, nil
				},
				CreateLLMClient: func(route llmutil.ResolvedRoute) (llm.Client, error) {
					createdRoutes = append(createdRoutes, route)
					if len(createdRoutes) == 1 {
						return parentClient, nil
					}
					return childClient, nil
				},
				Registry: func() *tools.Registry {
					reg := tools.NewRegistry()
					if err := reg.Register(stubAllowedSubtaskTool{name: "allowed_tool"}); err != nil {
						t.Fatalf("Register(allowed_tool) error = %v", err)
					}
					return reg
				},
				PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
					return agent.DefaultPromptSpec(), nil, nil
				},
			}
			rt, err := NewRunPreparer(deps, BootstrapOptions{AgentConfig: agent.Config{MaxSteps: 3, ToolRepeatLimit: 2}})
			if err != nil {
				t.Fatalf("NewRunPreparer() error = %v", err)
			}
			defer func() { _ = rt.Close() }()

			result, err := rt.Run(llmstats.WithRunID(context.Background(), tt.runID), RunRequest{
				Task:                tt.task,
				LLMProfile:          tt.profile,
				DisableRuntimeTools: true,
			})
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if result.Final == nil || result.Final.Output != "parent done" {
				t.Fatalf("Run() final = %#v, want parent done", result.Final)
			}
			if len(createdRoutes) != 2 {
				t.Fatalf("created routes = %#v, want parent and child", createdRoutes)
			}
			assertSubtaskRouteSnapshot(t, createdRoutes[0], tt.wantRoute)
			assertSubtaskRouteSnapshot(t, createdRoutes[1], tt.wantRoute)
			if !reflect.DeepEqual(createdRoutes[1], createdRoutes[0]) {
				t.Fatalf("child route = %#v, want unchanged parent route %#v", createdRoutes[1], createdRoutes[0])
			}
			if len(childClient.requests) != 1 {
				t.Fatalf("child requests = %d, want 1", len(childClient.requests))
			}
			wantChildModel := tt.wantRoute.ClientConfig.Model
			if tt.spawnModel != "" {
				wantChildModel = tt.spawnModel
			}
			if got := childClient.requests[0].Model; got != wantChildModel {
				t.Fatalf("child request model = %q, want %q", got, wantChildModel)
			}
		})
	}
}

func assertSubtaskRouteSnapshot(t *testing.T, got, want llmutil.ResolvedRoute) {
	t.Helper()
	if got.Identity != want.Identity || got.Profile != want.Profile ||
		got.ClientConfig.Provider != want.ClientConfig.Provider ||
		got.ClientConfig.Endpoint != want.ClientConfig.Endpoint ||
		got.ClientConfig.Model != want.ClientConfig.Model {
		t.Fatalf(
			"route identity/profile/provider/endpoint/model = %q/%q/%q/%q/%q, want %q/%q/%q/%q/%q",
			got.Identity,
			got.Profile,
			got.ClientConfig.Provider,
			got.ClientConfig.Endpoint,
			got.ClientConfig.Model,
			want.Identity,
			want.Profile,
			want.ClientConfig.Provider,
			want.ClientConfig.Endpoint,
			want.ClientConfig.Model,
		)
	}
}

func TestRunSubtaskPublicAPIResolvesRouteAtCall(t *testing.T) {
	wantRoute := llmutil.ResolvedRoute{
		Purpose:  llmutil.RoutePurposeMainLoop,
		Identity: "profile=current",
		Profile:  "current",
		Source:   llmutil.ProfileSourceConfig,
		ClientConfig: llmconfig.ClientConfig{
			Provider: "current-provider",
			Endpoint: "https://current.example.test/v1",
			Model:    "current-model",
		},
	}
	client := &stubTaskRuntimeClient{result: llm.Result{Text: `{"type":"final","output":"done"}`}}
	var createdRoute llmutil.ResolvedRoute
	rt, err := NewRunPreparer(depsutil.CommonDependencies{
		Logger:          func() (*slog.Logger, error) { return slog.Default(), nil },
		LogOptions:      func() agent.LogOptions { return agent.LogOptions{} },
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) { return wantRoute, nil },
		CreateLLMClient: func(route llmutil.ResolvedRoute) (llm.Client, error) {
			createdRoute = route
			return client, nil
		},
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), nil, nil
		},
	}, BootstrapOptions{AgentConfig: agent.Config{MaxSteps: 1}})
	if err != nil {
		t.Fatalf("NewRunPreparer() error = %v", err)
	}
	defer func() { _ = rt.Close() }()

	result, err := rt.RunSubtask(context.Background(), agent.SubtaskRequest{Task: "run child task"})
	if err != nil {
		t.Fatalf("RunSubtask() error = %v", err)
	}
	if result == nil || result.Status != agent.SubtaskStatusDone {
		t.Fatalf("RunSubtask() result = %#v, want done", result)
	}
	assertSubtaskRouteSnapshot(t, createdRoute, wantRoute)
}

func TestRunSubtaskReturnsEnvelope(t *testing.T) {
	client := &stubTaskRuntimeClient{
		result: llm.Result{Text: `{"type":"final","output":"{\"ok\":true}"}`},
	}
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
		RuntimeToolsConfig: toolsutil.RuntimeToolsRegisterConfig{
			PlanCreate: toolsutil.BuildPlanCreateRegisterConfig(true, 6),
			TodoUpdate: toolsutil.BuildTodoUpdateRegisterConfig(true, t.TempDir(), "contacts"),
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

	ctx := llmstats.WithRunID(context.Background(), "parent_run")
	reg := tools.NewRegistry()
	reg.Register(stubAllowedSubtaskTool{name: "bash"})
	result, err := rt.RunSubtask(ctx, agent.SubtaskRequest{
		Task:         "ping",
		Model:        "gpt-5.4",
		OutputSchema: "subtask.test.v1",
		Registry:     reg,
	})
	if err != nil {
		t.Fatalf("RunSubtask() error = %v", err)
	}
	if result == nil {
		t.Fatal("RunSubtask() result is nil")
	}
	if result.Status != agent.SubtaskStatusDone {
		t.Fatalf("Status = %q, want %q", result.Status, agent.SubtaskStatusDone)
	}
	if result.OutputKind != agent.SubtaskOutputKindJSON {
		t.Fatalf("OutputKind = %q, want %q", result.OutputKind, agent.SubtaskOutputKindJSON)
	}
	if result.OutputSchema != "subtask.test.v1" {
		t.Fatalf("OutputSchema = %q, want subtask.test.v1", result.OutputSchema)
	}
	payload, ok := result.Output.(map[string]any)
	if !ok || payload["ok"] != true {
		t.Fatalf("Output = %#v, want parsed JSON payload", result.Output)
	}
	if !strings.HasPrefix(result.TaskID, "sub_") {
		t.Fatalf("TaskID = %q, want prefix sub_", result.TaskID)
	}
	if len(client.requests) != 1 {
		t.Fatalf("client requests = %d, want 1", len(client.requests))
	}
	if got := client.requests[0].Scene; got != "spawn.subtask" {
		t.Fatalf("request scene = %q, want spawn.subtask", got)
	}
	if got := client.requests[0].Model; got != "gpt-5.4" {
		t.Fatalf("request model = %q, want gpt-5.4", got)
	}
	if len(client.requests[0].Tools) != 1 || client.requests[0].Tools[0].Name != "bash" {
		t.Fatalf("request tools = %#v, want only bash", client.requests[0].Tools)
	}
	foundParentRunID := false
	for _, msg := range client.requests[0].Messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "subtask_parent_run_id") && strings.Contains(msg.Content, "parent_run") {
			foundParentRunID = true
			break
		}
	}
	if !foundParentRunID {
		t.Fatalf("expected injected meta to include parent run id, messages=%#v", client.requests[0].Messages)
	}
	foundOutputSchemaInstruction := false
	for _, msg := range client.requests[0].Messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "output_schema=subtask.test.v1") {
			foundOutputSchemaInstruction = true
			break
		}
	}
	if !foundOutputSchemaInstruction {
		t.Fatalf("expected task prompt to include output schema instruction, messages=%#v", client.requests[0].Messages)
	}
}

func TestRunSubtaskDoesNotTriggerToolsOutsideWhitelist(t *testing.T) {
	client := &stubTaskRuntimeClient{
		result: llm.Result{Text: `{"type":"final","output":"ok"}`},
	}
	route := llmutil.ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{
			Provider: "openai",
			Model:    "gpt-5.2",
		},
	}
	var triggeredStaticTools bool
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
		ToolTriggers: func(string) map[string]bool {
			return map[string]bool{"bash": true}
		},
		RegisterTriggeredStaticTools: func(reg *tools.Registry, triggers map[string]bool) {
			if triggers["bash"] {
				triggeredStaticTools = true
				reg.Register(stubAllowedSubtaskTool{name: "bash"})
			}
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

	reg := tools.NewRegistry()
	reg.Register(stubAllowedSubtaskTool{name: "url_fetch"})
	result, err := rt.RunSubtask(context.Background(), agent.SubtaskRequest{
		Task:     "$bash echo should not escape whitelist",
		Registry: reg,
	})
	if err != nil {
		t.Fatalf("RunSubtask() error = %v", err)
	}
	if result == nil || result.Status != agent.SubtaskStatusDone {
		t.Fatalf("RunSubtask() result = %#v, want done", result)
	}
	if triggeredStaticTools {
		t.Fatal("subtask should not register static tools from task triggers")
	}
	if len(client.requests) != 1 {
		t.Fatalf("client requests = %d, want 1", len(client.requests))
	}
	if len(client.requests[0].Tools) != 1 || client.requests[0].Tools[0].Name != "url_fetch" {
		t.Fatalf("request tools = %#v, want only url_fetch", client.requests[0].Tools)
	}
}

func TestRunSubtaskDirectPathSkipsLLMAndNormalizesResult(t *testing.T) {
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
	}, BootstrapOptions{
		AgentConfig: agent.Config{MaxSteps: 2, ParseRetries: 0, ToolRepeatLimit: 2},
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	result, err := rt.RunSubtask(context.Background(), agent.SubtaskRequest{
		OutputSchema: "subtask.direct.v1",
		RunFunc: func(context.Context) (*agent.SubtaskResult, error) {
			return &agent.SubtaskResult{
				Status:     agent.SubtaskStatusDone,
				OutputKind: agent.SubtaskOutputKindJSON,
				Output: map[string]any{
					"ok": true,
				},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("RunSubtask() error = %v", err)
	}
	if result == nil {
		t.Fatal("RunSubtask() result is nil")
	}
	if result.Status != agent.SubtaskStatusDone {
		t.Fatalf("Status = %q, want done", result.Status)
	}
	if result.OutputSchema != "subtask.direct.v1" {
		t.Fatalf("OutputSchema = %q, want subtask.direct.v1", result.OutputSchema)
	}
	if len(client.requests) != 0 {
		t.Fatalf("expected direct subtask to skip llm client, got %d requests", len(client.requests))
	}
}

func TestRunSubtaskRejectsNestedDepth(t *testing.T) {
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
	}, BootstrapOptions{
		AgentConfig: agent.Config{MaxSteps: 2, ParseRetries: 0, ToolRepeatLimit: 2},
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	ctx := agent.WithSubtaskDepth(context.Background(), 1)
	result, err := rt.RunSubtask(ctx, agent.SubtaskRequest{
		RunFunc: func(context.Context) (*agent.SubtaskResult, error) {
			t.Fatal("nested subtask should not execute callback")
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("RunSubtask() error = %v", err)
	}
	if result == nil {
		t.Fatal("RunSubtask() result is nil")
	}
	if result.Status != agent.SubtaskStatusFailed {
		t.Fatalf("Status = %q, want failed", result.Status)
	}
	if !strings.Contains(result.Error, "depth limit") {
		t.Fatalf("Error = %q, want depth limit", result.Error)
	}
	if len(client.requests) != 0 {
		t.Fatalf("nested subtask should not reach llm client, got %d requests", len(client.requests))
	}
}
