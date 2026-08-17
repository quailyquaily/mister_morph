package taskruntime

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/contextcheckpoint"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/quailyquaily/mistermorph/tools/builtin"
)

type stubTaskRuntimeClient struct {
	requests []llm.Request
	result   llm.Result
}

type stubTaskRuntimeImageClient struct{}

type lifecycleTaskRuntimeClient struct {
	closeCalls int
}

func (*lifecycleTaskRuntimeClient) Chat(context.Context, llm.Request) (llm.Result, error) {
	return llm.Result{Text: `{"type":"final","output":"ok"}`}, nil
}

func (c *lifecycleTaskRuntimeClient) Close() error {
	c.closeCalls++
	return nil
}

type lifecycleTaskRuntimeDecorator struct {
	base llm.Client
}

func (c *lifecycleTaskRuntimeDecorator) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	return c.base.Chat(ctx, req)
}

func (c *lifecycleTaskRuntimeDecorator) Close() error {
	closer, ok := c.base.(interface{ Close() error })
	if !ok {
		return nil
	}
	return closer.Close()
}

type lifecycleTaskRuntimeImageClient struct {
	closeCalls int
}

type lifecycleTaskRuntimeAuditSink struct {
	closeCalls int
	closeErr   error
}

func (*lifecycleTaskRuntimeAuditSink) Emit(context.Context, guard.AuditEvent) error {
	return nil
}

func (s *lifecycleTaskRuntimeAuditSink) Close() error {
	s.closeCalls++
	return s.closeErr
}

func (*lifecycleTaskRuntimeImageClient) GenerateImage(context.Context, llm.ImageRequest) (llm.ImageResult, error) {
	return llm.ImageResult{}, nil
}

func (*lifecycleTaskRuntimeImageClient) EditImage(context.Context, llm.ImageEditRequest) (llm.ImageResult, error) {
	return llm.ImageResult{}, nil
}

func (c *lifecycleTaskRuntimeImageClient) Close() error {
	c.closeCalls++
	return nil
}

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

func TestRuntimeCloseClosesBootstrapClientsOnce(t *testing.T) {
	for _, tc := range []struct {
		name       string
		sameClient bool
	}{
		{name: "distinct clients"},
		{name: "same client", sameClient: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mainClient := &lifecycleTaskRuntimeClient{}
			planClient := &lifecycleTaskRuntimeClient{}
			rt, err := Bootstrap(lifecycleTaskRuntimeDeps(func(route llmutil.ResolvedRoute) (llm.Client, error) {
				if tc.sameClient || route.Purpose == llmutil.RoutePurposeMainLoop {
					return mainClient, nil
				}
				return planClient, nil
			}), BootstrapOptions{})
			if err != nil {
				t.Fatalf("Bootstrap() error = %v", err)
			}

			var wg sync.WaitGroup
			for range 8 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_ = rt.Close()
				}()
			}
			wg.Wait()

			if mainClient.closeCalls != 1 {
				t.Fatalf("main close calls = %d, want 1", mainClient.closeCalls)
			}
			wantPlanCloseCalls := 1
			if tc.sameClient {
				wantPlanCloseCalls = 0
			}
			if planClient.closeCalls != wantPlanCloseCalls {
				t.Fatalf("plan close calls = %d, want %d", planClient.closeCalls, wantPlanCloseCalls)
			}
		})
	}
}

func TestRuntimeCloseClosesSharedGuardOnce(t *testing.T) {
	sink := &lifecycleTaskRuntimeAuditSink{closeErr: errors.New("close guard audit")}
	deps := lifecycleTaskRuntimeDeps(func(llmutil.ResolvedRoute) (llm.Client, error) {
		return &lifecycleTaskRuntimeClient{}, nil
	})
	deps.Guard = func(*slog.Logger) (*guard.Guard, error) {
		return guard.New(guard.Config{Enabled: true}, sink, nil), nil
	}

	rt, err := Bootstrap(deps, BootstrapOptions{})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	for range 2 {
		err = rt.Close()
		if !errors.Is(err, sink.closeErr) {
			t.Fatalf("Close() error = %v, want guard close error", err)
		}
	}
	if sink.closeCalls != 1 {
		t.Fatalf("guard close calls = %d, want 1", sink.closeCalls)
	}
}

func TestNewRunPreparerPropagatesGuardInitializationError(t *testing.T) {
	guardErr := errors.New("initialize guard")
	deps := lifecycleTaskRuntimeDeps(func(llmutil.ResolvedRoute) (llm.Client, error) {
		return &lifecycleTaskRuntimeClient{}, nil
	})
	deps.Guard = func(*slog.Logger) (*guard.Guard, error) {
		return nil, guardErr
	}

	rt, err := NewRunPreparer(deps, BootstrapOptions{})
	if rt != nil {
		t.Fatalf("NewRunPreparer() runtime = %#v, want nil", rt)
	}
	if !errors.Is(err, guardErr) {
		t.Fatalf("NewRunPreparer() error = %v, want guard initialization error", err)
	}
}

