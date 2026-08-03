package integration

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/quailyquaily/mistermorph/llm"
)

func TestIntegrationUsageClientWrapReleasesClosedSharedClient(t *testing.T) {
	base := &integrationLifecycleClient{}
	root := t.TempDir()
	wrap := integrationUsageClientWrap(runtimepaths.Paths{
		LLMUsageJournalDir: filepath.Join(root, "usage"),
		TopicContextPath:   filepath.Join(root, "topic_context.json"),
	}, slog.Default())
	cfg := llmconfig.ClientConfig{Provider: "test", Model: "test-model"}

	first := wrap(base, cfg, "")
	firstCloser, ok := first.(io.Closer)
	if !ok {
		t.Fatalf("first wrapped client type %T does not implement io.Closer", first)
	}
	if err := firstCloser.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	second := wrap(base, cfg, "")
	secondCloser, ok := second.(io.Closer)
	if !ok {
		t.Fatalf("second wrapped client type %T does not implement io.Closer", second)
	}
	if err := secondCloser.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if base.closeCalls != 2 {
		t.Fatalf("base close calls = %d, want one close per completed client lifecycle", base.closeCalls)
	}
}

func TestNewRunEngineUsesSharedTodoWorkflowPrompt(t *testing.T) {
	var systemPrompt string
	cfg := DefaultConfig()
	cfg.Features.Skills = false
	cfg.Set("file_state_dir", t.TempDir())
	rt := newRuntime(cfg, runtimeBuildDependencies{
		buildClient: func(llmconfig.ClientConfig, llmutil.RuntimeValues) (llm.Client, error) {
			return &stubIntegrationLLMClient{chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
				for _, message := range req.Messages {
					if message.Role == "system" {
						systemPrompt = message.Content
						break
					}
				}
				return llm.Result{Text: `{"type":"final","output":"ok"}`}, nil
			}}, nil
		},
	})

	if _, _, err := rt.RunTask(context.Background(), "schedule a reminder", agent.RunOptions{}); err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	if !strings.Contains(systemPrompt, "[[ Cron Task Workflow ]]") || !strings.Contains(systemPrompt, "call `todo_update`") {
		t.Fatalf("system prompt is missing todo workflow guidance: %q", systemPrompt)
	}
}

func TestRunTaskUsesDefaultWorkspaceForPromptAndFileTools(t *testing.T) {
	workspaceDir := t.TempDir()
	var systemPrompt string
	requestCount := 0
	cfg := DefaultConfig()
	cfg.Features.Skills = false
	cfg.Set("workspace_dir", workspaceDir)
	cfg.Set("file_state_dir", t.TempDir())
	cfg.Set("file_cache_dir", t.TempDir())
	cfg.Set("tools.write_file.enabled", true)
	rt := newRuntime(cfg, runtimeBuildDependencies{
		buildClient: func(llmconfig.ClientConfig, llmutil.RuntimeValues) (llm.Client, error) {
			return &stubIntegrationLLMClient{chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
				requestCount++
				if requestCount == 1 {
					for _, message := range req.Messages {
						if message.Role == "system" {
							systemPrompt = message.Content
							break
						}
					}
					return llm.Result{ToolCalls: []llm.ToolCall{{
						ID:   "write_default_workspace",
						Name: "write_file",
						Arguments: map[string]any{
							"path":    "artifact.txt",
							"content": "workspace output",
						},
					}}}, nil
				}
				return llm.Result{Text: `{"type":"final","output":"ok"}`}, nil
			}}, nil
		},
	})

	if _, _, err := rt.RunTask(context.Background(), "write an artifact", agent.RunOptions{}); err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	if !strings.Contains(systemPrompt, "workspace_dir: "+workspaceDir) {
		t.Fatalf("system prompt is missing default workspace: %q", systemPrompt)
	}
	data, err := os.ReadFile(filepath.Join(workspaceDir, "artifact.txt"))
	if err != nil {
		t.Fatalf("ReadFile(default workspace artifact) error = %v", err)
	}
	if string(data) != "workspace output" {
		t.Fatalf("artifact content = %q, want workspace output", data)
	}
}

type stubIntegrationLLMClient struct {
	chatFn func(context.Context, llm.Request) (llm.Result, error)
}

func (c *stubIntegrationLLMClient) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	if c == nil || c.chatFn == nil {
		return llm.Result{}, nil
	}
	return c.chatFn(ctx, req)
}

