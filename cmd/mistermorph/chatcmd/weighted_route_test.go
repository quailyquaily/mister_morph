package chatcmd

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type chatWeightedStubClient struct {
	id         string
	closeCalls int
	closeErr   error
}

func (*chatWeightedStubClient) Chat(context.Context, llm.Request) (llm.Result, error) {
	return llm.Result{}, nil
}

func (c *chatWeightedStubClient) Close() error {
	c.closeCalls++
	return c.closeErr
}

func newWeightedChatTaskRuntime(t *testing.T, values llmutil.RuntimeValues, builtRoutes *[]llmutil.ResolvedRoute, clients *[]*chatWeightedStubClient) *taskruntime.Runtime {
	t.Helper()
	rt, err := taskruntime.NewRunPreparer(depsutil.CommonDependencies{
		Logger:     func() (*slog.Logger, error) { return slog.Default(), nil },
		LogOptions: func() agent.LogOptions { return agent.LogOptions{} },
		ResolveLLMRoute: func(purpose string) (llmutil.ResolvedRoute, error) {
			return llmutil.ResolveRoute(values, purpose)
		},
		CreateLLMClient: func(route llmutil.ResolvedRoute) (llm.Client, error) {
			*builtRoutes = append(*builtRoutes, route)
			client := &chatWeightedStubClient{id: route.ClientConfig.Model}
			*clients = append(*clients, client)
			return client, nil
		},
		Registry: func() *tools.Registry { return tools.NewRegistry() },
		PromptSpec: func(ctx context.Context, _ *slog.Logger, _ agent.LogOptions, _ string, _ llm.Client, _ string, _ []string) (agent.PromptSpec, []string, error) {
			if err := ctx.Err(); err != nil {
				return agent.PromptSpec{}, nil, err
			}
			return agent.DefaultPromptSpec(), []string{"chat-skill"}, nil
		},
	}, taskruntime.BootstrapOptions{})
	if err != nil {
		t.Fatalf("NewRunPreparer() error = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	return rt
}

func TestResolveChatTaskRouteFixesWeightedMainAndThinkCandidates(t *testing.T) {
	values := llmutil.RuntimeValues{
		Provider: "openai",
		Model:    "default-model",
		Profiles: map[string]llmutil.ProfileConfig{
			"main_a":  {Model: "main-a-model"},
			"main_b":  {Model: "main-b-model"},
			"plan_a":  {Model: "plan-a-model"},
			"plan_b":  {Model: "plan-b-model"},
			"think_a": {Model: "think-a-model"},
			"think_b": {Model: "think-b-model"},
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
			Think: llmutil.RoutePolicyConfig{Candidates: []llmutil.RouteCandidateConfig{
				{Profile: "think_a", Weight: 1},
				{Profile: "think_b", Weight: 1},
			}},
		}},
	}
	const runID = "chat-weighted-run"
	mainRoute, mainWasWeighted, err := resolveChatTaskRoute(
		values,
		llmselect.MainSelection{Mode: llmselect.ModeAuto},
		llmutil.RoutePurposeMainLoop,
		"",
		runID,
	)
	if err != nil {
		t.Fatalf("resolveChatTaskRoutes() error = %v", err)
	}
	if !mainWasWeighted {
		t.Fatal("mainWasWeighted = false, want true")
	}
	if len(mainRoute.Candidates) != 0 {
		t.Fatalf("main route remains weighted: %#v", mainRoute)
	}
	rawMain, _ := llmselect.ResolveMainRoute(values, llmselect.MainSelection{Mode: llmselect.ModeAuto})
	if want := llmutil.SelectRouteCandidate(rawMain, runID).ClientConfig.Model; mainRoute.ClientConfig.Model != want {
		t.Fatalf("main model = %q, want %q", mainRoute.ClientConfig.Model, want)
	}
	thinkRoute, thinkWasWeighted, err := resolveChatTaskRoute(
		values,
		llmselect.MainSelection{Mode: llmselect.ModeAuto},
		llmutil.RoutePurposeThink,
		llmutil.ReasoningEffortXHigh,
		runID,
	)
	if err != nil {
		t.Fatalf("resolveChatTaskRoutes(think) error = %v", err)
	}
	rawThink, _ := llmutil.ResolveRoute(values, llmutil.RoutePurposeThink)
	wantThink := llmutil.SelectRouteCandidate(rawThink, runID)
	if !thinkWasWeighted || len(thinkRoute.Candidates) != 0 || thinkRoute.ClientConfig.Model != wantThink.ClientConfig.Model {
		t.Fatalf("think route = %#v, want concrete model %q", thinkRoute, wantThink.ClientConfig.Model)
	}
	if thinkRoute.Values.ReasoningEffortRaw != llmutil.ReasoningEffortXHigh {
		t.Fatalf("think reasoning effort = %q, want xhigh", thinkRoute.Values.ReasoningEffortRaw)
	}
}

func TestPrepareChatRuntimeReturnsPerTurnWeightedClientsWithoutReplacingSession(t *testing.T) {
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
	var builtRoutes []llmutil.ResolvedRoute
	var clients []*chatWeightedStubClient
	sess := &chatSession{
		cmd:          New(Dependencies{}),
		sessionStore: llmselect.NewStore(),
		llmValues:    values,
	}
	sess.taskRuntime = newWeightedChatTaskRuntime(t, values, &builtRoutes, &clients)

	prepared, err := sess.prepareRuntimeForTaskRoute(context.Background(), "ping", llmutil.RoutePurposeMainLoop, "", "chat-turn")
	if err != nil {
		t.Fatalf("prepareRuntimeForTaskRoute() error = %v", err)
	}
	defer func() { _ = prepared.Cleanup() }()
	if len(builtRoutes) != 2 || len(builtRoutes[0].Candidates) != 0 || len(builtRoutes[1].Candidates) != 0 {
		t.Fatalf("built routes = %#v, want two concrete routes", builtRoutes)
	}
	if sess.mainCfg.Model != "" {
		t.Fatalf("per-turn preparation replaced session model: %q", sess.mainCfg.Model)
	}
	if got := len(prepared.LoadedSkills); got != 1 || prepared.LoadedSkills[0] != "chat-skill" {
		t.Fatalf("loaded skills = %#v, want chat-skill", prepared.LoadedSkills)
	}
}

func TestRebuildChatRuntimeStateKeepsMetadataAndClosesPreparedClients(t *testing.T) {
	values := llmutil.RuntimeValues{
		Provider: "openai",
		Model:    "default-model",
		Profiles: map[string]llmutil.ProfileConfig{
			"main": {Model: "main-model"},
			"plan": {Model: "plan-model"},
		},
		Routes: llmutil.RoutesConfig{PurposeRoutes: llmutil.PurposeRoutes{
			MainLoop:   llmutil.RoutePolicyConfig{Profile: "main"},
			PlanCreate: llmutil.RoutePolicyConfig{Profile: "plan"},
		}},
	}
	var builtRoutes []llmutil.ResolvedRoute
	var clients []*chatWeightedStubClient
	sess := &chatSession{
		cmd:          New(Dependencies{}),
		sessionStore: llmselect.NewStore(),
		llmValues:    values,
	}
	sess.taskRuntime = newWeightedChatTaskRuntime(t, values, &builtRoutes, &clients)

	if err := sess.rebuildRuntimeStateForTask(context.Background(), "Interactive chat session"); err != nil {
		t.Fatalf("rebuildRuntimeStateForTask() error = %v", err)
	}
	if sess.mainCfg.Model != "main-model" || len(sess.loadedSkills) != 1 {
		t.Fatalf("session metadata = model:%q skills:%#v", sess.mainCfg.Model, sess.loadedSkills)
	}
	for _, client := range clients {
		if client.closeCalls != 1 {
			t.Fatalf("baseline client %q close calls = %d, want 1", client.id, client.closeCalls)
		}
	}
}

func TestRebuildChatRuntimeStateDoesNotPublishMetadataBeforeCleanupSucceeds(t *testing.T) {
	closeErr := errors.New("close prepared client")
	values := llmutil.RuntimeValues{Provider: "openai", Model: "next-model"}
	rt, err := taskruntime.NewRunPreparer(depsutil.CommonDependencies{
		Logger:          func() (*slog.Logger, error) { return slog.Default(), nil },
		ResolveLLMRoute: func(purpose string) (llmutil.ResolvedRoute, error) { return llmutil.ResolveRoute(values, purpose) },
		CreateLLMClient: func(route llmutil.ResolvedRoute) (llm.Client, error) {
			return &chatWeightedStubClient{id: route.ClientConfig.Model, closeErr: closeErr}, nil
		},
		Registry: func() *tools.Registry { return tools.NewRegistry() },
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), []string{"next-skill"}, nil
		},
	}, taskruntime.BootstrapOptions{})
	if err != nil {
		t.Fatalf("NewRunPreparer() error = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	sess := &chatSession{
		cmd:          New(Dependencies{}),
		taskRuntime:  rt,
		sessionStore: llmselect.NewStore(),
		llmValues:    values,
		mainCfg:      llmconfig.ClientConfig{Model: "previous-model"},
		loadedSkills: []string{"previous-skill"},
	}

	err = sess.rebuildRuntimeStateForTask(context.Background(), "Interactive chat session")
	if !errors.Is(err, closeErr) {
		t.Fatalf("rebuildRuntimeStateForTask() error = %v, want cleanup error", err)
	}
	if got := sess.mainCfg.Model; got != "previous-model" {
		t.Fatalf("model = %q, want previous-model after cleanup failure", got)
	}
	if got := strings.Join(sess.loadedSkills, ","); got != "previous-skill" {
		t.Fatalf("loaded skills = %q, want previous-skill after cleanup failure", got)
	}
}

