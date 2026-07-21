package integration

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/mcphost"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type integrationLifecycleClient struct {
	closeCalls int
	closeErr   error
}

func (*integrationLifecycleClient) Chat(context.Context, llm.Request) (llm.Result, error) {
	return llm.Result{}, nil
}

func (c *integrationLifecycleClient) Close() error {
	c.closeCalls++
	return c.closeErr
}

type integrationLifecycleImageClient struct {
	closeCalls int
	closeErr   error
}

func (*integrationLifecycleImageClient) GenerateImage(context.Context, llm.ImageRequest) (llm.ImageResult, error) {
	return llm.ImageResult{}, nil
}

func (*integrationLifecycleImageClient) EditImage(context.Context, llm.ImageEditRequest) (llm.ImageResult, error) {
	return llm.ImageResult{}, nil
}

func (c *integrationLifecycleImageClient) Close() error {
	c.closeCalls++
	return c.closeErr
}

type integrationLifecycleTool struct {
	name string
}

func (t integrationLifecycleTool) Name() string          { return t.name }
func (integrationLifecycleTool) Description() string     { return "test MCP tool" }
func (integrationLifecycleTool) ParameterSchema() string { return `{}` }
func (integrationLifecycleTool) Execute(context.Context, map[string]any) (string, error) {
	return "ok", nil
}

func TestPreparedRunCleanupOwnsEveryCreatedResourceAndIsIdempotent(t *testing.T) {
	mainCloseErr := errors.New("close main")
	planCloseErr := errors.New("close plan")
	imageCloseErr := errors.New("close image")
	mcpCloseErr := errors.New("close MCP")
	mainClient := &integrationLifecycleClient{closeErr: mainCloseErr}
	planClient := &integrationLifecycleClient{closeErr: planCloseErr}
	imageClient := &integrationLifecycleImageClient{closeErr: imageCloseErr}
	mcpCloseCalls := 0

	cfg := integrationLifecycleConfig(t)
	rt := newRuntime(cfg, runtimeBuildDependencies{
		buildClient: func(cfg llmconfig.ClientConfig, _ llmutil.RuntimeValues) (llm.Client, error) {
			switch cfg.Model {
			case "main-model":
				return mainClient, nil
			case "plan-model":
				return planClient, nil
			default:
				t.Fatalf("unexpected model %q", cfg.Model)
				return nil, nil
			}
		},
		buildImageClient: func(llmutil.RuntimeValues, *slog.Logger) (llm.ImageClient, error) {
			return imageClient, nil
		},
		connectMCP: func(_ context.Context, _ []mcphost.ServerConfig, _ *slog.Logger) (mcpRegistration, error) {
			tool := integrationLifecycleTool{name: "mcp_test"}
			return mcpRegistration{
				tools: []tools.Tool{tool},
				close: func() error {
					mcpCloseCalls++
					return mcpCloseErr
				},
			}, nil
		},
	})

	prepared, err := rt.NewRunEngine(context.Background(), "$image_generate draw a diagram")
	if err != nil {
		t.Fatalf("NewRunEngine() error = %v", err)
	}
	for _, want := range []error{mainCloseErr, planCloseErr, imageCloseErr, mcpCloseErr} {
		if err := prepared.Cleanup(); !errors.Is(err, want) {
			t.Fatalf("Cleanup() error = %v, want errors.Is(%v)", err, want)
		}
	}
	if err := prepared.Cleanup(); !errors.Is(err, mainCloseErr) {
		t.Fatalf("second Cleanup() error = %v, want cached cleanup error", err)
	}
	if mainClient.closeCalls != 1 || planClient.closeCalls != 1 || imageClient.closeCalls != 1 || mcpCloseCalls != 1 {
		t.Fatalf("close calls = main:%d plan:%d image:%d MCP:%d, want 1 each", mainClient.closeCalls, planClient.closeCalls, imageClient.closeCalls, mcpCloseCalls)
	}
}

