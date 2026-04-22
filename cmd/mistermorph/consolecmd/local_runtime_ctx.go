package consolecmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/contextbudget"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/memory"
)

func (r *consoleLocalRuntime) handleConsoleCtxCommand(generation *consoleLocalRuntimeGeneration, req daemonruntime.SubmitTaskRequest) (string, bool, error) {
	task := strings.TrimSpace(req.Task)
	if task != "/ctx" {
		return "", false, nil
	}
	if generation == nil || generation.bundle == nil || generation.bundle.taskRuntime == nil {
		return "", true, fmt.Errorf("console task runtime is not initialized")
	}
	topicID := strings.TrimSpace(req.TopicID)
	if topicID == "" {
		return "", true, daemonruntime.BadRequest("topic_id is required for /ctx")
	}
	workspaceDir, err := r.workspaceDirForTopic(context.Background(), topicID)
	if err != nil {
		return "", true, err
	}
	job := consoleLocalTaskJob{
		ConversationKey: buildConsoleConversationKey(topicID),
		TopicID:         topicID,
		Task:            task,
		Model:           strings.TrimSpace(req.Model),
		WorkspaceDir:    workspaceDir,
		Generation:      generation,
	}
	status, err := r.consoleContextStatus(context.Background(), job)
	if err != nil {
		return "", true, err
	}
	return status, true, nil
}

func (r *consoleLocalRuntime) consoleContextStatus(ctx context.Context, job consoleLocalTaskJob) (string, error) {
	if r == nil || job.Generation == nil || job.Generation.bundle == nil || job.Generation.bundle.taskRuntime == nil {
		return "", fmt.Errorf("console runtime is not initialized")
	}
	generation := job.Generation
	runtime := generation.bundle.taskRuntime
	route, err := runtime.ResolveMainRouteForRun()
	if err != nil {
		return "", err
	}
	model := strings.TrimSpace(job.Model)
	if model == "" {
		_, model = defaultLLMConfigForGeneration(generation)
	}
	if model == "" {
		model = strings.TrimSpace(route.ClientConfig.Model)
	}

	mainClient, err := runtime.CreateClientForRoute(route)
	if err != nil {
		return "", err
	}
	defer closeConsoleContextClient(mainClient)

	reg := taskruntime.CloneRegistry(runtime.BaseRegistry)
	toolsutil.RegisterRuntimeTools(reg, generation.commonDeps.RuntimeToolsConfig, toolsutil.RuntimeToolLLMOptions{
		DefaultClient:    mainClient,
		DefaultModel:     model,
		PlanCreateClient: runtime.PlanClient,
		PlanCreateModel:  runtime.PlanModel,
	})

	promptSpec, _, err := depsutil.PromptSpecFromCommon(generation.commonDeps, ctx, runtime.Logger, runtime.LogOptions, "", mainClient, model, nil)
	if err != nil {
		return "", err
	}
	promptprofile.ApplyPersonaIdentity(&promptSpec, runtime.Logger)
	promptprofile.AppendLocalToolNotesBlock(&promptSpec, runtime.Logger)
	promptprofile.AppendPlanCreateGuidanceBlock(&promptSpec, reg)
	promptprofile.AppendTodoWorkflowBlock(&promptSpec, reg)
	if block := workspace.PromptBlock(job.WorkspaceDir); strings.TrimSpace(block.Content) != "" {
		promptSpec.Blocks = append([]agent.PromptBlock{block}, promptSpec.Blocks...)
	}
	depsutil.PromptAugmentFromCommon(generation.commonDeps, &promptSpec, reg)
	promptprofile.AppendGPT5PromptPatch(&promptSpec, model, runtime.Logger)

	historyMsgs, _, err := r.buildConsolePromptMessages(job)
	if err != nil {
		return "", err
	}
	messages := []llm.Message{{
		Role:    "system",
		Content: agent.BuildSystemPrompt(reg, promptSpec),
	}}

	memSubjectID := buildConsoleMemorySubjectID(buildConsoleConversationKey(job.TopicID))
	if generation.reader.GetBool("memory.enabled") && generation.memRuntime.Orchestrator != nil && memSubjectID != "" {
		memoryContext, memErr := generation.memRuntime.Orchestrator.PrepareInjection(memoryruntime.PrepareInjectionRequest{
			SubjectID:      memSubjectID,
			RequestContext: memory.ContextPrivate,
			MaxItems:       generation.reader.GetInt("memory.injection.max_items"),
		})
		if memErr != nil && runtime.Logger != nil {
			runtime.Logger.Warn("console_ctx_memory_injection_failed", "error", memErr.Error())
		}
		if memoryMsg, ok := agent.BuildInjectedMemoryMessage(memoryContext); ok {
			messages = append(messages, llm.Message{Role: "user", Content: memoryMsg})
		}
	}
	messages = append(messages, historyMsgs...)

	contextBudgetCfg, estimator, err := contextbudget.BuildAgentContextBudget(
		route.Values,
		route.ClientConfig.Provider,
		model,
	)
	if err != nil {
		return "", err
	}
	status, err := contextbudget.EstimateStatus(contextBudgetCfg, estimator, llm.Request{
		Model:     model,
		Messages:  messages,
		Tools:     agent.BuildLLMTools(reg),
		ForceJSON: true,
	}, 0)
	if err != nil {
		return "", err
	}
	return contextbudget.FormatStatusJSON(status), nil
}

func closeConsoleContextClient(client llm.Client) {
	if client == nil {
		return
	}
	closer, ok := client.(io.Closer)
	if !ok {
		return
	}
	_ = closer.Close()
}
