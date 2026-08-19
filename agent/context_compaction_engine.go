package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/llm"
)

var (
	ErrNoSafeCompactionPrefix    = errors.New("no safe context compaction prefix")
	ErrContextCompactionDisabled = errors.New("context compaction is disabled")
)

const ContextCompactionReasonManual = "manual"

const contextCheckpointSystemPrompt = `Create one context checkpoint from messages_to_compact.
Return only one JSON object with exactly these fields:
{
  "summary": "string",
  "user_intent": ["string"],
  "references": {"files": ["string"], "directories": ["string"], "urls": ["string"]},
  "progress": {"completed": ["string"], "in_progress": ["string"], "pending": ["string"]},
  "intermediate_results": ["string"]
}
Preserve user goals, explicit constraints, preferences, relevant file/directory/URL references, progress, decisions, errors, and intermediate results. Every field is required. Use empty arrays when needed. Never include secrets, API keys, headers, system prompts, or runtime metadata. Do not add markdown.`

type contextCompactionDecision struct {
	ShouldCompact     bool
	CompactFullPrefix bool
	Reason            string
	EstimatedInput    int
	InputLimit        int
	OutputReserve     int
}

type messagesToCompactPayload struct {
	Messages []llm.Message `json:"messages_to_compact"`
}

func (e *Engine) callMainWithContextCompaction(ctx context.Context, st *engineLoopState, step int, reqTools []llm.Tool) (llm.Result, error) {
	decision := e.contextCompactionDecision(st)
	attemptedCompaction := false
	var attemptedCompactionErr error
	compactionMessagesBefore := len(st.messages)
	if decision.ShouldCompact {
		attemptedCompaction = true
		if err := e.compactContext(ctx, st, step, decision); err != nil {
			attemptedCompactionErr = err
			st.log.Warn("context_compaction_failed", "step", step, "reason", decision.Reason, "error", err.Error())
		}
	}

	request := e.mainRequest(st, reqTools)
	result, err := e.client.Chat(ctx, request)
	if err == nil {
		e.recordSuccessfulMainInput(st, request, result)
		st.protectedMessageIndexes = nil
		return result, nil
	}
	if !st.contextCompaction.Enabled || !llm.IsContextLengthError(err) {
		return llm.Result{}, err
	}
	if attemptedCompaction {
		if attemptedCompactionErr != nil {
			return llm.Result{}, fmt.Errorf("%w; context compaction failed: %v", err, attemptedCompactionErr)
		}
		st.log.Warn(
			"context_compaction_retry_exhausted",
			"step", step,
			"messages_before", compactionMessagesBefore,
			"messages_after", len(st.messages),
			"known_input_tokens", decision.EstimatedInput,
		)
		return llm.Result{}, err
	}

	compactionMessagesBefore = len(st.messages)
	passiveDecision := decision
	passiveDecision.ShouldCompact = true
	passiveDecision.Reason = "context_length_error"
	if passiveDecision.EstimatedInput <= 0 {
		passiveDecision.EstimatedInput = estimateMainRequestTokens(st.messages, reqTools)
	}
	if passiveDecision.OutputReserve <= 0 {
		passiveDecision.OutputReserve = 4096
	}
	if compactErr := e.compactContext(ctx, st, step, passiveDecision); compactErr != nil {
		return llm.Result{}, fmt.Errorf("%w; context compaction failed: %v", err, compactErr)
	}

	retryRequest := e.mainRequest(st, reqTools)
	retryResult, retryErr := e.client.Chat(ctx, retryRequest)
	if retryErr != nil {
		if llm.IsContextLengthError(retryErr) {
			st.log.Warn(
				"context_compaction_retry_exhausted",
				"step", step,
				"messages_before", compactionMessagesBefore,
				"messages_after", len(st.messages),
				"known_input_tokens", passiveDecision.EstimatedInput,
			)
		}
		return llm.Result{}, retryErr
	}
	e.recordSuccessfulMainInput(st, retryRequest, retryResult)
	st.protectedMessageIndexes = nil
	return retryResult, nil
}