func TestNewRunEngineUsesFallbackProfiles(t *testing.T) {
	buildModels := make([]string, 0, 2)
	requestModels := make([]string, 0, 2)
	buildClient := func(cfg llmconfig.ClientConfig, _ llmutil.RuntimeValues) (llm.Client, error) {
		buildModels = append(buildModels, cfg.Model)
		switch cfg.Model {
		case "gpt-5.2":
			return &stubIntegrationLLMClient{
				chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
					requestModels = append(requestModels, req.Model)
					return llm.Result{}, errors.New("openai API request failed with status 429: too many requests")
				},
			}, nil
		case "gpt-4.1-mini":
			return &stubIntegrationLLMClient{
				chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
					requestModels = append(requestModels, req.Model)
					return llm.Result{Text: `{"type":"final","output":"fallback ok"}`}, nil
				},
			}, nil
		default:
			t.Fatalf("unexpected model build %q", cfg.Model)
			return nil, nil
		}
	}

	cfg := DefaultConfig()
	cfg.Features.Skills = false
	cfg.Set("file_state_dir", t.TempDir())
	cfg.Set("llm.provider", "openai")
	cfg.Set("llm.model", "gpt-5.2")
	cfg.Set("llm.profiles", map[string]any{
		"cheap": map[string]any{
			"model": "gpt-4.1-mini",
		},
	})
	cfg.Set("llm.routes", map[string]any{
		"main_loop": map[string]any{
			"fallback_profiles": []string{"cheap"},
		},
	})

	rt := newRuntime(cfg, runtimeBuildDependencies{buildClient: buildClient})
	prepared, err := rt.NewRunEngine(context.Background(), "ping")
	if err != nil {
		t.Fatalf("NewRunEngine() error = %v", err)
	}
	defer func() { _ = prepared.Cleanup() }()

	final, _, err := prepared.Engine.Run(context.Background(), "ping", agent.RunOptions{Model: prepared.Model})
	if err != nil {
		t.Fatalf("Engine.Run() error = %v", err)
	}
	if final == nil {
		t.Fatal("final is nil")
	}
	if final.Output != "fallback ok" {
		t.Fatalf("final output = %q, want fallback ok", final.Output)
	}
	if len(buildModels) < 2 {
		t.Fatalf("build models = %#v, want at least primary + fallback", buildModels)
	}
	if buildModels[0] != "gpt-5.2" || buildModels[1] != "gpt-4.1-mini" {
		t.Fatalf("build models prefix = %#v, want [gpt-5.2 gpt-4.1-mini]", buildModels[:2])
	}
	if len(requestModels) != 2 {
		t.Fatalf("request models = %#v, want primary + fallback", requestModels)
	}
	if requestModels[0] != "gpt-5.2" || requestModels[1] != "gpt-4.1-mini" {
		t.Fatalf("request models = %#v, want [gpt-5.2 gpt-4.1-mini]", requestModels)
	}
}

func TestSharedDependenciesCreateLLMClientUsesFallbackProfiles(t *testing.T) {
	buildModels := make([]string, 0, 2)
	buildClient := func(cfg llmconfig.ClientConfig, _ llmutil.RuntimeValues) (llm.Client, error) {
		buildModels = append(buildModels, cfg.Model)
		switch cfg.Model {
		case "gpt-5.2":
			return &stubIntegrationLLMClient{
				chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
					if req.Model != "gpt-5.2" {
						t.Fatalf("primary request model = %q, want gpt-5.2", req.Model)
					}
					return llm.Result{}, errors.New("openai API request failed with status 500: upstream error")
				},
			}, nil
		case "gpt-4.1-mini":
			return &stubIntegrationLLMClient{
				chatFn: func(_ context.Context, req llm.Request) (llm.Result, error) {
					if req.Model != "gpt-4.1-mini" {
						t.Fatalf("fallback request model = %q, want gpt-4.1-mini", req.Model)
					}
					return llm.Result{Text: req.Model}, nil
				},
			}, nil
		default:
			t.Fatalf("unexpected model build %q", cfg.Model)
			return nil, nil
		}
	}

	cfg := DefaultConfig()
	cfg.Set("llm.provider", "openai")
	cfg.Set("llm.model", "gpt-5.2")
	cfg.Set("llm.profiles", map[string]any{
		"cheap": map[string]any{
			"model": "gpt-4.1-mini",
		},
	})
	cfg.Set("llm.routes", map[string]any{
		"main_loop": map[string]any{
			"fallback_profiles": []string{"cheap"},
		},
	})

	rt := newRuntime(cfg, runtimeBuildDependencies{buildClient: buildClient})
	snap := rt.snapshot()
	deps := rt.sharedDependencies(snap)
	route, err := deps.ResolveLLMRoute(llmutil.RoutePurposeMainLoop)
	if err != nil {
		t.Fatalf("ResolveLLMRoute() error = %v", err)
	}
	client, err := deps.CreateLLMClient(route)
	if err != nil {
		t.Fatalf("CreateLLMClient() error = %v", err)
	}

	result, err := client.Chat(context.Background(), llm.Request{Model: route.ClientConfig.Model})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if result.Text != "gpt-4.1-mini" {
		t.Fatalf("result text = %q, want gpt-4.1-mini", result.Text)
	}
	if len(buildModels) < 2 {
		t.Fatalf("build models = %#v, want at least primary + fallback", buildModels)
	}
	if buildModels[0] != "gpt-5.2" || buildModels[1] != "gpt-4.1-mini" {
		t.Fatalf("build models prefix = %#v, want [gpt-5.2 gpt-4.1-mini]", buildModels[:2])
	}
}