func TestNewRunPreparerClosesPartialGuardOnInitializationError(t *testing.T) {
	initErr := errors.New("initialize guard")
	sink := &lifecycleTaskRuntimeAuditSink{closeErr: errors.New("close partial guard")}
	deps := lifecycleTaskRuntimeDeps(func(llmutil.ResolvedRoute) (llm.Client, error) {
		return &lifecycleTaskRuntimeClient{}, nil
	})
	deps.Guard = func(*slog.Logger) (*guard.Guard, error) {
		return guard.New(guard.Config{Enabled: true}, sink, nil), initErr
	}

	rt, err := NewRunPreparer(deps, BootstrapOptions{})
	if rt != nil {
		t.Fatalf("NewRunPreparer() runtime = %#v, want nil", rt)
	}
	if !errors.Is(err, initErr) || !errors.Is(err, sink.closeErr) {
		t.Fatalf("NewRunPreparer() error = %v, want initialization and cleanup errors", err)
	}
	if sink.closeCalls != 1 {
		t.Fatalf("partial guard close calls = %d, want 1", sink.closeCalls)
	}
}

func TestRunPreparerDefersClientsAndPreparedEngineOwnsCleanup(t *testing.T) {
	client := &lifecycleTaskRuntimeClient{}
	createCalls := 0
	deps := lifecycleTaskRuntimeDeps(func(route llmutil.ResolvedRoute) (llm.Client, error) {
		createCalls++
		return client, nil
	})

	rt, err := NewRunPreparer(deps, BootstrapOptions{})
	if err != nil {
		t.Fatalf("NewRunPreparer() error = %v", err)
	}
	if createCalls != 0 {
		t.Fatalf("client create calls after NewRunPreparer() = %d, want 0", createCalls)
	}

	prepared, err := rt.PrepareEngine(context.Background(), RunRequest{Task: "ping"})
	if err != nil {
		t.Fatalf("PrepareEngine() error = %v", err)
	}
	if prepared.Engine == nil || prepared.Route.ClientConfig.Model == "" || prepared.Model != prepared.Route.ClientConfig.Model {
		t.Fatalf("prepared engine = %#v", prepared)
	}
	if createCalls != 2 {
		t.Fatalf("client create calls after PrepareEngine() = %d, want main and plan clients", createCalls)
	}
	if err := prepared.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if err := prepared.Cleanup(); err != nil {
		t.Fatalf("second Cleanup() error = %v", err)
	}
	if client.closeCalls != 1 {
		t.Fatalf("client close calls = %d, want 1", client.closeCalls)
	}
}

