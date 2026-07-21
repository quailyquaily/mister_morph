package runcmd

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/spf13/viper"
)

type cliRunTestClient struct {
	requests   []llm.Request
	closeCalls int
}

func (c *cliRunTestClient) Chat(_ context.Context, req llm.Request) (llm.Result, error) {
	c.requests = append(c.requests, req)
	return llm.Result{Text: `{"type":"final","output":"ok"}`}, nil
}

func (c *cliRunTestClient) Close() error {
	c.closeCalls++
	return nil
}

func TestCLIContextCompactionStatusWritesOnlySuccessToStatusStream(t *testing.T) {
	var output bytes.Buffer
	ctx := withCLIContextCompactionStatus(context.Background(), nil, &output)

	agent.EmitEvent(ctx, nil, agent.Event{Kind: agent.EventKindContextCompactionStart})
	agent.EmitEvent(ctx, nil, agent.Event{Kind: agent.EventKindContextCompactionDone})

	want := taskruntime.ContextCompactionDoneText + "\n"
	if output.String() != want {
		t.Fatalf("status output = %q, want %q", output.String(), want)
	}
}

func TestResolveRunRoutesFixesWeightedCandidates(t *testing.T) {
	values := llmutil.RuntimeValues{
		Provider: "openai",
		Model:    "default-model",
		Profiles: map[string]llmutil.ProfileConfig{
			"main_a": {Model: "main-a-model"},
			"main_b": {Model: "main-b-model"},
			"plan_a": {Model: "plan-a-model"},
			"plan_b": {Model: "plan-b-model"},
		},
		Routes: llmutil.RoutesConfig{PurposeRoutes: llmutil.PurposeRoutes{
			MainLoop: llmutil.RoutePolicyConfig{Candidates: []llmutil.RouteCandidateConfig{
				{Profile: "main_a", Weight: 1},
				{Profile: "main_b", Weight: 1},
			}},
			PlanCreate: llmutil.RoutePolicyConfig{Candidates: []llmutil.RouteCandidateConfig{
				{Profile: "plan_a", Weight: 1},
				{Profile: "plan_b", Weight: 1},
			}},
		}},
	}
	const runID = "cli-weighted-run"
	mainRoute, planRoute, err := resolveRunRoutes(values, runID)
	if err != nil {
		t.Fatalf("resolveRunRoutes() error = %v", err)
	}
	if len(mainRoute.Candidates) != 0 || len(planRoute.Candidates) != 0 {
		t.Fatalf("routes remain weighted: main=%#v plan=%#v", mainRoute, planRoute)
	}
	if mainRoute.ClientConfig.Model == "" || planRoute.ClientConfig.Model == "" {
		t.Fatalf("routes are missing client configs: main=%#v plan=%#v", mainRoute, planRoute)
	}
	rawMain, _ := llmutil.ResolveRoute(values, llmutil.RoutePurposeMainLoop)
	rawPlan, _ := llmutil.ResolveRoute(values, llmutil.RoutePurposePlanCreate)
	if want := llmutil.SelectRouteCandidate(rawMain, runID).ClientConfig.Model; mainRoute.ClientConfig.Model != want {
		t.Fatalf("main model = %q, want %q", mainRoute.ClientConfig.Model, want)
	}
	if want := llmutil.SelectRouteCandidate(rawPlan, runID).ClientConfig.Model; planRoute.ClientConfig.Model != want {
		t.Fatalf("plan model = %q, want %q", planRoute.ClientConfig.Model, want)
	}
}

func TestApplyRunClientConfigOverridesUsesOnlyChangedFlags(t *testing.T) {
	cmd := New(Dependencies{})
	for name, value := range map[string]string{
		"provider":            "override-provider",
		"endpoint":            "https://override.invalid/v1",
		"api-key":             "override-key",
		"model":               "override-model",
		"llm-request-timeout": "17s",
	} {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("set --%s: %v", name, err)
		}
	}
	cfg := llmconfig.ClientConfig{
		Provider:       "configured-provider",
		Endpoint:       "https://configured.invalid/v1",
		APIKey:         "configured-key",
		Model:          "configured-model",
		RequestTimeout: time.Minute,
	}

	applyRunClientConfigOverrides(cmd, &cfg)

	if cfg.Provider != "override-provider" || cfg.Endpoint != "https://override.invalid/v1" || cfg.APIKey != "override-key" || cfg.Model != "override-model" {
		t.Fatalf("overridden config = %#v", cfg)
	}
	if cfg.RequestTimeout != 17*time.Second {
		t.Fatalf("request timeout = %s, want 17s", cfg.RequestTimeout)
	}
}

