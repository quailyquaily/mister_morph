package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/domainjournal"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

const defaultIntegrationTaskTarget = "integration"

const (
	integrationTaskJournalDomain     = "task"
	integrationTaskJournalTaskUpsert = "task_upsert"
	integrationTaskJournalTaskUpdate = "task_update"
)

type RunTaskOptions struct {
	Agent agent.RunOptions

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
	if rt == nil {
		return RunTaskResult{}, fmt.Errorf("runtime is nil")
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

	var err error
	var taskJournal *domainjournal.Journal
	var taskTrigger daemonruntime.TaskTrigger
	var taskInfo daemonruntime.TaskInfo
	if opts.PersistTask {
		taskJournal, err = rt.newIntegrationTaskJournal()
		if err != nil {
			return result, err
		}
		defer func() {
			_ = taskJournal.Close()
		}()
		taskTrigger = daemonruntime.TaskTrigger{
			Source:  defaultIntegrationTaskTarget,
			Event:   "run_task",
			Ref:     taskID,
			TraceID: traceID,
		}
		now := time.Now().UTC()
		taskInfo = daemonruntime.TaskInfo{
			ID:        taskID,
			Status:    daemonruntime.TaskQueued,
			Task:      task,
			Model:     runOpts.Model,
			CreatedAt: now,
			TopicID:   topicID,
		}
		if err := appendIntegrationTaskEvent(taskJournal, integrationTaskJournalTaskUpsert, now, taskTrigger, taskInfo); err != nil {
			return result, err
		}
	}

	prepared, err := rt.NewRunEngine(ctx, task)
	if err != nil {
		if taskJournal != nil {
			err = errors.Join(err, appendIntegrationTaskFailed(taskJournal, taskTrigger, taskInfo, err, false))
		}
		return result, err
	}
	defer func() {
		_ = prepared.Cleanup()
	}()

	if strings.TrimSpace(runOpts.Model) == "" {
		runOpts.Model = prepared.Model
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
			runErr = errors.Join(runErr, appendIntegrationTaskFailed(taskJournal, taskTrigger, taskInfo, runErr, daemonruntime.IsContextDeadline(ctx, runErr)))
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
	fileStateDir := strings.TrimSpace(snap.Registry.PathRoots.FileStateDir)
	journalDir := pathutil.ResolveStateChildDir(fileStateDir, snap.Registry.JournalDirName, "journal")
	return domainjournal.New(domainjournal.JournalOptions{
		Dir:            journalDir,
		RotateMaxBytes: snap.Registry.TasksRotateMaxBytes,
		SyncEachWrite:  true,
	})
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

func appendIntegrationTaskRunning(journal *domainjournal.Journal, trigger daemonruntime.TaskTrigger, task daemonruntime.TaskInfo) (daemonruntime.TaskInfo, error) {
	startedAt := time.Now().UTC()
	task.Status = daemonruntime.TaskRunning
	task.StartedAt = &startedAt
	err := appendIntegrationTaskEvent(journal, integrationTaskJournalTaskUpdate, startedAt, trigger, task)
	return task, err
}

func appendIntegrationTaskDone(journal *domainjournal.Journal, trigger daemonruntime.TaskTrigger, task daemonruntime.TaskInfo, final *agent.Final) error {
	finishedAt := time.Now().UTC()
	output := daemonruntime.TruncateUTF8(integrationFinalOutput(final), 4000)
	task.Status = daemonruntime.TaskDone
	task.FinishedAt = &finishedAt
	task.Result = map[string]any{"output": output}
	return appendIntegrationTaskEvent(journal, integrationTaskJournalTaskUpdate, finishedAt, trigger, task)
}

func appendIntegrationTaskFailed(journal *domainjournal.Journal, trigger daemonruntime.TaskTrigger, task daemonruntime.TaskInfo, taskErr error, canceled bool) error {
	finishedAt := time.Now().UTC()
	status := daemonruntime.TaskFailed
	if canceled {
		status = daemonruntime.TaskCanceled
	}
	msg := ""
	if taskErr != nil {
		msg = strings.TrimSpace(taskErr.Error())
	}
	task.Status = status
	task.Error = msg
	task.FinishedAt = &finishedAt
	return appendIntegrationTaskEvent(journal, integrationTaskJournalTaskUpdate, finishedAt, trigger, task)
}

type integrationTaskJournalPayload struct {
	At      time.Time                  `json:"at"`
	Target  string                     `json:"target,omitempty"`
	Trigger *daemonruntime.TaskTrigger `json:"trigger,omitempty"`
	Task    *daemonruntime.TaskInfo    `json:"task,omitempty"`
}

func appendIntegrationTaskEvent(journal *domainjournal.Journal, eventType string, now time.Time, trigger daemonruntime.TaskTrigger, task daemonruntime.TaskInfo) error {
	if journal == nil {
		return nil
	}
	task.ID = strings.TrimSpace(task.ID)
	if task.ID == "" {
		return nil
	}
	task.Task = strings.TrimSpace(task.Task)
	task.Model = strings.TrimSpace(task.Model)
	task.TopicID = strings.TrimSpace(task.TopicID)
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now.UTC()
	} else {
		task.CreatedAt = task.CreatedAt.UTC()
	}
	payload := integrationTaskJournalPayload{
		At:     now.UTC(),
		Target: defaultIntegrationTaskTarget,
		Task:   &task,
	}
	if integrationHasTaskTrigger(trigger) {
		payload.Trigger = &trigger
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode integration task journal payload: %w", err)
	}
	_, err = journal.Append(domainjournal.Event{
		ID:            llmstats.NewSyntheticRunID("evt"),
		Time:          now.UTC().Format(time.RFC3339Nano),
		Domain:        integrationTaskJournalDomain,
		Type:          eventType,
		SchemaVersion: 1,
		Trace: domainjournal.Trace{
			TraceID: strings.TrimSpace(trigger.TraceID),
			Runtime: defaultIntegrationTaskTarget,
			Target:  defaultIntegrationTaskTarget,
			TopicID: task.TopicID,
			TaskID:  task.ID,
		},
		Payload: raw,
	})
	return err
}

func integrationHasTaskTrigger(trigger daemonruntime.TaskTrigger) bool {
	return strings.TrimSpace(trigger.Source) != "" ||
		strings.TrimSpace(trigger.Event) != "" ||
		strings.TrimSpace(trigger.Ref) != "" ||
		strings.TrimSpace(trigger.TraceID) != ""
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
