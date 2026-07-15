package agent

import (
	"encoding/json"
	"strings"

	"github.com/quailyquaily/mistermorph/llm"
)

const (
	transcriptBlockReasonInvalidTool     = "invalid_tool_exchange"
	transcriptBlockReasonPendingTool     = "pending_tool_exchange"
	transcriptBlockReasonProtected       = "protected_message"
	transcriptBlockReasonUnpreparedImage = "unprepared_image"
)

type transcriptBlockOptions struct {
	FixedMessageCount           int
	PendingToolCallIDs          map[string]struct{}
	ProtectedMessageIndexes     map[int]struct{}
	PreparedImageMessageIndexes map[int]struct{}
}

type transcriptBlock struct {
	Start           int
	End             int
	EstimatedTokens int
	Compactable     bool
	Reason          string
}

type transcriptSelection struct {
	Start           int
	End             int
	BlockCount      int
	EstimatedTokens int
	ReachedTarget   bool
}

func buildTranscriptBlocks(messages []llm.Message, opts transcriptBlockOptions) []transcriptBlock {
	start := opts.FixedMessageCount
	if start < 0 {
		start = 0
	}
	if start >= len(messages) {
		return nil
	}

	blocks := make([]transcriptBlock, 0, len(messages)-start)
	for i := start; i < len(messages); {
		message := messages[i]
		role := normalizedMessageRole(message.Role)
		if role == "assistant" && len(message.ToolCalls) > 0 {
			block, next := buildToolExchangeBlock(messages, i, opts)
			blocks = append(blocks, block)
			i = next
			continue
		}

		block := transcriptBlock{
			Start:           i,
			End:             i + 1,
			EstimatedTokens: estimateMessageTokens(message),
			Compactable:     true,
		}
		switch {
		case role == "tool":
			block.Compactable = false
			block.Reason = transcriptBlockReasonInvalidTool
		case messageIndexProtected(i, opts.ProtectedMessageIndexes):
			block.Compactable = false
			block.Reason = transcriptBlockReasonProtected
		case messageHasImagePart(message) && !messageIndexProtected(i, opts.PreparedImageMessageIndexes):
			block.Compactable = false
			block.Reason = transcriptBlockReasonUnpreparedImage
		}
		blocks = append(blocks, block)
		i++
	}
	return blocks
}

func buildToolExchangeBlock(messages []llm.Message, start int, opts transcriptBlockOptions) (transcriptBlock, int) {
	assistant := messages[start]
	callIDs := make(map[string]struct{}, len(assistant.ToolCalls))
	valid := true
	pending := false
	for _, call := range assistant.ToolCalls {
		id := strings.TrimSpace(call.ID)
		if id == "" {
			valid = false
			continue
		}
		if _, exists := callIDs[id]; exists {
			valid = false
			continue
		}
		callIDs[id] = struct{}{}
		if _, exists := opts.PendingToolCallIDs[id]; exists {
			pending = true
		}
	}

	resultIDs := make(map[string]struct{}, len(callIDs))
	end := start + 1
	for end < len(messages) && normalizedMessageRole(messages[end].Role) == "tool" {
		id := strings.TrimSpace(messages[end].ToolCallID)
		if _, exists := callIDs[id]; !exists || id == "" {
			valid = false
		} else if _, duplicate := resultIDs[id]; duplicate {
			valid = false
		} else {
			resultIDs[id] = struct{}{}
		}
		end++
	}
	if len(resultIDs) != len(callIDs) {
		valid = false
	}

	block := transcriptBlock{
		Start:           start,
		End:             end,
		EstimatedTokens: estimateMessagesTokens(messages[start:end]),
		Compactable:     valid,
	}
	if !valid {
		block.Reason = transcriptBlockReasonInvalidTool
	}
	if pending {
		block.Compactable = false
		block.Reason = transcriptBlockReasonPendingTool
	}
	for i := start; i < end; i++ {
		if messageIndexProtected(i, opts.ProtectedMessageIndexes) {
			block.Compactable = false
			block.Reason = transcriptBlockReasonProtected
			break
		}
		if messageHasImagePart(messages[i]) && !messageIndexProtected(i, opts.PreparedImageMessageIndexes) {
			block.Compactable = false
			block.Reason = transcriptBlockReasonUnpreparedImage
			break
		}
	}
	return block, end
}

func selectTranscriptPrefix(blocks []transcriptBlock, targetTokens int) (transcriptSelection, bool) {
	if len(blocks) < 2 || targetTokens <= 0 {
		return transcriptSelection{}, false
	}

	selection := transcriptSelection{Start: blocks[0].Start}
	previousEnd := blocks[0].Start
	for i := 0; i < len(blocks)-1; i++ {
		block := blocks[i]
		if !block.Compactable || block.Start != previousEnd || block.End <= block.Start {
			break
		}
		selection.End = block.End
		selection.BlockCount++
		selection.EstimatedTokens += block.EstimatedTokens
		previousEnd = block.End
		if selection.EstimatedTokens >= targetTokens {
			selection.ReachedTarget = true
			break
		}
	}
	if selection.BlockCount == 0 {
		return transcriptSelection{}, false
	}
	return selection, true
}

func estimateMessagesTokens(messages []llm.Message) int {
	total := 0
	for _, message := range messages {
		total += estimateMessageTokens(message)
	}
	return total
}

func estimateMessageTokens(message llm.Message) int {
	raw, err := json.Marshal(message)
	if err != nil {
		return 1
	}
	// This estimate is only used to choose a safe prefix. Provider usage remains
	// the authoritative input-token count.
	tokens := (len(raw) + 2) / 3
	if tokens < 1 {
		return 1
	}
	return tokens
}

func normalizedMessageRole(role string) string {
	return strings.ToLower(strings.TrimSpace(role))
}

func messageHasImagePart(message llm.Message) bool {
	for _, part := range message.Parts {
		switch strings.ToLower(strings.TrimSpace(part.Type)) {
		case llm.PartTypeImageBase64, llm.PartTypeImageURL:
			return true
		}
	}
	return false
}

func messageIndexProtected(index int, indexes map[int]struct{}) bool {
	if len(indexes) == 0 {
		return false
	}
	_, ok := indexes[index]
	return ok
}