func TestRunCommandPropagatesGuardInitializationError(t *testing.T) {
	guardErr := errors.New("guard unavailable")
	viper.Reset()
	t.Cleanup(viper.Reset)
	configdefaults.Apply(viper.GetViper())
	viper.Set("file_state_dir", t.TempDir())
	viper.Set("llm.provider", "openai")
	viper.Set("llm.model", "test-model")
	cmd := New(Dependencies{
		GuardFromViper: func(*slog.Logger) (*guard.Guard, error) {
			return nil, guardErr
		},
	})
	cmd.SetArgs([]string{"--task", "test guard", "--no-workspace"})

	err := cmd.Execute()

	if !errors.Is(err, guardErr) {
		t.Fatalf("Execute() error = %v, want guard error", err)
	}
}

func TestRunCommandPropagatesCommandCancellationToLLMCall(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	configdefaults.Apply(viper.GetViper())
	viper.Set("file_state_dir", t.TempDir())
	viper.Set("llm.provider", "openai_custom")
	viper.Set("llm.endpoint", "http://127.0.0.1:1/v1")
	viper.Set("llm.api_key", "test-key")
	viper.Set("llm.model", "test-model")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := New(Dependencies{})
	cmd.SetArgs([]string{"--task", "test cancellation", "--no-workspace"})

	err := cmd.ExecuteContext(ctx)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "context canceled") {
		t.Fatalf("ExecuteContext() error = %v, want context cancellation", err)
	}
}

