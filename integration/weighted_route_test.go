package integration

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/llm"
)

func TestNewRunEngineSelectsWeightedMainAndPlanRoutesBeforeBuildingClients(t *testing.T) {
	const runID = "integration-weighted-run"
	var builtModels []string
	cfg := DefaultConfig()
	cfg.Set("file_state_dir", t.TempDir())
	cfg.Set("llm.provider", "openai")
	cfg.Set("llm.model", "default-model")
	cfg.Set("llm.profiles", map[string]any{
		"main_a": map[string]any{"model": "main-a-model", "context_window": "1000"},
		"main_b": map[string]any{"model": "main-b-model", "context_window": "2000"},
		"plan_a": map[string]any{"model": "plan-a-model"},
		"plan_b": map[string]any{"model": "plan-b-model"},
	})
	cfg.Set("llm.routes", map[string]any{
		"main_loop": map[string]any{"candidates": []map[string]any{
			{"profile": "main_a", "weight": 1},
			{"profile": "main_b", "weight": 1},
		}},
		"plan_create": map[string]any{"candidates": []map[string]any{
			{"profile": "plan_a", "weight": 1},
			{"profile": "plan_b", "weight": 1},
		}},
	})
	rt := newRuntime(cfg, runtimeBuildDependencies{
		buildClient: func(cfg llmconfig.ClientConfig, _ llmutil.RuntimeValues) (llm.Client, error) {
			builtModels = append(builtModels, cfg.Model)
			return &stubIntegrationLLMClient{}, nil
		},
	})

	mainRoute, err := llmutil.ResolveRoute(rt.snap.LLMValues, llmutil.RoutePurposeMainLoop)
	if err != nil {
		t.Fatalf("ResolveRoute(main) error = %v", err)
	}
	planRoute, err := llmutil.ResolveRoute(rt.snap.LLMValues, llmutil.RoutePurposePlanCreate)
	if err != nil {
		t.Fatalf("ResolveRoute(plan) error = %v", err)
	}
	wantMain := llmutil.SelectRouteCandidate(mainRoute, runID)
	wantPlan := llmutil.SelectRouteCandidate(planRoute, runID)

	prepared, err := rt.NewRunEngine(llmstats.WithRunID(context.Background(), runID), "ping")
	if err != nil {
		t.Fatalf("NewRunEngine() error = %v", err)
	}
	defer func() { _ = prepared.Cleanup() }()
	if prepared.Model != wantMain.ClientConfig.Model {
		t.Fatalf("prepared model = %q, want %q", prepared.Model, wantMain.ClientConfig.Model)
	}
	if prepared.ContextWindowTokens != wantMain.ClientConfig.ContextWindowTokens {
		t.Fatalf("context window = %d, want %d", prepared.ContextWindowTokens, wantMain.ClientConfig.ContextWindowTokens)
	}
	if len(builtModels) < 4 {
		t.Fatalf("built models = %#v, want main and plan primary/fallback clients", builtModels)
	}
	if builtModels[0] != wantMain.ClientConfig.Model {
		t.Fatalf("first main client model = %q, want %q", builtModels[0], wantMain.ClientConfig.Model)
	}
	planIndex := -1
	for index, model := range builtModels {
		if model == "plan-a-model" || model == "plan-b-model" {
			planIndex = index
			break
		}
	}
	if planIndex < 0 || builtModels[planIndex] != wantPlan.ClientConfig.Model {
		t.Fatalf("built models = %#v, first plan model want %q", builtModels, wantPlan.ClientConfig.Model)
	}
}

func TestRunTaskCreatesRunIDBeforeWeightedRoutePreparation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Features.Skills = false
	cfg.Set("file_state_dir", t.TempDir())
	cfg.Set("llm.provider", "openai")
	cfg.Set("llm.model", "default-model")
	cfg.Set("llm.profiles", map[string]any{
		"main_a": map[string]any{"model": "main-a-model"},
		"main_b": map[string]any{"model": "main-b-model"},
	})
	cfg.Set("llm.routes", map[string]any{
		"main_loop": map[string]any{"candidates": []map[string]any{
			{"profile": "main_a", "weight": 1},
			{"profile": "main_b", "weight": 1},
		}},
	})
	var usedRunID string
	var usedModel string
	rt := newRuntime(cfg, runtimeBuildDependencies{
		buildClient: func(llmconfig.ClientConfig, llmutil.RuntimeValues) (llm.Client, error) {
			return &stubIntegrationLLMClient{chatFn: func(ctx context.Context, req llm.Request) (llm.Result, error) {
				usedRunID = llmstats.RunIDFromContext(ctx)
				usedModel = req.Model
				return llm.Result{Text: `{"type":"final","output":"ok"}`}, nil
			}}, nil
		},
	})

	if _, _, err := rt.RunTask(context.Background(), "ping", agent.RunOptions{}); err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	if !strings.HasPrefix(usedRunID, defaultIntegrationTaskTarget+"_") {
		t.Fatalf("run id = %q, want integration-generated id", usedRunID)
	}
	rawRoute, err := llmutil.ResolveRoute(rt.snap.LLMValues, llmutil.RoutePurposeMainLoop)
	if err != nil {
		t.Fatalf("ResolveRoute() error = %v", err)
	}
	wantModel := llmutil.SelectRouteCandidate(rawRoute, usedRunID).ClientConfig.Model
	if usedModel != wantModel {
		t.Fatalf("run model = %q, want candidate %q selected by run id %q", usedModel, wantModel, usedRunID)
	}
}

func TestRunTaskWithOptionsPersistsSelectedWeightedModelFromQueuedState(t *testing.T) {
	stateDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Features.Skills = false
	cfg.Set("file_state_dir", stateDir)
	cfg.Set("llm.provider", "openai")
	cfg.Set("llm.model", "default-model")
	cfg.Set("llm.profiles", map[string]any{
		"main_a": map[string]any{"model": "main-a-model"},
		"main_b": map[string]any{"model": "main-b-model"},
	})
	cfg.Set("llm.routes", map[string]any{
		"main_loop": map[string]any{"candidates": []map[string]any{
			{"profile": "main_a", "weight": 1},
			{"profile": "main_b", "weight": 1},
		}},
	})
	rt := newRuntime(cfg, runtimeBuildDependencies{
		buildClient: func(llmconfig.ClientConfig, llmutil.RuntimeValues) (llm.Client, error) {
			return &stubIntegrationLLMClient{chatFn: func(context.Context, llm.Request) (llm.Result, error) {
				return llm.Result{Text: `{"type":"final","output":"ok"}`}, nil
			}}, nil
		},
	})
	const taskID = "integration-weighted-task"

	if _, err := rt.RunTaskWithOptions(context.Background(), "ping", RunTaskOptions{TaskID: taskID, PersistTask: true}); err != nil {
		t.Fatalf("RunTaskWithOptions() error = %v", err)
	}
	rawRoute, err := llmutil.ResolveRoute(rt.snap.LLMValues, llmutil.RoutePurposeMainLoop)
	if err != nil {
		t.Fatalf("ResolveRoute() error = %v", err)
	}
	wantModel := llmutil.SelectRouteCandidate(rawRoute, taskID).ClientConfig.Model
	events := replayIntegrationTaskEvents(t, filepath.Join(stateDir, "journal"))
	if len(events) != 3 {
		t.Fatalf("task event count = %d, want 3", len(events))
	}
	for index, event := range events {
		task := decodeTaskJournalPayload(t, event.Event.Payload).Task
		if task == nil || task.Model != wantModel {
			t.Fatalf("event %d task = %#v, want selected model %q", index, task, wantModel)
		}
	}
}