func TestRunRequestHookRunsOnPreparedEngine(t *testing.T) {
	client := &lifecycleTaskRuntimeClient{}
	rt, err := NewRunPreparer(lifecycleTaskRuntimeDeps(func(llmutil.ResolvedRoute) (llm.Client, error) {
		return client, nil
	}), BootstrapOptions{AgentConfig: agent.Config{MaxSteps: 1}})
	if err != nil {
		t.Fatalf("NewRunPreparer() error = %v", err)
	}
	defer func() { _ = rt.Close() }()

	hookCalls := 0
	result, err := rt.Run(context.Background(), RunRequest{
		Task: "ping",
		Hook: func(context.Context, int, *agent.Context, *[]llm.Message) error {
			hookCalls++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Final == nil || result.Final.Output != "ok" {
		t.Fatalf("Run() final = %#v, want output ok", result.Final)
	}
	if hookCalls != 1 {
		t.Fatalf("hook calls = %d, want 1", hookCalls)
	}
}

func TestPreparedEngineExposesLoadedSkills(t *testing.T) {
	deps := lifecycleTaskRuntimeDeps(func(llmutil.ResolvedRoute) (llm.Client, error) {
		return &lifecycleTaskRuntimeClient{}, nil
	})
	deps.PromptSpec = func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
		return agent.DefaultPromptSpec(), []string{"repo-skill"}, nil
	}
	rt, err := NewRunPreparer(deps, BootstrapOptions{})
	if err != nil {
		t.Fatalf("NewRunPreparer() error = %v", err)
	}
	defer func() { _ = rt.Close() }()

	prepared, err := rt.PrepareEngine(context.Background(), RunRequest{Task: "ping"})
	if err != nil {
		t.Fatalf("PrepareEngine() error = %v", err)
	}
	defer func() { _ = prepared.Cleanup() }()
	if got := strings.Join(prepared.LoadedSkills, ","); got != "repo-skill" {
		t.Fatalf("LoadedSkills = %q, want repo-skill", got)
	}
}

func TestPrepareRunRefreshesAgentSendRegistration(t *testing.T) {
	deps := lifecycleTaskRuntimeDeps(func(llmutil.ResolvedRoute) (llm.Client, error) {
		return &lifecycleTaskRuntimeClient{}, nil
	})
	deps.Registry = func() *tools.Registry {
		reg := tools.NewRegistry()
		if err := reg.Register(builtin.NewAgentSendTool(builtin.ContactsSendToolOptions{Enabled: true})); err != nil {
			t.Fatalf("Register() error = %v", err)
		}
		return reg
	}
	var refreshCalled bool
	deps.RegisterTriggeredStaticTools = func(reg *tools.Registry, triggers map[string]bool) {
		refreshCalled = true
		if !triggers[toolsutil.BuiltinAgentSend] {
			t.Fatal("agent_send availability trigger is missing")
		}
		if _, ok := reg.Get(toolsutil.BuiltinAgentSend); ok {
			t.Fatal("stale agent_send remained before availability refresh")
		}
	}
	rt, err := NewRunPreparer(deps, BootstrapOptions{})
	if err != nil {
		t.Fatalf("NewRunPreparer() error = %v", err)
	}
	defer func() { _ = rt.Close() }()

	prepared, err := rt.PrepareEngine(context.Background(), RunRequest{Task: "ping"})
	if err != nil {
		t.Fatalf("PrepareEngine() error = %v", err)
	}
	defer func() { _ = prepared.Cleanup() }()
	if !refreshCalled {
		t.Fatal("agent_send availability was not refreshed")
	}
}

func TestRunRequestForwardsToolCallbacksToPreparedEngine(t *testing.T) {
	client := &approvalResumeClient{}
	tool := &approvalResumeTool{}
	deps := lifecycleTaskRuntimeDeps(func(llmutil.ResolvedRoute) (llm.Client, error) {
		return client, nil
	})
	deps.Registry = func() *tools.Registry {
		reg := tools.NewRegistry()
		if err := reg.Register(tool); err != nil {
			t.Fatalf("Register() error = %v", err)
		}
		return reg
	}
	rt, err := NewRunPreparer(deps, BootstrapOptions{AgentConfig: agent.Config{MaxSteps: 2}})
	if err != nil {
		t.Fatalf("NewRunPreparer() error = %v", err)
	}
	defer func() { _ = rt.Close() }()

	var starts, callStarts, callDone int
	result, err := rt.Run(context.Background(), RunRequest{
		Task: "ping",
		OnToolStart: func(*agent.Context, string) {
			starts++
		},
		OnToolCallStart: func(*agent.Context, agent.ToolCall) {
			callStarts++
		},
		OnToolCallDone: func(*agent.Context, agent.ToolCall, string, error) {
			callDone++
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Final == nil || result.Final.Output != "done" {
		t.Fatalf("Run() final = %#v, want done", result.Final)
	}
	if starts != 1 || callStarts != 1 || callDone != 1 {
		t.Fatalf("callback calls = start:%d call_start:%d call_done:%d, want 1 each", starts, callStarts, callDone)
	}
}

func TestRunRequestUsesImageDependenciesFromOneRunSnapshot(t *testing.T) {
	defaultImageCreates := 0
	overrideImage := &lifecycleTaskRuntimeImageClient{}
	deps := lifecycleTaskRuntimeDeps(func(llmutil.ResolvedRoute) (llm.Client, error) {
		return &lifecycleTaskRuntimeClient{}, nil
	})
	deps.ToolTriggers = func(string) map[string]bool {
		return map[string]bool{toolsutil.BuiltinImageGenerate: true}
	}
	deps.CreateImageClient = func() (llm.ImageClient, error) {
		defaultImageCreates++
		return &lifecycleTaskRuntimeImageClient{}, nil
	}
	rt, err := NewRunPreparer(deps, BootstrapOptions{})
	if err != nil {
		t.Fatalf("NewRunPreparer() error = %v", err)
	}
	defer func() { _ = rt.Close() }()
	rt.ImageClient = stubTaskRuntimeImageClient{}

	overrideCreates := 0
	configured := toolsutil.RuntimeToolsRegisterConfig{Image: toolsutil.ImageToolsRegisterConfig{
		Configured: true,
		Provider:   "openai",
		Model:      "image-model",
	}}
	prepared, err := rt.PrepareEngine(context.Background(), RunRequest{
		Task:               "draw a cat",
		RuntimeToolsConfig: &configured,
		CreateImageClient: func() (llm.ImageClient, error) {
			overrideCreates++
			return overrideImage, nil
		},
	})
	if err != nil {
		t.Fatalf("PrepareEngine() error = %v", err)
	}
	if defaultImageCreates != 0 || overrideCreates != 1 {
		t.Fatalf("image creates = default:%d override:%d, want 0/1", defaultImageCreates, overrideCreates)
	}
	if err := prepared.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if overrideImage.closeCalls != 1 {
		t.Fatalf("override image close calls = %d, want 1", overrideImage.closeCalls)
	}
	if rt.commonDeps.RuntimeToolsConfig.Image.Configured {
		t.Fatal("per-run RuntimeToolsConfig was written back to Runtime")
	}
}

func TestRunRequestClosesPartialImageClientOnFactoryError(t *testing.T) {
	partial := &lifecycleTaskRuntimeImageClient{}
	createErr := errors.New("image unavailable")
	deps := lifecycleTaskRuntimeDeps(func(llmutil.ResolvedRoute) (llm.Client, error) {
		return &lifecycleTaskRuntimeClient{}, nil
	})
	deps.ToolTriggers = func(string) map[string]bool {
		return map[string]bool{toolsutil.BuiltinImageGenerate: true}
	}
	rt, err := NewRunPreparer(deps, BootstrapOptions{})
	if err != nil {
		t.Fatalf("NewRunPreparer() error = %v", err)
	}
	defer func() { _ = rt.Close() }()

	configured := toolsutil.RuntimeToolsRegisterConfig{Image: toolsutil.ImageToolsRegisterConfig{Configured: true}}
	prepared, err := rt.PrepareEngine(context.Background(), RunRequest{
		Task:               "draw a cat",
		RuntimeToolsConfig: &configured,
		CreateImageClient: func() (llm.ImageClient, error) {
			return partial, createErr
		},
	})
	if err != nil {
		t.Fatalf("PrepareEngine() error = %v, image creation remains best effort", err)
	}
	defer func() { _ = prepared.Cleanup() }()
	if partial.closeCalls != 1 {
		t.Fatalf("partial image close calls = %d, want 1", partial.closeCalls)
	}
}

func TestRuntimeCloseClosesSharedDecoratedBootstrapClientOnce(t *testing.T) {
	shared := &lifecycleTaskRuntimeClient{}
	rt, err := Bootstrap(lifecycleTaskRuntimeDeps(func(llmutil.ResolvedRoute) (llm.Client, error) {
		return shared, nil
	}), BootstrapOptions{
		ClientDecorator: func(client llm.Client, _ llmutil.ResolvedRoute) llm.Client {
			return &lifecycleTaskRuntimeDecorator{base: client}
		},
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if rt.BootstrapMainClient == rt.PlanClient {
		t.Fatal("decorated main and plan clients should be distinct wrappers")
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if shared.closeCalls != 1 {
		t.Fatalf("shared decorated client close calls = %d, want 1", shared.closeCalls)
	}
}

func TestPreparedCleanupClosesSharedDecoratedTaskClientOnce(t *testing.T) {
	bootstrapMain := &lifecycleTaskRuntimeClient{}
	bootstrapPlan := &lifecycleTaskRuntimeClient{}
	sharedTaskClient := &lifecycleTaskRuntimeClient{}
	createCalls := 0
	rt, err := Bootstrap(lifecycleTaskRuntimeDeps(func(route llmutil.ResolvedRoute) (llm.Client, error) {
		createCalls++
		if createCalls > 2 {
			return sharedTaskClient, nil
		}
		if route.Purpose == llmutil.RoutePurposePlanCreate {
			return bootstrapPlan, nil
		}
		return bootstrapMain, nil
	}), BootstrapOptions{
		ClientDecorator: func(client llm.Client, _ llmutil.ResolvedRoute) llm.Client {
			return &lifecycleTaskRuntimeDecorator{base: client}
		},
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	defer func() { _ = rt.Close() }()

	prepared, err := rt.prepareRun(context.Background(), RunRequest{Task: "ping"})
	if err != nil {
		t.Fatalf("prepareRun() error = %v", err)
	}
	prepared.close()
	prepared.close()
	if sharedTaskClient.closeCalls != 1 {
		t.Fatalf("shared decorated task client close calls = %d, want 1", sharedTaskClient.closeCalls)
	}
}

func TestBootstrapClosesCreatedClientsOnFailure(t *testing.T) {
	t.Run("main route resolution", func(t *testing.T) {
		sink := &lifecycleTaskRuntimeAuditSink{}
		deps := lifecycleTaskRuntimeDeps(func(llmutil.ResolvedRoute) (llm.Client, error) {
			t.Fatal("CreateLLMClient() called after main route resolution failure")
			return nil, nil
		})
		deps.Guard = func(*slog.Logger) (*guard.Guard, error) {
			return guard.New(guard.Config{Enabled: true}, sink, nil), nil
		}
		deps.ResolveLLMRoute = func(string) (llmutil.ResolvedRoute, error) {
			return llmutil.ResolvedRoute{}, errors.New("resolve main")
		}

		if _, err := Bootstrap(deps, BootstrapOptions{}); err == nil {
			t.Fatal("Bootstrap() error = nil, want failure")
		}
		if sink.closeCalls != 1 {
			t.Fatalf("guard close calls = %d, want 1", sink.closeCalls)
		}
	})

	t.Run("main client creation", func(t *testing.T) {
		sink := &lifecycleTaskRuntimeAuditSink{}
		mainClient := &lifecycleTaskRuntimeClient{}
		deps := lifecycleTaskRuntimeDeps(func(llmutil.ResolvedRoute) (llm.Client, error) {
			return mainClient, errors.New("create main")
		})
		deps.Guard = func(*slog.Logger) (*guard.Guard, error) {
			return guard.New(guard.Config{Enabled: true}, sink, nil), nil
		}

		if _, err := Bootstrap(deps, BootstrapOptions{}); err == nil {
			t.Fatal("Bootstrap() error = nil, want failure")
		}
		if mainClient.closeCalls != 1 {
			t.Fatalf("main close calls = %d, want 1", mainClient.closeCalls)
		}
		if sink.closeCalls != 1 {
			t.Fatalf("guard close calls = %d, want 1", sink.closeCalls)
		}
	})

	t.Run("plan route resolution", func(t *testing.T) {
		sink := &lifecycleTaskRuntimeAuditSink{}
		mainClient := &lifecycleTaskRuntimeClient{}
		deps := lifecycleTaskRuntimeDeps(func(llmutil.ResolvedRoute) (llm.Client, error) {
			return mainClient, nil
		})
		deps.Guard = func(*slog.Logger) (*guard.Guard, error) {
			return guard.New(guard.Config{Enabled: true}, sink, nil), nil
		}
		resolve := deps.ResolveLLMRoute
		deps.ResolveLLMRoute = func(purpose string) (llmutil.ResolvedRoute, error) {
			if purpose == llmutil.RoutePurposePlanCreate {
				return llmutil.ResolvedRoute{}, errors.New("resolve plan")
			}
			return resolve(purpose)
		}

		if _, err := Bootstrap(deps, BootstrapOptions{}); err == nil {
			t.Fatal("Bootstrap() error = nil, want failure")
		}
		if mainClient.closeCalls != 1 {
			t.Fatalf("main close calls = %d, want 1", mainClient.closeCalls)
		}
		if sink.closeCalls != 1 {
			t.Fatalf("guard close calls = %d, want 1", sink.closeCalls)
		}
	})

	t.Run("plan client creation", func(t *testing.T) {
		sink := &lifecycleTaskRuntimeAuditSink{}
		mainClient := &lifecycleTaskRuntimeClient{}
		planClient := &lifecycleTaskRuntimeClient{}
		deps := lifecycleTaskRuntimeDeps(func(route llmutil.ResolvedRoute) (llm.Client, error) {
			if route.Purpose == llmutil.RoutePurposePlanCreate {
				return planClient, errors.New("create plan")
			}
			return mainClient, nil
		})
		deps.Guard = func(*slog.Logger) (*guard.Guard, error) {
			return guard.New(guard.Config{Enabled: true}, sink, nil), nil
		}

		if _, err := Bootstrap(deps, BootstrapOptions{}); err == nil {
			t.Fatal("Bootstrap() error = nil, want failure")
		}
		if mainClient.closeCalls != 1 || planClient.closeCalls != 1 {
			t.Fatalf("close calls = main:%d plan:%d, want 1 each", mainClient.closeCalls, planClient.closeCalls)
		}
		if sink.closeCalls != 1 {
			t.Fatalf("guard close calls = %d, want 1", sink.closeCalls)
		}
	})
}

func TestPerTaskImageClientCleanup(t *testing.T) {
	t.Run("successful run", func(t *testing.T) {
		imageClient := &lifecycleTaskRuntimeImageClient{}
		rt := bootstrapImageLifecycleRuntime(t, func() (llm.ImageClient, error) {
			return imageClient, nil
		}, nil)
		defer func() { _ = rt.Close() }()

		if _, err := rt.Run(context.Background(), RunRequest{Task: "$image_generate draw a diagram"}); err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if imageClient.closeCalls != 1 {
			t.Fatalf("image close calls = %d, want 1", imageClient.closeCalls)
		}
	})

	t.Run("preparation failure", func(t *testing.T) {
		imageClient := &lifecycleTaskRuntimeImageClient{}
		rt := bootstrapImageLifecycleRuntime(t, func() (llm.ImageClient, error) {
			return imageClient, nil
		}, errors.New("build prompt"))
		defer func() { _ = rt.Close() }()

		if _, err := rt.Run(context.Background(), RunRequest{Task: "$image_generate draw a diagram"}); err == nil {
			t.Fatal("Run() error = nil, want failure")
		}
		if imageClient.closeCalls != 1 {
			t.Fatalf("image close calls = %d, want 1", imageClient.closeCalls)
		}
	})

	t.Run("client creation returns client and error", func(t *testing.T) {
		imageClient := &lifecycleTaskRuntimeImageClient{}
		rt := bootstrapImageLifecycleRuntime(t, func() (llm.ImageClient, error) {
			return imageClient, errors.New("create image client")
		}, nil)
		defer func() { _ = rt.Close() }()

		if _, err := rt.Run(context.Background(), RunRequest{Task: "$image_generate draw a diagram"}); err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if imageClient.closeCalls != 1 {
			t.Fatalf("image close calls = %d, want 1", imageClient.closeCalls)
		}
	})

	t.Run("repeated prepared cleanup", func(t *testing.T) {
		imageClient := &lifecycleTaskRuntimeImageClient{}
		rt := bootstrapImageLifecycleRuntime(t, func() (llm.ImageClient, error) {
			return imageClient, nil
		}, nil)
		defer func() { _ = rt.Close() }()

		prepared, err := rt.prepareRun(context.Background(), RunRequest{Task: "$image_generate draw a diagram"})
		if err != nil {
			t.Fatalf("prepareRun() error = %v", err)
		}
		prepared.close()
		prepared.close()
		if imageClient.closeCalls != 1 {
			t.Fatalf("image close calls = %d, want 1", imageClient.closeCalls)
		}
	})
}

func lifecycleTaskRuntimeDeps(createClient func(llmutil.ResolvedRoute) (llm.Client, error)) depsutil.CommonDependencies {
	return depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) { return slog.Default(), nil },
		ResolveLLMRoute: func(purpose string) (llmutil.ResolvedRoute, error) {
			profile := "main"
			model := "main-model"
			if purpose == llmutil.RoutePurposePlanCreate {
				profile = "plan"
				model = "plan-model"
			}
			return llmutil.ResolvedRoute{
				Purpose: purpose,
				Profile: profile,
				ClientConfig: llmconfig.ClientConfig{
					Provider: "test",
					Model:    model,
				},
			}, nil
		},
		CreateLLMClient: createClient,
		Registry:        func() *tools.Registry { return tools.NewRegistry() },
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), nil, nil
		},
	}
}

func bootstrapImageLifecycleRuntime(t *testing.T, createImageClient func() (llm.ImageClient, error), promptErr error) *Runtime {
	t.Helper()
	deps := lifecycleTaskRuntimeDeps(func(llmutil.ResolvedRoute) (llm.Client, error) {
		return &lifecycleTaskRuntimeClient{}, nil
	})
	deps.CreateImageClient = createImageClient
	deps.RuntimeToolsConfig = toolsutil.RuntimeToolsRegisterConfig{
		Image: toolsutil.ImageToolsRegisterConfig{
			GenerateEnabled: true,
			Configured:      true,
			Provider:        "openai",
			Model:           "gpt-image-test",
		},
	}
	deps.PromptSpec = func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
		if promptErr != nil {
			return agent.PromptSpec{}, nil, promptErr
		}
		return agent.DefaultPromptSpec(), nil, nil
	}
	rt, err := Bootstrap(deps, BootstrapOptions{AgentConfig: agent.Config{MaxSteps: 2}})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	return rt
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

func TestRunUsesPerTaskLLMProfile(t *testing.T) {
	defaultClient := &stubTaskRuntimeClient{}
	profileClient := &stubTaskRuntimeClient{}
	defaultRoute := llmutil.ResolvedRoute{
		Purpose: llmutil.RoutePurposeMainLoop,
		ClientConfig: llmconfig.ClientConfig{
			Provider: "test",
			Model:    "default-model",
		},
	}
	profileRoute := llmutil.ResolvedRoute{
		Purpose: llmutil.RoutePurposeMainLoop,
		Profile: "cheap",
		ClientConfig: llmconfig.ClientConfig{
			Provider: "test",
			Model:    "profile-model",
		},
	}
	var gotPurpose string
	var gotProfile string
	rt, err := Bootstrap(depsutil.CommonDependencies{
		Logger:     func() (*slog.Logger, error) { return slog.Default(), nil },
		LogOptions: func() agent.LogOptions { return agent.LogOptions{} },
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return defaultRoute, nil
		},
		ResolveLLMRouteWithProfile: func(purpose, profile string) (llmutil.ResolvedRoute, error) {
			gotPurpose = purpose
			gotProfile = profile
			return profileRoute, nil
		},
		CreateLLMClient: func(route llmutil.ResolvedRoute) (llm.Client, error) {
			if route.ClientConfig.Model == "profile-model" {
				return profileClient, nil
			}
			return defaultClient, nil
		},
		Registry: func() *tools.Registry { return tools.NewRegistry() },
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), nil, nil
		},
	}, BootstrapOptions{AgentConfig: agent.Config{MaxSteps: 2}})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	_, err = rt.Run(context.Background(), RunRequest{
		Task:       "ping",
		LLMProfile: "cheap",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if gotPurpose != llmutil.RoutePurposeMainLoop || gotProfile != "cheap" {
		t.Fatalf("profile resolver args = %q/%q, want main_loop/cheap", gotPurpose, gotProfile)
	}
	if len(profileClient.requests) != 1 || profileClient.requests[0].Model != "profile-model" {
		t.Fatalf("profile requests = %#v, want one profile-model request", profileClient.requests)
	}
	if len(defaultClient.requests) != 0 {
		t.Fatalf("default requests = %#v, want none", defaultClient.requests)
	}
}

func TestRunFixesProfileResolverRouteBeforePreparation(t *testing.T) {
	client := &stubTaskRuntimeClient{}
	profileRoute := llmutil.ResolvedRoute{
		Purpose:  llmutil.RoutePurposeMainLoop,
		Identity: "profile-weighted",
		Candidates: []llmutil.ResolvedCandidate{
			{Profile: "profile-a", Weight: 1, ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "profile-a-model"}},
			{Profile: "profile-b", Weight: 1, ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "profile-b-model"}},
		},
	}
	var preparedRoute llmutil.ResolvedRoute
	rt, err := Bootstrap(depsutil.CommonDependencies{
		Logger:     func() (*slog.Logger, error) { return slog.Default(), nil },
		LogOptions: func() agent.LogOptions { return agent.LogOptions{} },
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return llmutil.ResolvedRoute{Profile: "default", ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "default-model"}}, nil
		},
		ResolveLLMRouteWithProfile: func(string, string) (llmutil.ResolvedRoute, error) {
			return profileRoute, nil
		},
		CreateLLMClient: func(route llmutil.ResolvedRoute) (llm.Client, error) {
			if route.Identity == profileRoute.Identity {
				preparedRoute = route
			}
			return client, nil
		},
		Registry: func() *tools.Registry { return tools.NewRegistry() },
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), nil, nil
		},
	}, BootstrapOptions{AgentConfig: agent.Config{MaxSteps: 2}})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	const runID = "profile-run"
	if _, err := rt.Run(llmstats.WithRunID(context.Background(), runID), RunRequest{Task: "ping", LLMProfile: "profile"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := llmutil.SelectRouteCandidate(profileRoute, runID)
	if len(preparedRoute.Candidates) != 0 || preparedRoute.ClientConfig.Model != want.ClientConfig.Model {
		t.Fatalf("prepared route = %#v, want concrete model %q", preparedRoute, want.ClientConfig.Model)
	}
}

func TestRunSelectsWeightedCandidateBeforePreparation(t *testing.T) {
	client := &stubTaskRuntimeClient{}
	route := llmutil.ResolvedRoute{
		Purpose:  llmutil.RoutePurposeMainLoop,
		Identity: "weighted-preparation",
		Candidates: []llmutil.ResolvedCandidate{
			{Profile: "small", Weight: 1, Values: llmutil.RuntimeValues{CacheTTL: "5m"}, ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "small-model", ContextWindowTokens: 1000}},
			{Profile: "large", Weight: 1, Values: llmutil.RuntimeValues{CacheTTL: "1h"}, ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "large-model", ContextWindowTokens: 2000}},
		},
	}
	var preparedRoute llmutil.ResolvedRoute
	var promptModel string
	rt, err := Bootstrap(depsutil.CommonDependencies{
		Logger:          func() (*slog.Logger, error) { return slog.Default(), nil },
		LogOptions:      func() agent.LogOptions { return agent.LogOptions{} },
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) { return route, nil },
		CreateLLMClient: func(got llmutil.ResolvedRoute) (llm.Client, error) {
			if len(got.Candidates) == 0 {
				preparedRoute = got
			}
			return client, nil
		},
		Registry: func() *tools.Registry { return tools.NewRegistry() },
		PromptSpec: func(_ context.Context, _ *slog.Logger, _ agent.LogOptions, _ string, _ llm.Client, model string, _ []string) (agent.PromptSpec, []string, error) {
			promptModel = model
			return agent.DefaultPromptSpec(), nil, nil
		},
	}, BootstrapOptions{AgentConfig: agent.Config{MaxSteps: 2}})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	ctx := llmstats.WithRunID(context.Background(), "weighted-run")
	prepared, err := rt.prepareRun(ctx, RunRequest{Task: "ping"})
	if err != nil {
		t.Fatalf("prepareRun() error = %v", err)
	}
	defer prepared.close()
	if prepared.contextWindowTokens != preparedRoute.ClientConfig.ContextWindowTokens {
		t.Fatalf("context window = %d, selected route = %d", prepared.contextWindowTokens, preparedRoute.ClientConfig.ContextWindowTokens)
	}
	if _, _, err := prepared.engine.Run(ctx, prepared.task, agent.RunOptions{Model: prepared.model, ContextWindowTokens: prepared.contextWindowTokens}); err != nil {
		t.Fatalf("Engine.Run() error = %v", err)
	}
	if len(preparedRoute.Candidates) != 0 || strings.TrimSpace(preparedRoute.Profile) == "" {
		t.Fatalf("prepared route is not concrete: %#v", preparedRoute)
	}
	if len(client.requests) != 1 {
		t.Fatalf("client requests = %d, want 1", len(client.requests))
	}
	if got, want := client.requests[0].Model, preparedRoute.ClientConfig.Model; got != want {
		t.Fatalf("request model = %q, selected model = %q", got, want)
	}
	if promptModel != preparedRoute.ClientConfig.Model {
		t.Fatalf("prompt model = %q, selected model = %q", promptModel, preparedRoute.ClientConfig.Model)
	}
	if len(client.requests[0].Messages) == 0 || len(client.requests[0].Messages[0].Parts) == 0 || client.requests[0].Messages[0].Parts[0].CacheControl == nil {
		t.Fatalf("request system prompt cache control missing: %#v", client.requests[0].Messages)
	}
	if got, want := client.requests[0].Messages[0].Parts[0].CacheControl.TTL, preparedRoute.Values.CacheTTL; got != want {
		t.Fatalf("cache TTL = %q, selected route = %q", got, want)
	}
}