func TestPrepareChatCommandRuntimeUsesConcreteWeightedRoutes(t *testing.T) {
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
	var builtRoutes []llmutil.ResolvedRoute
	var clients []*chatWeightedStubClient
	sess := &chatSession{
		cmd:          New(Dependencies{}),
		mainCfg:      llmconfig.ClientConfig{Provider: "openai", Model: "default-model"},
		sessionStore: llmselect.NewStore(),
		llmValues:    values,
	}
	sess.taskRuntime = newWeightedChatTaskRuntime(t, values, &builtRoutes, &clients)
	prepared, err := prepareChatCommandRuntime(context.Background(), sess, "/update")
	if err != nil {
		t.Fatalf("prepareChatCommandRuntime() error = %v", err)
	}
	if len(builtRoutes) != 2 || len(builtRoutes[0].Candidates) != 0 || len(builtRoutes[1].Candidates) != 0 {
		t.Fatalf("built routes = %#v, want concrete main and plan routes", builtRoutes)
	}
	if prepared.Engine == nil || prepared.Model == "default-model" {
		t.Fatalf("prepared command runtime = engine:%p model:%q, want selected route", prepared.Engine, prepared.Model)
	}
	if sess.mainCfg.Model != "default-model" {
		t.Fatalf("command preparation replaced baseline model: %q", sess.mainCfg.Model)
	}
	if err := prepared.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	for _, client := range clients {
		if client.closeCalls != 1 {
			t.Fatalf("temporary client %q close calls = %d, want 1", client.id, client.closeCalls)
		}
	}
}