func TestNewRunEngineRollsBackAcquiredResourcesWhenPlanClientBuildFails(t *testing.T) {
	buildErr := errors.New("build plan")
	mainClient := &integrationLifecycleClient{}
	planClient := &integrationLifecycleClient{}
	imageClient := &integrationLifecycleImageClient{}
	mcpCloseCalls := 0

	rt := newRuntime(integrationLifecycleConfig(t), runtimeBuildDependencies{
		buildClient: func(cfg llmconfig.ClientConfig, _ llmutil.RuntimeValues) (llm.Client, error) {
			if cfg.Model == "plan-model" {
				return planClient, buildErr
			}
			return mainClient, nil
		},
		buildImageClient: func(llmutil.RuntimeValues, *slog.Logger) (llm.ImageClient, error) {
			return imageClient, nil
		},
		connectMCP: func(_ context.Context, _ []mcphost.ServerConfig, _ *slog.Logger) (mcpRegistration, error) {
			return mcpRegistration{close: func() error {
				mcpCloseCalls++
				return nil
			}}, nil
		},
	})

	prepared, err := rt.NewRunEngine(context.Background(), "$image_generate draw a diagram")
	if prepared != nil {
		t.Fatal("NewRunEngine() returned a prepared run on failure")
	}
	if !errors.Is(err, buildErr) {
		t.Fatalf("NewRunEngine() error = %v, want %v", err, buildErr)
	}
	if mainClient.closeCalls != 1 || planClient.closeCalls != 1 || imageClient.closeCalls != 0 || mcpCloseCalls != 1 {
		t.Fatalf("rollback close calls = main:%d plan:%d image:%d MCP:%d, want acquired resources 1 and unacquired image 0", mainClient.closeCalls, planClient.closeCalls, imageClient.closeCalls, mcpCloseCalls)
	}
}

func TestNewRunEngineRollsBackPartialMCPConnection(t *testing.T) {
	connectErr := errors.New("connect MCP")
	closeErr := errors.New("close partial MCP")
	mainClient := &integrationLifecycleClient{}
	mcpCloseCalls := 0
	rt := newRuntime(integrationLifecycleConfig(t), runtimeBuildDependencies{
		buildClient: func(llmconfig.ClientConfig, llmutil.RuntimeValues) (llm.Client, error) {
			return mainClient, nil
		},
		connectMCP: func(context.Context, []mcphost.ServerConfig, *slog.Logger) (mcpRegistration, error) {
			return mcpRegistration{close: func() error {
				mcpCloseCalls++
				return closeErr
			}}, connectErr
		},
	})

	prepared, err := rt.NewRunEngine(context.Background(), "ping")
	if prepared != nil {
		t.Fatal("NewRunEngine() returned a prepared run on failure")
	}
	if !errors.Is(err, connectErr) || !errors.Is(err, closeErr) {
		t.Fatalf("NewRunEngine() error = %v, want connect and cleanup errors", err)
	}
	if mainClient.closeCalls != 0 || mcpCloseCalls != 1 {
		t.Fatalf("rollback close calls = main:%d MCP:%d, want unacquired main 0 and acquired MCP 1", mainClient.closeCalls, mcpCloseCalls)
	}
}

func TestPreparedRunCleanupClosesSharedMainAndPlanClientOnce(t *testing.T) {
	shared := &integrationLifecycleClient{}
	rt := newRuntime(integrationLifecycleConfig(t), runtimeBuildDependencies{
		buildClient: func(llmconfig.ClientConfig, llmutil.RuntimeValues) (llm.Client, error) {
			return shared, nil
		},
		connectMCP: func(context.Context, []mcphost.ServerConfig, *slog.Logger) (mcpRegistration, error) {
			return mcpRegistration{}, nil
		},
	})
	prepared, err := rt.NewRunEngine(context.Background(), "ping")
	if err != nil {
		t.Fatalf("NewRunEngine() error = %v", err)
	}
	if err := prepared.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if shared.closeCalls != 1 {
		t.Fatalf("shared main/plan client close calls = %d, want 1", shared.closeCalls)
	}
}