func TestPrepareRunUsesCapturedRouteWithoutResolvingCurrentSelection(t *testing.T) {
	client := &stubTaskRuntimeClient{}
	resolveCalls := 0
	var createdRoute llmutil.ResolvedRoute
	deps := lifecycleTaskRuntimeDeps(func(route llmutil.ResolvedRoute) (llm.Client, error) {
		createdRoute = route
		return client, nil
	})
	deps.ResolveLLMRoute = func(string) (llmutil.ResolvedRoute, error) {
		resolveCalls++
		return llmutil.ResolvedRoute{ClientConfig: llmconfig.ClientConfig{Provider: "changed", Model: "changed-model"}}, nil
	}
	rt, err := NewRunPreparer(deps, BootstrapOptions{AgentConfig: agent.Config{MaxSteps: 1}})
	if err != nil {
		t.Fatalf("NewRunPreparer() error = %v", err)
	}
	captured := llmutil.ResolvedRoute{
		Purpose:  llmutil.RoutePurposeMainLoop,
		Profile:  "captured-profile",
		Identity: "captured-route",
		Values:   llmutil.RuntimeValues{CacheTTL: "1h"},
		ClientConfig: llmconfig.ClientConfig{
			Provider:            "captured",
			Model:               "captured-model",
			ContextWindowTokens: 8192,
		},
	}
	prepared, err := rt.prepareRun(context.Background(), RunRequest{
		Task:                "ping",
		Route:               &captured,
		DisableRuntimeTools: true,
	})
	if err != nil {
		t.Fatalf("prepareRun() error = %v", err)
	}
	defer prepared.close()
	if resolveCalls != 0 {
		t.Fatalf("ResolveLLMRoute calls = %d, want 0", resolveCalls)
	}
	if createdRoute.Identity != captured.Identity || createdRoute.ClientConfig.Model != captured.ClientConfig.Model {
		t.Fatalf("created route = %#v, want captured route %#v", createdRoute, captured)
	}
	if prepared.model != captured.ClientConfig.Model || prepared.contextWindowTokens != captured.ClientConfig.ContextWindowTokens {
		t.Fatalf("prepared model/window = %q/%d, want %q/%d", prepared.model, prepared.contextWindowTokens, captured.ClientConfig.Model, captured.ClientConfig.ContextWindowTokens)
	}
}