func TestRebuildChatRuntimeUsesCallerContext(t *testing.T) {
	values := llmutil.RuntimeValues{Provider: "openai", Model: "default-model"}
	var builtRoutes []llmutil.ResolvedRoute
	var clients []*chatWeightedStubClient
	sess := &chatSession{
		cmd:          New(Dependencies{}),
		sessionStore: llmselect.NewStore(),
		llmValues:    values,
	}
	sess.taskRuntime = newWeightedChatTaskRuntime(t, values, &builtRoutes, &clients)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	prepared, err := sess.prepareRuntimeForTaskRoute(ctx, "ping", llmutil.RoutePurposeMainLoop, "", "chat-turn")
	if prepared != nil {
		t.Fatalf("prepared = %#v, want nil", prepared)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestPrepareChatRuntimeAppliesCLIOverridesUntilModelCommandChangesSelection(t *testing.T) {
	values := llmutil.RuntimeValues{Provider: "openai", Model: "route-model"}
	for _, tc := range []struct {
		name             string
		overridesEnabled bool
		wantModel        string
	}{
		{name: "initial selection", overridesEnabled: true, wantModel: "flag-model"},
		{name: "models command selection", overridesEnabled: false, wantModel: "route-model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := New(Dependencies{})
			if err := cmd.Flags().Set("model", "flag-model"); err != nil {
				t.Fatalf("Set(model) error = %v", err)
			}
			var builtRoutes []llmutil.ResolvedRoute
			var clients []*chatWeightedStubClient
			sess := &chatSession{
				cmd:                    cmd,
				sessionStore:           llmselect.NewStore(),
				llmValues:              values,
				clientOverridesEnabled: tc.overridesEnabled,
			}
			sess.taskRuntime = newWeightedChatTaskRuntime(t, values, &builtRoutes, &clients)

			prepared, err := sess.prepareRuntimeForTaskRoute(context.Background(), "ping", llmutil.RoutePurposeMainLoop, "", "chat-turn")
			if err != nil {
				t.Fatalf("prepareRuntimeForTaskRoute() error = %v", err)
			}
			defer func() { _ = prepared.Cleanup() }()
			if prepared.Model != tc.wantModel {
				t.Fatalf("prepared model = %q, want %q", prepared.Model, tc.wantModel)
			}
		})
	}
}

func TestChatModelCommandDisablesCLIOverridesBeforeRebuild(t *testing.T) {
	values := llmutil.RuntimeValues{
		Provider: "openai",
		Model:    "default-model",
		Profiles: map[string]llmutil.ProfileConfig{
			"cheap": {Model: "cheap-model"},
		},
	}
	cmd := New(Dependencies{})
	if err := cmd.Flags().Set("model", "flag-model"); err != nil {
		t.Fatalf("Set(model) error = %v", err)
	}
	var builtRoutes []llmutil.ResolvedRoute
	var clients []*chatWeightedStubClient
	sess := &chatSession{
		cmd:                    cmd,
		rootContext:            context.Background(),
		sessionStore:           llmselect.NewStore(),
		llmValues:              values,
		clientOverridesEnabled: true,
	}
	sess.taskRuntime = newWeightedChatTaskRuntime(t, values, &builtRoutes, &clients)

	output, handled, err := chatModelCommand(sess)("/models set cheap")
	if err != nil {
		t.Fatalf("chatModelCommand() error = %v", err)
	}
	if !handled {
		t.Fatal("chatModelCommand() handled = false")
	}
	if sess.clientOverridesEnabled {
		t.Fatal("clientOverridesEnabled remains true after model selection")
	}
	if got := sess.mainCfg.Model; got != "cheap-model" {
		t.Fatalf("active model = %q, want cheap-model", got)
	}
	if !strings.Contains(output, "[active model: cheap-model]") {
		t.Fatalf("output = %q, want selected profile model", output)
	}
}

func TestPrepareChatRuntimeDoesNotReapplyCLIOverridesAfterWeightedReset(t *testing.T) {
	values := llmutil.RuntimeValues{
		Provider: "openai",
		Model:    "default-model",
		Profiles: map[string]llmutil.ProfileConfig{
			"main_a": {Model: "main-a-model"},
			"main_b": {Model: "main-b-model"},
		},
		Routes: llmutil.RoutesConfig{PurposeRoutes: llmutil.PurposeRoutes{
			MainLoop: llmutil.RoutePolicyConfig{Candidates: []llmutil.RouteCandidateConfig{
				{Profile: "main_a", Weight: 1},
				{Profile: "main_b", Weight: 1},
			}},
		}},
	}
	cmd := New(Dependencies{})
	if err := cmd.Flags().Set("model", "flag-model"); err != nil {
		t.Fatalf("Set(model) error = %v", err)
	}
	var builtRoutes []llmutil.ResolvedRoute
	var clients []*chatWeightedStubClient
	sess := &chatSession{
		cmd:                    cmd,
		sessionStore:           llmselect.NewStore(),
		llmValues:              values,
		clientOverridesEnabled: false,
	}
	sess.taskRuntime = newWeightedChatTaskRuntime(t, values, &builtRoutes, &clients)

	prepared, err := sess.prepareRuntimeForTaskRoute(context.Background(), "ping", llmutil.RoutePurposeMainLoop, "", "chat-turn")
	if err != nil {
		t.Fatalf("prepareRuntimeForTaskRoute() error = %v", err)
	}
	defer func() { _ = prepared.Cleanup() }()
	if prepared.Model == "flag-model" {
		t.Fatalf("prepared model = %q, CLI override was reapplied after reset", prepared.Model)
	}
}
