package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/tools"
)

const (
	SubtaskStatusDone   = "done"
	SubtaskStatusFailed = "failed"

	SubtaskOutputKindText = "text"
	SubtaskOutputKindJSON = "json"

	subtaskSummaryMaxChars = 160
)

type SubtaskRequest struct {
	Task         string
	Model        string
	OutputSchema string
	Registry     *tools.Registry
	Meta         map[string]any
	RunFunc      SubtaskFunc
}

type SubtaskResult struct {
	TaskID       string `json:"task_id"`
	Status       string `json:"status"`
	Summary      string `json:"summary"`
	OutputKind   string `json:"output_kind"`
	OutputSchema string `json:"output_schema"`
	Output       any    `json:"output"`
	Error        string `json:"error"`
}

type SubtaskRunner interface {
	RunSubtask(ctx context.Context, req SubtaskRequest) (*SubtaskResult, error)
}

type SubtaskFunc func(ctx context.Context) (*SubtaskResult, error)

type subtaskRunnerContextKey struct{}
type subtaskDepthContextKey struct{}

func WithSubtaskRunnerContext(ctx context.Context, runner SubtaskRunner) context.Context {
	if runner == nil {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, subtaskRunnerContextKey{}, runner)
}

func SubtaskRunnerFromContext(ctx context.Context) (SubtaskRunner, bool) {
	if ctx == nil {
		return nil, false
	}
	runner, ok := ctx.Value(subtaskRunnerContextKey{}).(SubtaskRunner)
	return runner, ok && runner != nil
}

func WithSubtaskDepth(ctx context.Context, depth int) context.Context {
	if depth <= 0 {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, subtaskDepthContextKey{}, depth)
}

func SubtaskDepthFromContext(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	v, _ := ctx.Value(subtaskDepthContextKey{}).(int)
	if v < 0 {
		return 0
	}
	return v
}

func BuildSubtaskTask(task string, outputSchema string) string {
	task = strings.TrimSpace(task)
	outputSchema = strings.TrimSpace(outputSchema)
	if outputSchema == "" {
		return task
	}
	var b strings.Builder
	b.WriteString(task)
	if task != "" {
		b.WriteString("\n\n")
	}
	b.WriteString("Final output requirement:\n")
	b.WriteString("- Your final answer MUST set `output` to a JSON value.\n")
	b.WriteString("- That JSON value must match `output_schema=")
	b.WriteString(outputSchema)
	b.WriteString("`.\n")
	b.WriteString("- Do not wrap the JSON value again.\n")
	return b.String()
}

func SubtaskResultFromFinal(taskID string, outputSchema string, final *Final) *SubtaskResult {
	outputSchema = strings.TrimSpace(outputSchema)
	if outputSchema != "" {
		return structuredSubtaskResultFromFinal(taskID, outputSchema, final)
	}

	result := &SubtaskResult{
		TaskID:       strings.TrimSpace(taskID),
		Status:       SubtaskStatusDone,
		Summary:      "subtask completed",
		OutputKind:   SubtaskOutputKindText,
		OutputSchema: "",
		Output:       "",
		Error:        "",
	}
	if final == nil || final.Output == nil {
		return result
	}

	switch v := final.Output.(type) {
	case string:
		out := strings.TrimSpace(v)
		result.OutputKind = SubtaskOutputKindText
		result.Output = out
		if summary := summarizeSubtaskText(out); summary != "" {
			result.Summary = summary
		}
	default:
		result.OutputKind = SubtaskOutputKindJSON
		result.OutputSchema = strings.TrimSpace(outputSchema)
		result.Output = v
		result.Summary = "subtask completed with structured output"
	}
	return result
}

func structuredSubtaskResultFromFinal(taskID string, outputSchema string, final *Final) *SubtaskResult {
	if final == nil || final.Output == nil {
		result := FailedSubtaskResult(taskID, fmt.Errorf("subtask output_schema %q requires JSON output", outputSchema))
		result.OutputSchema = outputSchema
		return result
	}

	value, err := normalizeStructuredSubtaskOutput(final.Output)
	if err != nil {
		result := FailedSubtaskResult(taskID, err)
		result.OutputSchema = outputSchema
		return result
	}

	return &SubtaskResult{
		TaskID:       strings.TrimSpace(taskID),
		Status:       SubtaskStatusDone,
		Summary:      "subtask completed with structured output",
		OutputKind:   SubtaskOutputKindJSON,
		OutputSchema: outputSchema,
		Output:       value,
		Error:        "",
	}
}

func FailedSubtaskResult(taskID string, err error) *SubtaskResult {
	msg := ""
	if err != nil {
		msg = strings.TrimSpace(err.Error())
	}
	summary := "subtask failed"
	if msg != "" {
		summary = daemonruntime.TruncateUTF8(msg, subtaskSummaryMaxChars)
	}
	return &SubtaskResult{
		TaskID:       strings.TrimSpace(taskID),
		Status:       SubtaskStatusFailed,
		Summary:      summary,
		OutputKind:   SubtaskOutputKindText,
		OutputSchema: "",
		Output:       "",
		Error:        msg,
	}
}

func summarizeSubtaskText(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	line := out
	if idx := strings.IndexAny(line, "\r\n"); idx >= 0 {
		line = line[:idx]
	}
	return daemonruntime.TruncateUTF8(strings.TrimSpace(line), subtaskSummaryMaxChars)
}

func normalizeStructuredSubtaskOutput(raw any) (any, error) {
	if raw == nil {
		return nil, fmt.Errorf("subtask structured output is empty")
	}
	s, ok := raw.(string)
	if !ok {
		return raw, nil
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("subtask structured output is empty")
	}
	var value any
	if err := json.Unmarshal([]byte(s), &value); err != nil {
		return nil, fmt.Errorf("subtask output_schema requires JSON output, got non-JSON string")
	}
	return value, nil
}