func TestPreparedRunCleanupClosesSharedInspectedMainAndPlanClientOnce(t *testing.T) {
	shared := &integrationLifecycleClient{}
	cfg := integrationLifecycleConfig(t)
	cfg.Inspect.Prompt = true
	cfg.Inspect.DumpDir = t.TempDir()
	rt := newRuntime(cfg, runtimeBuildDependencies{
		buildClient: func(llmconfig.ClientConfig, llmutil.RuntimeValues) (llm.Client, error) {
			return shared, nil
		},
		connectMCP: func(context.Context, []mcphost.ServerConfig, *slog.Logger) (mcpRegistration, error) {
			return mcpRegistration{}, nil
		},
	})
	prepared, err := rt.NewRunEngine(context.Background(), "ping")
	if err != nil {
		t.Fatalf("NewRunEngine() error = %v", err)
	}
	if err := prepared.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if shared.closeCalls != 1 {
		t.Fatalf("shared inspected main/plan client close calls = %d, want 1", shared.closeCalls)
	}
}

func TestPrepareChannelDependenciesAddsMCPToolsToBothRegistriesAndClosesOnce(t *testing.T) {
	tool := integrationLifecycleTool{name: "mcp_channel_test"}
	closeCalls := 0
	ctx := context.WithValue(context.Background(), struct{}{}, "run-context")
	var gotContext context.Context

	rt := newRuntime(integrationLifecycleConfig(t), runtimeBuildDependencies{
		connectMCP: func(ctx context.Context, _ []mcphost.ServerConfig, _ *slog.Logger) (mcpRegistration, error) {
			gotContext = ctx
			return mcpRegistration{
				tools: []tools.Tool{tool},
				close: func() error {
					closeCalls++
					return nil
				},
			}, nil
		},
	})

	deps, cleanup, err := rt.prepareChannelDependencies(ctx, rt.snapshot())
	if err != nil {
		t.Fatalf("prepareChannelDependencies() error = %v", err)
	}
	if gotContext != ctx {
		t.Fatal("MCP registration did not receive the bot run context")
	}
	for name, reg := range map[string]*tools.Registry{
		"task":      deps.Registry(),
		"awareness": deps.AwarenessRegistry(),
	} {
		if _, ok := reg.Get(tool.Name()); !ok {
			t.Fatalf("%s registry is missing MCP tool %q", name, tool.Name())
		}
	}
	if err := cleanup(); err != nil {
		t.Fatalf("cleanup() error = %v", err)
	}
	if err := cleanup(); err != nil {
		t.Fatalf("second cleanup() error = %v", err)
	}
	if closeCalls != 1 {
		t.Fatalf("MCP close calls = %d, want 1", closeCalls)
	}
}

func TestPrepareChannelDependenciesRejectsSnapshotErrorBeforeConnectingMCP(t *testing.T) {
	initErr := errors.New("invalid runtime config")
	connectCalls := 0
	rt := newRuntime(integrationLifecycleConfig(t), runtimeBuildDependencies{
		connectMCP: func(context.Context, []mcphost.ServerConfig, *slog.Logger) (mcpRegistration, error) {
			connectCalls++
			return mcpRegistration{}, nil
		},
	})
	snap := rt.snapshot()
	snap.InitErr = initErr
	deps, cleanup, err := rt.prepareChannelDependencies(context.Background(), snap)
	if !errors.Is(err, initErr) {
		t.Fatalf("prepareChannelDependencies() error = %v, want %v", err, initErr)
	}
	if cleanup != nil {
		t.Fatal("prepareChannelDependencies() returned cleanup on validation failure")
	}
	if deps.Registry != nil || deps.AwarenessRegistry != nil {
		t.Fatal("prepareChannelDependencies() published dependencies on validation failure")
	}
	if connectCalls != 0 {
		t.Fatalf("MCP connect calls = %d, want 0", connectCalls)
	}
}