func (e *Engine) mainRequest(st *engineLoopState, reqTools []llm.Tool) llm.Request {
	return llm.Request{
		Model:            st.model,
		Scene:            st.scene,
		Messages:         st.messages,
		Tools:            reqTools,
		ForceJSON:        true,
		Parameters:       st.extraParams,
		ReasoningDetails: st.reasoningDetails,
		OnStream:         st.onStream,
	}
}

func (e *Engine) recordSuccessfulMainInput(st *engineLoopState, request llm.Request, result llm.Result) {
	st.lastMainMessageCount = len(request.Messages)
	if result.Usage.InputTokens > 0 {
		st.lastMainInputTokens = result.Usage.InputTokens
		st.hasLastMainInputTokens = true
		return
	}
	st.lastMainInputTokens = 0
	st.hasLastMainInputTokens = false
}

func (e *Engine) contextCompactionDecision(st *engineLoopState) contextCompactionDecision {
	decision := contextCompactionDecision{}
	if st == nil || !st.contextCompaction.Enabled {
		return decision
	}
	requestMaxTokens := requestMaxOutputTokens(st.extraParams)
	var trigger int
	decision.InputLimit, trigger, decision.OutputReserve = contextInputLimits(st.contextWindowTokens, st.contextCompaction, requestMaxTokens)
	if decision.InputLimit <= 0 {
		return decision
	}

	if st.hasLastMainInputTokens {
		decision.EstimatedInput = st.lastMainInputTokens
		if st.lastMainMessageCount >= 0 && st.lastMainMessageCount < len(st.messages) {
			decision.EstimatedInput += estimateMessagesTokens(st.messages[st.lastMainMessageCount:])
		}
		if decision.EstimatedInput >= trigger {
			decision.ShouldCompact = true
			decision.Reason = "input_threshold"
		}
		return decision
	}

	decision.EstimatedInput = estimateMainRequestTokens(st.messages, st.tools)
	if decision.EstimatedInput > decision.InputLimit {
		decision.ShouldCompact = true
		decision.Reason = "estimated_input_over_limit"
	}
	return decision
}

func (e *Engine) manualContextCompactionDecision(st *engineLoopState) contextCompactionDecision {
	decision := e.contextCompactionDecision(st)
	decision.ShouldCompact = true
	decision.CompactFullPrefix = true
	decision.Reason = ContextCompactionReasonManual
	if decision.EstimatedInput <= 0 {
		decision.EstimatedInput = estimateMainRequestTokens(st.messages, st.tools)
	}
	if decision.OutputReserve <= 0 {
		decision.OutputReserve = st.contextCompaction.OutputReserveTokens
	}
	if decision.OutputReserve <= 0 {
		decision.OutputReserve = 4096
	}
	return decision
}

