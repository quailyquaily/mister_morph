package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/quailyquaily/mistermorph/llm"
)

const (
	maxCheckpointJSONBytes        = 64 * 1024
	maxCheckpointStringBytes      = 16 * 1024
	checkpointContinueInstruction = "Continue from this checkpoint. Do not repeat completed work."
)

type checkpointReferences struct {
	Files       []string `json:"files"`
	Directories []string `json:"directories"`
	URLs        []string `json:"urls"`
}

type checkpointProgress struct {
	Completed  []string `json:"completed"`
	InProgress []string `json:"in_progress"`
	Pending    []string `json:"pending"`
}

type checkpointContent struct {
	Summary             string               `json:"summary"`
	UserIntent          []string             `json:"user_intent"`
	References          checkpointReferences `json:"references"`
	Progress            checkpointProgress   `json:"progress"`
	IntermediateResults []string             `json:"intermediate_results"`
}

type checkpointRuntimeContext struct {
	Kind string `json:"kind"`
	checkpointContent
}

type checkpointValidationOptions struct {
	RequireUserIntent      bool
	RequiredFileReferences []string
}

type checkpointMessageEnvelope struct {
	RuntimeContext checkpointRuntimeContext `json:"runtime_context"`
	Instruction    string                   `json:"instruction"`
}

func parseCheckpointContent(raw []byte, opts checkpointValidationOptions) (checkpointContent, error) {
	if len(raw) == 0 {
		return checkpointContent{}, fmt.Errorf("checkpoint JSON is empty")
	}
	if len(raw) > maxCheckpointJSONBytes {
		return checkpointContent{}, fmt.Errorf("checkpoint JSON is too large: %d bytes", len(raw))
	}
	if err := validateCheckpointFieldPresence(raw); err != nil {
		return checkpointContent{}, err
	}

	var content checkpointContent
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&content); err != nil {
		return checkpointContent{}, fmt.Errorf("decode checkpoint JSON: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return checkpointContent{}, err
	}
	normalizeCheckpointContent(&content)
	if err := validateCheckpointContent(content, opts); err != nil {
		return checkpointContent{}, err
	}
	return content, nil
}

func validateCheckpointFieldPresence(raw []byte) error {
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(raw, &outer); err != nil {
		return fmt.Errorf("decode checkpoint JSON: %w", err)
	}
	for _, field := range []string{"summary", "user_intent", "references", "progress", "intermediate_results"} {
		value, ok := outer[field]
		if !ok || len(bytes.TrimSpace(value)) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("checkpoint field %q is required", field)
		}
	}
	if err := requireNestedCheckpointFields(outer["references"], "references", []string{"files", "directories", "urls"}); err != nil {
		return err
	}
	return requireNestedCheckpointFields(outer["progress"], "progress", []string{"completed", "in_progress", "pending"})
}

func requireNestedCheckpointFields(raw json.RawMessage, parent string, fields []string) error {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return fmt.Errorf("checkpoint field %q must be an object: %w", parent, err)
	}
	for _, field := range fields {
		value, ok := values[field]
		if !ok || len(bytes.TrimSpace(value)) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("checkpoint field %q is required", parent+"."+field)
		}
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode checkpoint JSON suffix: %w", err)
	}
	return fmt.Errorf("checkpoint JSON contains more than one value")
}

func normalizeCheckpointContent(content *checkpointContent) {
	if content == nil {
		return
	}
	content.Summary = strings.TrimSpace(content.Summary)
	trimStringSlice(content.UserIntent)
	trimStringSlice(content.References.Files)
	trimStringSlice(content.References.Directories)
	trimStringSlice(content.References.URLs)
	trimStringSlice(content.Progress.Completed)
	trimStringSlice(content.Progress.InProgress)
	trimStringSlice(content.Progress.Pending)
	trimStringSlice(content.IntermediateResults)
}

func trimStringSlice(values []string) {
	for i := range values {
		values[i] = strings.TrimSpace(values[i])
	}
}