func TestPrepareChannelDependenciesClosesMCPRegistrationReturnedWithError(t *testing.T) {
	connectErr := errors.New("connect MCP")
	closeErr := errors.New("close partial MCP")
	closeCalls := 0
	rt := newRuntime(integrationLifecycleConfig(t), runtimeBuildDependencies{
		connectMCP: func(context.Context, []mcphost.ServerConfig, *slog.Logger) (mcpRegistration, error) {
			return mcpRegistration{close: func() error {
				closeCalls++
				return closeErr
			}}, connectErr
		},
	})

	deps, cleanup, err := rt.prepareChannelDependencies(context.Background(), rt.snapshot())
	if !errors.Is(err, connectErr) || !errors.Is(err, closeErr) {
		t.Fatalf("prepareChannelDependencies() error = %v, want connect and cleanup errors", err)
	}
	if cleanup != nil {
		t.Fatal("prepareChannelDependencies() returned cleanup after connect failure")
	}
	if deps.Registry != nil || deps.AwarenessRegistry != nil {
		t.Fatal("prepareChannelDependencies() published dependencies after connect failure")
	}
	if closeCalls != 1 {
		t.Fatalf("partial MCP close calls = %d, want 1", closeCalls)
	}
}

func TestChannelBotRunConnectsMCPWithRunContextAndClosesIt(t *testing.T) {
	for _, channel := range []string{"telegram", "slack"} {
		t.Run(channel, func(t *testing.T) {
			connectCalls := 0
			closeCalls := 0
			var gotContext context.Context
			rt := newRuntime(integrationLifecycleConfig(t), runtimeBuildDependencies{
				buildClient: func(llmconfig.ClientConfig, llmutil.RuntimeValues) (llm.Client, error) {
					return &stubIntegrationLLMClient{}, nil
				},
				connectMCP: func(ctx context.Context, _ []mcphost.ServerConfig, _ *slog.Logger) (mcpRegistration, error) {
					connectCalls++
					gotContext = ctx
					return mcpRegistration{close: func() error {
						closeCalls++
						return nil
					}}, nil
				},
			})

			var runner BotRunner
			var err error
			switch channel {
			case "telegram":
				runner, err = rt.NewTelegramBot(TelegramOptions{BotToken: "test"})
			case "slack":
				runner, err = rt.NewSlackBot(SlackOptions{BotToken: "xoxb-test", AppToken: "xapp-test"})
			}
			if err != nil {
				t.Fatalf("create %s runner: %v", channel, err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_ = runner.Run(ctx)

			if connectCalls != 1 {
				t.Fatalf("MCP connect calls = %d, want 1", connectCalls)
			}
			if gotContext == nil {
				t.Fatal("MCP context is nil")
			}
			if !errors.Is(gotContext.Err(), context.Canceled) {
				t.Fatalf("MCP context error = %v, want context canceled", gotContext.Err())
			}
			if closeCalls != 1 {
				t.Fatalf("MCP close calls = %d, want 1", closeCalls)
			}
		})
	}
}

func integrationLifecycleConfig(t *testing.T) Config {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Features.Skills = false
	cfg.Set("file_state_dir", t.TempDir())
	cfg.Set("file_cache_dir", t.TempDir())
	cfg.Set("llm.provider", "openai")
	cfg.Set("llm.api_key", "test")
	cfg.Set("llm.model", "main-model")
	cfg.Set("llm.image.provider", "openai")
	cfg.Set("llm.image.api_key", "test")
	cfg.Set("llm.image.model", "image-model")
	cfg.Set("llm.profiles", map[string]any{
		"plan": map[string]any{
			"model": "plan-model",
		},
	})
	cfg.Set("llm.routes", map[string]any{
		"plan_create": "plan",
	})
	cfg.Set("telegram.serve_listen", "127.0.0.1:0")
	cfg.Set("slack.serve_listen", "127.0.0.1:0")
	cfg.Set("mcp.servers", []any{
		map[string]any{
			"name":    "test",
			"enable":  true,
			"type":    "stdio",
			"command": "unused",
		},
	})
	return cfg
}