func TestRunSelectsWeightedPlanCandidateBeforeRegistration(t *testing.T) {
	client := &stubTaskRuntimeClient{}
	mainRoute := llmutil.ResolvedRoute{
		Purpose:  llmutil.RoutePurposeMainLoop,
		Identity: "main-route",
		Candidates: []llmutil.ResolvedCandidate{
			{Profile: "main-a", Weight: 1, ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "main-a-model"}},
			{Profile: "main-b", Weight: 1, ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "main-b-model"}},
		},
	}
	planRoute := llmutil.ResolvedRoute{
		Purpose:  llmutil.RoutePurposePlanCreate,
		Identity: "plan-route",
		Candidates: []llmutil.ResolvedCandidate{
			{Profile: "plan-a", Weight: 1, ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "plan-a-model"}},
			{Profile: "plan-b", Weight: 1, ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "plan-b-model"}},
		},
	}
	var createdRoutes []llmutil.ResolvedRoute
	rt, err := Bootstrap(depsutil.CommonDependencies{
		Logger:     func() (*slog.Logger, error) { return slog.Default(), nil },
		LogOptions: func() agent.LogOptions { return agent.LogOptions{} },
		ResolveLLMRoute: func(purpose string) (llmutil.ResolvedRoute, error) {
			if purpose == llmutil.RoutePurposePlanCreate {
				return planRoute, nil
			}
			return mainRoute, nil
		},
		CreateLLMClient: func(route llmutil.ResolvedRoute) (llm.Client, error) {
			createdRoutes = append(createdRoutes, route)
			return client, nil
		},
		Registry: func() *tools.Registry { return tools.NewRegistry() },
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), nil, nil
		},
	}, BootstrapOptions{AgentConfig: agent.Config{MaxSteps: 2}})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	const runID = "weighted-plan-run"
	if _, err := rt.Run(llmstats.WithRunID(context.Background(), runID), RunRequest{Task: "ping"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := llmutil.SelectRouteCandidate(planRoute, runID)
	var selected *llmutil.ResolvedRoute
	for idx := range createdRoutes {
		route := &createdRoutes[idx]
		if route.Purpose == llmutil.RoutePurposePlanCreate && len(route.Candidates) == 0 {
			selected = route
		}
	}
	if selected == nil {
		t.Fatalf("created routes = %#v, want concrete plan route", createdRoutes)
	}
	if selected.ClientConfig.Model != want.ClientConfig.Model {
		t.Fatalf("selected plan model = %q, want %q", selected.ClientConfig.Model, want.ClientConfig.Model)
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
		Guard: func(*slog.Logger) (*guard.Guard, error) {
			return g, nil
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
	if strings.TrimSpace(outputfmt.FormatFinalOutput(resumed.Final)) != "done" {
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
		Guard: func(*slog.Logger) (*guard.Guard, error) { return g, nil },
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
	if strings.TrimSpace(outputfmt.FormatFinalOutput(resumed.Final)) != "done" {
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
		Purpose:  llmutil.RoutePurposeThink,
		Identity: "weighted-think",
		Candidates: []llmutil.ResolvedCandidate{
			{
				Profile: "reasoning-a",
				Weight:  1,
				Values:  llmutil.RuntimeValues{ReasoningEffortRaw: "low"},
				ClientConfig: llmconfig.ClientConfig{
					Provider: "openai",
					Model:    "think-a-model",
				},
			},
			{
				Profile: "reasoning-b",
				Weight:  1,
				Values:  llmutil.RuntimeValues{ReasoningEffortRaw: "medium"},
				ClientConfig: llmconfig.ClientConfig{
					Provider: "openai",
					Model:    "think-b-model",
				},
			},
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

	const runID = "weighted-think-run"
	_, err = rt.Run(llmstats.WithRunID(context.Background(), runID), RunRequest{
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
	var runRoute llmutil.ResolvedRoute
	for _, route := range created {
		if route.Purpose == llmutil.RoutePurposeThink {
			runRoute = route
		}
	}
	if runRoute.Purpose != llmutil.RoutePurposeThink {
		t.Fatalf("created routes = %#v, want think route", created)
	}
	wantRoute := llmutil.SelectRouteCandidate(thinkRoute, runID)
	if len(runRoute.Candidates) != 0 || runRoute.ClientConfig.Model != wantRoute.ClientConfig.Model {
		t.Fatalf("run route = %#v, want concrete model %q", runRoute, wantRoute.ClientConfig.Model)
	}
	if runRoute.Values.ReasoningEffortRaw != llmutil.ReasoningEffortXHigh {
		t.Fatalf("run reasoning effort = %q, want xhigh", runRoute.Values.ReasoningEffortRaw)
	}
	if len(client.requests) != 1 {
		t.Fatalf("client requests = %d, want 1", len(client.requests))
	}
	if client.requests[0].Model != wantRoute.ClientConfig.Model {
		t.Fatalf("request model = %q, want %q", client.requests[0].Model, wantRoute.ClientConfig.Model)
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