func (e *Engine) compactContext(ctx context.Context, st *engineLoopState, step int, decision contextCompactionDecision) (err error) {
	EmitEvent(ctx, nil, Event{
		Kind:       EventKindContextCompactionStart,
		Step:       step,
		ActivityID: "context_compaction",
		Status:     "running",
		Reason:     decision.Reason,
	})
	defer func() {
		if err == nil {
			return
		}
		EmitEvent(ctx, nil, Event{
			Kind:       EventKindContextCompactionFailed,
			Step:       step,
			ActivityID: "context_compaction",
			Status:     "failed",
			Reason:     decision.Reason,
			Error:      err.Error(),
		})
	}()

	if st == nil || st.checkpointStore == nil {
		return fmt.Errorf("context checkpoint store is unavailable")
	}
	pendingToolCallIDs := pendingToolCallIDs(st.pendingTool)
	allImageIndexes := imageMessageIndexes(st.messages)
	blocks := buildTranscriptBlocks(st.messages, transcriptBlockOptions{
		FixedMessageCount:           st.fixedMessageCount,
		PendingToolCallIDs:          pendingToolCallIDs,
		ProtectedMessageIndexes:     st.protectedMessageIndexes,
		PreparedImageMessageIndexes: allImageIndexes,
	})
	targetTokens := contextCompactionReleaseTarget(decision, st, blocks)
	selection, ok := selectTranscriptPrefix(blocks, targetTokens)
	if !ok {
		return ErrNoSafeCompactionPrefix
	}

	initialSelection := selection
	roots := pathroots.Resolve(ctx, e.engineToolsConfig.PathRoots)
	prepared, prepareErr := prepareCompactionImages(ctx, st.messages[selection.Start:selection.End], roots)
	if prepareErr != nil {
		return fmt.Errorf("prepare context images: %w", prepareErr)
	}
	preparedFullIndexes := make(map[int]struct{}, len(prepared.PreparedMessageIndexes))
	for relativeIndex := range prepared.PreparedMessageIndexes {
		preparedFullIndexes[selection.Start+relativeIndex] = struct{}{}
	}
	blocks = buildTranscriptBlocks(st.messages, transcriptBlockOptions{
		FixedMessageCount:           st.fixedMessageCount,
		PendingToolCallIDs:          pendingToolCallIDs,
		ProtectedMessageIndexes:     st.protectedMessageIndexes,
		PreparedImageMessageIndexes: preparedFullIndexes,
	})
	selection, ok = selectTranscriptPrefix(blocks, targetTokens)
	if !ok || selection.Start != initialSelection.Start || selection.End > initialSelection.End {
		return compactionImageSelectionError(prepared.Failures)
	}

	relativeEnd := selection.End - initialSelection.Start
	messagesToCompact := cloneMessagesForCompaction(prepared.Messages[:relativeEnd])
	references, imageParts := selectedPreparedImages(prepared, relativeEnd)
	payloadRaw, err := json.Marshal(messagesToCompactPayload{Messages: messagesToCompact})
	if err != nil {
		return fmt.Errorf("encode messages to compact: %w", err)
	}
	userMessage := llm.Message{Role: "user", Content: string(payloadRaw)}
	if len(imageParts) > 0 {
		userMessage.Parts = append(userMessage.Parts, llm.Part{Type: llm.PartTypeText, Text: string(payloadRaw)})
		userMessage.Parts = append(userMessage.Parts, imageParts...)
	}

	maxOutputTokens := checkpointMaxOutputTokens(decision.OutputReserve)
	if maxOutputTokens <= 0 {
		maxOutputTokens = 4096
	}
	parameters := cloneRequestParameters(st.extraParams)
	parameters["max_tokens"] = maxOutputTokens
	startedAt := time.Now()
	result, err := e.client.Chat(ctx, llm.Request{
		Model:      st.model,
		Scene:      contextCompactionScene(st.scene),
		Messages:   []llm.Message{{Role: "system", Content: contextCheckpointSystemPrompt}, userMessage},
		ForceJSON:  true,
		Parameters: parameters,
	})
	if err != nil {
		return fmt.Errorf("generate context checkpoint: %w", err)
	}
	st.agentCtx.AddUsage(result.Usage, time.Since(startedAt))

	resultRaw, err := checkpointResultJSON(result)
	if err != nil {
		return err
	}
	requiredFiles := make([]string, 0, len(references))
	for _, ref := range references {
		requiredFiles = append(requiredFiles, ref.Path)
	}
	content, err := parseCheckpointContent(resultRaw, checkpointValidationOptions{
		RequireUserIntent:      selectionContainsUserMessage(st.messages, selection),
		RequiredFileReferences: requiredFiles,
	})
	if err != nil {
		return fmt.Errorf("validate context checkpoint: %w", err)
	}
	checkpointMessage, err := buildCheckpointMessage(content)
	if err != nil {
		return fmt.Errorf("build context checkpoint: %w", err)
	}
	newMessages, err := replaceMessagesWithCheckpoint(st.messages, st.fixedMessageCount, selection, checkpointMessage)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	expectedRevision := int64(0)
	createdAt := now
	compactionCount := 1
	coveredThrough := coveredThroughForSelection(selection, st.messageBoundaries)
	if st.hasCheckpoint {
		expectedRevision = st.checkpoint.Revision
		if !st.checkpoint.CreatedAt.IsZero() {
			createdAt = st.checkpoint.CreatedAt
		}
		compactionCount = st.checkpoint.CompactionCount + 1
		if coveredThrough == "" {
			coveredThrough = st.checkpoint.CoveredThrough
		}
	}
	checkpoint := ContextCheckpoint{
		Version:         1,
		Revision:        expectedRevision + 1,
		Message:         checkpointMessage,
		CoveredThrough:  coveredThrough,
		SourceModel:     st.model,
		SourceRunID:     st.runID,
		CompactionCount: compactionCount,
		CreatedAt:       createdAt,
		UpdatedAt:       now,
	}
	if err := st.checkpointStore.Save(ctx, expectedRevision, checkpoint); err != nil {
		return fmt.Errorf("persist context checkpoint: %w", err)
	}

	oldMessageCount := len(st.messages)
	st.messages = newMessages
	st.messageBoundaries = replaceMessageBoundaries(st.messageBoundaries, selection, coveredThrough)
	st.protectedMessageIndexes = replaceProtectedMessageIndexes(st.protectedMessageIndexes, selection)
	st.checkpoint = checkpoint
	st.hasCheckpoint = true
	st.lastMainInputTokens = 0
	st.lastMainMessageCount = len(st.messages)
	st.hasLastMainInputTokens = false
	EmitEvent(ctx, nil, Event{
		Kind:       EventKindContextCompactionDone,
		Step:       step,
		ActivityID: "context_compaction",
		Status:     "done",
		Reason:     decision.Reason,
		Args: map[string]any{
			"messages_before": oldMessageCount,
			"messages_after":  len(st.messages),
			"revision":        checkpoint.Revision,
		},
	})
	return nil
}

