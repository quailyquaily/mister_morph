package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/domainjournal"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/taskdomain"
	"github.com/quailyquaily/mistermorph/internal/textutil"
)

const defaultIntegrationTaskTarget = "integration"

type RunTaskOptions struct {
	Agent agent.RunOptions
	// LLMProfile selects an LLM profile for this task only. Empty follows the runtime route.
	LLMProfile string

	// TaskID is the persisted task id. Empty uses Agent.Meta["task_id"], then an auto id.
	TaskID string
	// TopicID is optional. Empty uses Agent.Meta["topic_id"], then stays empty.
	TopicID string
	// TraceID is optional. Empty uses Agent.Meta["trace_id"], then stays empty.
	TraceID string

	// PersistTask writes the one-shot run lifecycle into the task journal.
	PersistTask bool
}

type RunTaskResult struct {
	Final   *agent.Final
	Context *agent.Context

	TaskID  string
	RunID   string
	TopicID string
	TraceID string
}

func (rt *Runtime) RunTaskWithOptions(ctx context.Context, task string, opts RunTaskOptions) (RunTaskResult, error) {
	if err := rt.Err(); err != nil {
		return RunTaskResult{}, err
	}
	runOpts := opts.Agent
	taskID := firstNonEmpty(opts.TaskID, metaString(runOpts.Meta, "task_id"))
	if taskID == "" {
		taskID = llmstats.NewSyntheticRunID(defaultIntegrationTaskTarget)
	}
	runID := taskID
	topicID := firstNonEmpty(opts.TopicID, metaString(runOpts.Meta, "topic_id"))
	traceID := firstNonEmpty(opts.TraceID, metaString(runOpts.Meta, "trace_id"))
	result := RunTaskResult{
		TaskID:  taskID,
		RunID:   runID,
		TopicID: topicID,
		TraceID: traceID,
	}

	runOpts.Meta = integrationRunMeta(runOpts.Meta, taskID, runID, topicID, traceID)
	ctx = llmstats.WithRunID(ctx, runID)
	profile := strings.TrimSpace(opts.LLMProfile)
	if profile != "" || strings.TrimSpace(runOpts.Model) == "" {
		if route, routeErr := rt.resolveRunMainRoute(ctx, rt.snapshot(), profile); routeErr == nil {
			runOpts.Model = strings.TrimSpace(route.ClientConfig.Model)
		}
	}

	var err error
	var taskJournal *domainjournal.Journal
	var taskTrigger taskdomain.TaskTrigger
	var taskInfo taskdomain.TaskInfo
	if opts.PersistTask {
		taskJournal, err = rt.newIntegrationTaskJournal()
		if err != nil {
			return result, err
		}
		defer func() {
			_ = taskJournal.Close()
		}()
		taskTrigger = taskdomain.TaskTrigger{
			Source:  defaultIntegrationTaskTarget,
			Event:   "run_task",
			Ref:     taskID,
			TraceID: traceID,
		}
		now := time.Now().UTC()
		taskInfo = taskdomain.TaskInfo{
			ID:         taskID,
			Status:     taskdomain.TaskQueued,
			Task:       task,
			Model:      runOpts.Model,
			LLMProfile: strings.TrimSpace(opts.LLMProfile),
			CreatedAt:  now,
			TopicID:    topicID,
		}
		if _, err := taskdomain.AppendJournalEvent(taskJournal, defaultIntegrationTaskTarget, taskdomain.JournalTypeTaskUpsert, now, taskTrigger, &taskInfo, nil); err != nil {
			return result, err
		}
	}

	prepared, err := rt.newRunEngineWithRegistry(ctx, task, nil, profile)
	if err != nil {
		if taskJournal != nil {
			err = errors.Join(err, appendIntegrationTaskFailed(taskJournal, taskTrigger, taskInfo, err, false))
		}
		return result, err
	}
	defer func() {
		_ = prepared.Cleanup()
	}()

	if profile != "" || strings.TrimSpace(runOpts.Model) == "" {
		runOpts.Model = prepared.Model
	}
	if runOpts.ContextWindowTokens <= 0 {
		runOpts.ContextWindowTokens = prepared.ContextWindowTokens
	}
	if taskJournal != nil {
		taskInfo.Model = runOpts.Model
		next, err := appendIntegrationTaskRunning(taskJournal, taskTrigger, taskInfo)
		if err != nil {
			return result, err
		}
		taskInfo = next
	}

	final, agentCtx, runErr := prepared.Engine.Run(ctx, task, runOpts)
	result.Final = final
	result.Context = agentCtx
	if runErr != nil {
		if taskJournal != nil {
			runErr = errors.Join(runErr, appendIntegrationTaskFailed(taskJournal, taskTrigger, taskInfo, runErr, taskdomain.EndedByCancellation(ctx, runErr)))
		}
		return result, runErr
	}
	if taskJournal != nil {
		if err := appendIntegrationTaskDone(taskJournal, taskTrigger, taskInfo, final); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (rt *Runtime) newIntegrationTaskJournal() (*domainjournal.Journal, error) {
	snap := rt.snapshot()
	return taskdomain.NewJournal(snap.Paths.JournalDir, snap.Registry.TasksRotateMaxBytes)
}

func integrationRunMeta(base map[string]any, taskID, runID, topicID, traceID string) map[string]any {
	out := make(map[string]any, len(base)+4)
	for k, v := range base {
		if strings.TrimSpace(k) == "" {
			continue
		}
		out[k] = v
	}
	out["task_id"] = taskID
	out["run_id"] = runID
	if topicID != "" {
		out["topic_id"] = topicID
	}
	if traceID != "" {
		out["trace_id"] = traceID
	}
	return out
}

func appendIntegrationTaskRunning(journal *domainjournal.Journal, trigger taskdomain.TaskTrigger, task taskdomain.TaskInfo) (taskdomain.TaskInfo, error) {
	startedAt := time.Now().UTC()
	task.Status = taskdomain.TaskRunning
	task.StartedAt = &startedAt
	_, err := taskdomain.AppendJournalEvent(journal, defaultIntegrationTaskTarget, taskdomain.JournalTypeTaskUpdate, startedAt, trigger, &task, nil)
	return task, err
}

func appendIntegrationTaskDone(journal *domainjournal.Journal, trigger taskdomain.TaskTrigger, task taskdomain.TaskInfo, final *agent.Final) error {
	finishedAt := time.Now().UTC()
	output := textutil.TruncateRunes(integrationFinalOutput(final), 4000)
	task.Status = taskdomain.TaskDone
	task.FinishedAt = &finishedAt
	task.Result = map[string]any{"output": output}
	_, err := taskdomain.AppendJournalEvent(journal, defaultIntegrationTaskTarget, taskdomain.JournalTypeTaskUpdate, finishedAt, trigger, &task, nil)
	return err
}

func appendIntegrationTaskFailed(journal *domainjournal.Journal, trigger taskdomain.TaskTrigger, task taskdomain.TaskInfo, taskErr error, canceled bool) error {
	finishedAt := time.Now().UTC()
	status := taskdomain.TaskFailed
	if canceled {
		status = taskdomain.TaskCanceled
	}
	msg := ""
	if taskErr != nil {
		msg = strings.TrimSpace(taskErr.Error())
	}
	task.Status = status
	task.Error = msg
	task.FinishedAt = &finishedAt
	_, err := taskdomain.AppendJournalEvent(journal, defaultIntegrationTaskTarget, taskdomain.JournalTypeTaskUpdate, finishedAt, trigger, &task, nil)
	return err
}

func integrationFinalOutput(final *agent.Final) string {
	if final == nil || final.Output == nil {
		return ""
	}
	if s, ok := final.Output.(string); ok {
		return strings.TrimSpace(s)
	}
	raw, err := json.Marshal(final.Output)
	if err == nil {
		return string(raw)
	}
	return fmt.Sprint(final.Output)
}

func metaString(meta map[string]any, key string) string {
	if len(meta) == 0 {
		return ""
	}
	if v, ok := meta[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