func TestRunCommandAcceptsEmptyHeartbeatChecklist(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	configdefaults.Apply(viper.GetViper())
	viper.Set("file_state_dir", t.TempDir())
	viper.Set("llm.provider", "openai_custom")
	viper.Set("llm.endpoint", "http://127.0.0.1:1/v1")
	viper.Set("llm.api_key", "test-key")
	viper.Set("llm.model", "test-model")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := New(Dependencies{})
	cmd.SetArgs([]string{"--heartbeat", "--no-workspace"})

	err := cmd.ExecuteContext(ctx)
	if err == nil || strings.Contains(strings.ToLower(err.Error()), "empty task") {
		t.Fatalf("ExecuteContext() error = %v, want heartbeat to reach the canceled LLM call", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "context canceled") {
		t.Fatalf("ExecuteContext() error = %v, want context cancellation", err)
	}
}

func TestCLIRunPreparerPreservesRoutePromptHeartbeatAndClientOwnership(t *testing.T) {
	mainClient := &cliRunTestClient{}
	planClient := &cliRunTestClient{}
	var createdRoutes []llmutil.ResolvedRoute
	prep := cliRunPreparationFixture(func(route llmutil.ResolvedRoute) (llm.Client, error) {
		createdRoutes = append(createdRoutes, route)
		if route.Purpose == llmutil.RoutePurposePlanCreate {
			return planClient, nil
		}
		return mainClient, nil
	})
	prep.workspaceDir = "/workspace/project"
	prep.runtimeToolsConfig.TodoUpdate.Enabled = true

	runtime, err := newCLIRunPreparer(prep, Dependencies{})
	if err != nil {
		t.Fatalf("newCLIRunPreparer() error = %v", err)
	}
	defer func() { _ = runtime.Close() }()

	ctx := llmstats.WithRunID(context.Background(), prep.runID)
	result, err := runtime.Run(ctx, taskruntime.RunRequest{
		Task:                     "check heartbeat",
		Model:                    prep.mainRoute.ClientConfig.Model,
		Route:                    &prep.mainRoute,
		Scene:                    "cli.loop",
		DisableTodoWorkflow:      true,
		DisableContextCompaction: true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Final == nil || result.Final.Output != "ok" {
		t.Fatalf("Run() final = %#v, want output ok", result.Final)
	}
	if len(createdRoutes) != 2 {
		t.Fatalf("created routes = %d, want main and plan", len(createdRoutes))
	}
	if got := createdRoutes[0].ClientConfig; got.Provider != "override-provider" || got.Model != "override-model" || got.RequestTimeout != 17*time.Second {
		t.Fatalf("main route config = %#v, want explicit CLI overrides", got)
	}
	if createdRoutes[1].Purpose != llmutil.RoutePurposePlanCreate || createdRoutes[1].ClientConfig.Model != "plan-model" {
		t.Fatalf("plan route = %#v", createdRoutes[1])
	}
	if len(mainClient.requests) != 1 {
		t.Fatalf("main requests = %d, want 1", len(mainClient.requests))
	}
	systemPrompt := mainClient.requests[0].Messages[0].Content
	if !strings.Contains(systemPrompt, "## Workspace Context") || !strings.Contains(systemPrompt, "workspace_dir: /workspace/project") {
		t.Fatalf("system prompt is missing workspace block: %q", systemPrompt)
	}
	if strings.Contains(systemPrompt, "[[ Cron Task Workflow ]]") {
		t.Fatalf("heartbeat prompt includes todo workflow: %q", systemPrompt)
	}

	if _, err := runtime.Run(ctx, taskruntime.RunRequest{
		Task:  "regular task",
		Model: prep.mainRoute.ClientConfig.Model,
		Route: &prep.mainRoute,
		Scene: "cli.loop",
	}); err != nil {
		t.Fatalf("regular Run() error = %v", err)
	}
	if len(mainClient.requests) != 2 || !strings.Contains(mainClient.requests[1].Messages[0].Content, "[[ Cron Task Workflow ]]") {
		t.Fatalf("regular run prompt is missing todo workflow: %#v", mainClient.requests)
	}
	if mainClient.closeCalls != 2 || planClient.closeCalls != 2 {
		t.Fatalf("client close calls = main:%d plan:%d, want one close per run", mainClient.closeCalls, planClient.closeCalls)
	}
}

func TestCLIRunPreparerAppliesSharedDecoratorToMainAndPlanClients(t *testing.T) {
	mainClient := &cliRunTestClient{}
	planClient := &cliRunTestClient{}
	prep := cliRunPreparationFixture(func(route llmutil.ResolvedRoute) (llm.Client, error) {
		if route.Purpose == llmutil.RoutePurposePlanCreate {
			return planClient, nil
		}
		return mainClient, nil
	})
	var decoratedPurposes []string
	prep.clientDecorator = func(client llm.Client, route llmutil.ResolvedRoute) llm.Client {
		decoratedPurposes = append(decoratedPurposes, route.Purpose)
		return client
	}

	runtime, err := newCLIRunPreparer(prep, Dependencies{})
	if err != nil {
		t.Fatalf("newCLIRunPreparer() error = %v", err)
	}
	defer func() { _ = runtime.Close() }()
	if _, err := runtime.Run(context.Background(), taskruntime.RunRequest{
		Task:  "inspect this task",
		Model: prep.mainRoute.ClientConfig.Model,
		Route: &prep.mainRoute,
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(decoratedPurposes) != 2 || decoratedPurposes[0] != llmutil.RoutePurposeMainLoop || decoratedPurposes[1] != llmutil.RoutePurposePlanCreate {
		t.Fatalf("decorated route purposes = %#v, want main and plan", decoratedPurposes)
	}
}

func cliRunPreparationFixture(createClient func(llmutil.ResolvedRoute) (llm.Client, error)) cliRunPreparation {
	values := llmutil.RuntimeValues{Provider: "configured-provider", Model: "configured-model"}
	mainRoute := llmutil.ResolvedRoute{
		Purpose: llmutil.RoutePurposeMainLoop,
		Profile: "main",
		Values:  values,
		ClientConfig: llmconfig.ClientConfig{
			Provider:       "override-provider",
			Model:          "override-model",
			RequestTimeout: 17 * time.Second,
		},
	}
	planRoute := llmutil.ResolvedRoute{
		Purpose: llmutil.RoutePurposePlanCreate,
		Profile: "plan",
		Values:  values,
		ClientConfig: llmconfig.ClientConfig{
			Provider: "configured-provider",
			Model:    "plan-model",
		},
	}
	return cliRunPreparation{
		runID:      "cli-test-run",
		values:     values,
		mainRoute:  mainRoute,
		planRoute:  planRoute,
		logger:     slog.Default(),
		logOptions: agent.LogOptions{},
		skillsConfig: skillsutil.SkillsConfig{
			Enabled: false,
		},
		runtimeToolsConfig: toolsutil.RuntimeToolsRegisterConfig{},
		runtimePaths:       runtimepaths.Paths{},
		agentConfig:        agent.Config{MaxSteps: 1},
		engineToolsConfig:  agent.EngineToolsConfig{},
		createLLMClient:    createClient,
	}
}