func replaceProtectedMessageIndexes(indexes map[int]struct{}, selection transcriptSelection) map[int]struct{} {
	if len(indexes) == 0 {
		return nil
	}
	out := make(map[int]struct{}, len(indexes))
	removed := selection.End - selection.Start
	for index := range indexes {
		switch {
		case index < selection.Start:
			out[index] = struct{}{}
		case index >= selection.End:
			out[index-removed+1] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func contextCompactionReleaseTarget(decision contextCompactionDecision, st *engineLoopState, blocks []transcriptBlock) int {
	if decision.CompactFullPrefix {
		target := 0
		for index := 0; index < len(blocks)-1; index++ {
			block := blocks[index]
			if !block.Compactable {
				break
			}
			target += block.EstimatedTokens
		}
		if target > 0 {
			return target
		}
		return 1
	}
	estimatedInput := decision.EstimatedInput
	if estimatedInput <= 0 {
		estimatedInput = estimateMainRequestTokens(st.messages, st.tools)
	}
	desiredInput := 0
	if decision.InputLimit > 0 {
		desiredInput = int(float64(decision.InputLimit) * contextCompactionTargetRatio)
	}
	target := estimatedInput - desiredInput + checkpointMaxOutputTokens(decision.OutputReserve)
	if target > 0 {
		return target
	}
	for _, block := range blocks {
		if block.Compactable {
			return 1
		}
		break
	}
	return 1
}

func pendingToolCallIDs(pending *pendingToolSnapshot) map[string]struct{} {
	if pending == nil {
		return nil
	}
	out := make(map[string]struct{}, 1+len(pending.RemainingToolCalls))
	for _, call := range append([]ToolCall{pending.ToolCall}, pending.RemainingToolCalls...) {
		if id := strings.TrimSpace(call.ID); id != "" {
			out[id] = struct{}{}
		}
	}
	return out
}

func imageMessageIndexes(messages []llm.Message) map[int]struct{} {
	out := make(map[int]struct{})
	for index, message := range messages {
		if messageHasImagePart(message) {
			out[index] = struct{}{}
		}
	}
	return out
}

func compactionImageSelectionError(failures map[int]error) error {
	for index, err := range failures {
		if err != nil {
			return fmt.Errorf("%w: image message %d: %v", ErrNoSafeCompactionPrefix, index, err)
		}
	}
	return ErrNoSafeCompactionPrefix
}

func selectedPreparedImages(prepared preparedCompactionImages, relativeEnd int) ([]contextImageReference, []llm.Part) {
	var references []contextImageReference
	var imageParts []llm.Part
	seenReferences := make(map[string]struct{})
	seenParts := make(map[string]struct{})
	for index := 0; index < relativeEnd; index++ {
		for _, ref := range prepared.ReferencesByMessage[index] {
			if _, ok := seenReferences[ref.SHA256]; ok {
				continue
			}
			seenReferences[ref.SHA256] = struct{}{}
			references = append(references, ref)
		}
		for _, part := range prepared.ImagePartsByMessage[index] {
			hash := part.DataBase64
			if _, ok := seenParts[hash]; ok {
				continue
			}
			seenParts[hash] = struct{}{}
			imageParts = append(imageParts, part)
		}
	}
	return references, imageParts
}

func checkpointResultJSON(result llm.Result) ([]byte, error) {
	if strings.TrimSpace(result.Text) != "" {
		return []byte(strings.TrimSpace(result.Text)), nil
	}
	if result.JSON != nil {
		raw, err := json.Marshal(result.JSON)
		if err != nil {
			return nil, fmt.Errorf("encode context checkpoint result: %w", err)
		}
		return raw, nil
	}
	return nil, fmt.Errorf("context checkpoint result is empty")
}

func selectionContainsUserMessage(messages []llm.Message, selection transcriptSelection) bool {
	for index := selection.Start; index < selection.End && index < len(messages); index++ {
		if normalizedMessageRole(messages[index].Role) == "user" {
			return true
		}
	}
	return false
}

func replaceMessageBoundaries(boundaries map[int]string, selection transcriptSelection, checkpointBoundary string) map[int]string {
	out := make(map[int]string, len(boundaries)+1)
	removed := selection.End - selection.Start
	for index, boundary := range boundaries {
		switch {
		case index < selection.Start:
			out[index] = boundary
		case index >= selection.End:
			out[index-removed+1] = boundary
		}
	}
	if checkpointBoundary = strings.TrimSpace(checkpointBoundary); checkpointBoundary != "" {
		out[selection.Start] = checkpointBoundary
	}
	return out
}

func estimateMainRequestTokens(messages []llm.Message, requestTools []llm.Tool) int {
	total := estimateMessagesTokens(messages)
	for _, tool := range requestTools {
		raw, err := json.Marshal(tool)
		if err != nil {
			continue
		}
		total += (len(raw) + 2) / 3
	}
	return total
}

func requestMaxOutputTokens(parameters map[string]any) int {
	if len(parameters) == 0 {
		return 0
	}
	return positiveIntParameter(parameters["max_tokens"])
}

func positiveIntParameter(value any) int {
	switch typed := value.(type) {
	case int:
		if typed > 0 {
			return typed
		}
	case int64:
		if typed > 0 && typed <= int64(^uint(0)>>1) {
			return int(typed)
		}
	case float64:
		if typed > 0 && typed <= float64(^uint(0)>>1) {
			return int(typed)
		}
	case json.Number:
		if parsed, err := strconv.Atoi(string(typed)); err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}

func cloneRequestParameters(parameters map[string]any) map[string]any {
	out := make(map[string]any, len(parameters)+1)
	for key, value := range parameters {
		out[key] = value
	}
	return out
}

func contextCompactionScene(scene string) string {
	scene = strings.TrimSpace(scene)
	if scene == "" {
		return "agent.context_compact"
	}
	if strings.HasSuffix(scene, ".loop") {
		scene = strings.TrimSuffix(scene, ".loop")
	}
	return scene + ".context_compact"
}