func validateCheckpointContent(content checkpointContent, opts checkpointValidationOptions) error {
	if content.UserIntent == nil || content.References.Files == nil || content.References.Directories == nil || content.References.URLs == nil ||
		content.Progress.Completed == nil || content.Progress.InProgress == nil || content.Progress.Pending == nil || content.IntermediateResults == nil {
		return fmt.Errorf("checkpoint array fields must use JSON arrays, not null")
	}
	if content.Summary == "" && !hasNonEmptyString(content.UserIntent) {
		return fmt.Errorf("checkpoint summary and user_intent cannot both be empty")
	}
	if opts.RequireUserIntent && !hasNonEmptyString(content.UserIntent) {
		return fmt.Errorf("checkpoint user_intent is required for compacted user messages")
	}
	if err := validateCheckpointStringLengths(content); err != nil {
		return err
	}
	files := make(map[string]struct{}, len(content.References.Files))
	for _, path := range content.References.Files {
		if path != "" {
			files[path] = struct{}{}
		}
	}
	for _, required := range opts.RequiredFileReferences {
		required = strings.TrimSpace(required)
		if required == "" {
			continue
		}
		if _, ok := files[required]; !ok {
			return fmt.Errorf("checkpoint is missing required file reference %q", required)
		}
	}
	return nil
}

func validateCheckpointStringLengths(content checkpointContent) error {
	values := []string{content.Summary}
	values = append(values, content.UserIntent...)
	values = append(values, content.References.Files...)
	values = append(values, content.References.Directories...)
	values = append(values, content.References.URLs...)
	values = append(values, content.Progress.Completed...)
	values = append(values, content.Progress.InProgress...)
	values = append(values, content.Progress.Pending...)
	values = append(values, content.IntermediateResults...)
	for _, value := range values {
		if len(value) > maxCheckpointStringBytes {
			return fmt.Errorf("checkpoint string is too large: %d bytes", len(value))
		}
	}
	return nil
}

func hasNonEmptyString(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func buildCheckpointMessage(content checkpointContent) (llm.Message, error) {
	if err := validateCheckpointContent(content, checkpointValidationOptions{}); err != nil {
		return llm.Message{}, err
	}
	raw, err := json.Marshal(checkpointMessageEnvelope{
		RuntimeContext: checkpointRuntimeContext{
			Kind:              "context_checkpoint",
			checkpointContent: content,
		},
		Instruction: checkpointContinueInstruction,
	})
	if err != nil {
		return llm.Message{}, fmt.Errorf("encode checkpoint message: %w", err)
	}
	if len(raw) > maxCheckpointJSONBytes {
		return llm.Message{}, fmt.Errorf("checkpoint JSON is too large: %d bytes", len(raw))
	}
	return llm.Message{Role: "user", Content: string(raw)}, nil
}

func replaceMessagesWithCheckpoint(messages []llm.Message, fixedMessageCount int, selection transcriptSelection, checkpoint llm.Message) ([]llm.Message, error) {
	if fixedMessageCount < 0 || fixedMessageCount > len(messages) {
		return nil, fmt.Errorf("fixed message count %d is out of range", fixedMessageCount)
	}
	if selection.Start < fixedMessageCount || selection.Start < 0 || selection.End > len(messages) || selection.End <= selection.Start {
		return nil, fmt.Errorf("invalid compaction selection [%d,%d)", selection.Start, selection.End)
	}
	if normalizedMessageRole(checkpoint.Role) != "user" || strings.TrimSpace(checkpoint.Content) == "" || checkpoint.ToolCallID != "" || len(checkpoint.ToolCalls) > 0 {
		return nil, fmt.Errorf("checkpoint must be one non-empty user message")
	}

	out := make([]llm.Message, 0, len(messages)-(selection.End-selection.Start)+1)
	out = append(out, messages[:selection.Start]...)
	out = append(out, checkpoint)
	out = append(out, messages[selection.End:]...)
	if err := validateCompleteToolExchanges(out, fixedMessageCount); err != nil {
		return nil, err
	}
	return out, nil
}

func validateCompleteToolExchanges(messages []llm.Message, fixedMessageCount int) error {
	blocks := buildTranscriptBlocks(messages, transcriptBlockOptions{FixedMessageCount: fixedMessageCount})
	for _, block := range blocks {
		if block.Reason == transcriptBlockReasonInvalidTool {
			return fmt.Errorf("incomplete tool exchange at messages [%d,%d)", block.Start, block.End)
		}
	}
	return nil
}
