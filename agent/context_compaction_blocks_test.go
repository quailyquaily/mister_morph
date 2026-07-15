package agent

import (
	"reflect"
	"testing"

	"github.com/quailyquaily/mistermorph/llm"
)

func TestBuildTranscriptBlocksExcludesFixedMessages(t *testing.T) {
	messages := []llm.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "runtime meta"},
		{Role: "user", Content: "old user message"},
		{Role: "assistant", Content: "old assistant message"},
	}

	blocks := buildTranscriptBlocks(messages, transcriptBlockOptions{FixedMessageCount: 2})
	if len(blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(blocks))
	}
	if blocks[0].Start != 2 || blocks[0].End != 3 {
		t.Fatalf("first block = [%d,%d), want [2,3)", blocks[0].Start, blocks[0].End)
	}
	if blocks[1].Start != 3 || blocks[1].End != 4 {
		t.Fatalf("second block = [%d,%d), want [3,4)", blocks[1].Start, blocks[1].End)
	}
	for _, block := range blocks {
		if !block.Compactable {
			t.Fatalf("ordinary block unexpectedly not compactable: %+v", block)
		}
	}
}

func TestBuildTranscriptBlocksKeepsParallelToolExchangeAtomic(t *testing.T) {
	messages := []llm.Message{
		{Role: "user", Content: "do work"},
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{
				{ID: "call_a", Name: "read_file"},
				{ID: "call_b", Name: "bash"},
			},
		},
		{Role: "tool", ToolCallID: "call_a", Content: "file"},
		{Role: "tool", ToolCallID: "call_b", Content: "command"},
		{Role: "assistant", Content: "recent tail"},
	}

	blocks := buildTranscriptBlocks(messages, transcriptBlockOptions{})
	if len(blocks) != 3 {
		t.Fatalf("blocks = %d, want 3", len(blocks))
	}
	toolBlock := blocks[1]
	if toolBlock.Start != 1 || toolBlock.End != 4 {
		t.Fatalf("tool block = [%d,%d), want [1,4)", toolBlock.Start, toolBlock.End)
	}
	if !toolBlock.Compactable {
		t.Fatalf("complete tool exchange not compactable: %+v", toolBlock)
	}
}

func TestBuildTranscriptBlocksRejectsIncompleteAndOrphanToolMessages(t *testing.T) {
	tests := []struct {
		name     string
		messages []llm.Message
	}{
		{
			name: "missing result",
			messages: []llm.Message{
				{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call_a", Name: "read_file"}, {ID: "call_b", Name: "bash"}}},
				{Role: "tool", ToolCallID: "call_a", Content: "file"},
			},
		},
		{
			name: "orphan result",
			messages: []llm.Message{
				{Role: "tool", ToolCallID: "call_a", Content: "file"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := buildTranscriptBlocks(tt.messages, transcriptBlockOptions{})
			if len(blocks) != 1 {
				t.Fatalf("blocks = %d, want 1", len(blocks))
			}
			if blocks[0].Compactable {
				t.Fatalf("invalid tool block unexpectedly compactable: %+v", blocks[0])
			}
		})
	}
}

func TestBuildTranscriptBlocksProtectsPendingToolExchange(t *testing.T) {
	messages := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call_a", Name: "bash"}}},
		{Role: "tool", ToolCallID: "call_a", Content: "done"},
		{Role: "user", Content: "tail"},
	}
	blocks := buildTranscriptBlocks(messages, transcriptBlockOptions{
		PendingToolCallIDs: map[string]struct{}{"call_a": {}},
	})
	if len(blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(blocks))
	}
	if blocks[0].Compactable {
		t.Fatalf("pending tool block unexpectedly compactable: %+v", blocks[0])
	}
	if blocks[0].Reason != transcriptBlockReasonPendingTool {
		t.Fatalf("reason = %q, want %q", blocks[0].Reason, transcriptBlockReasonPendingTool)
	}
}

func TestBuildTranscriptBlocksRequiresPreparedImageReference(t *testing.T) {
	messages := []llm.Message{{
		Role:    "user",
		Content: "inspect this image",
		Parts: []llm.Part{
			{Type: llm.PartTypeText, Text: "inspect this image"},
			{Type: llm.PartTypeImageBase64, MIMEType: "image/png", DataBase64: "aW1hZ2U="},
		},
	}}

	blocks := buildTranscriptBlocks(messages, transcriptBlockOptions{})
	if len(blocks) != 1 || blocks[0].Compactable {
		t.Fatalf("unprepared image block = %+v, want one non-compactable block", blocks)
	}
	if blocks[0].Reason != transcriptBlockReasonUnpreparedImage {
		t.Fatalf("reason = %q, want %q", blocks[0].Reason, transcriptBlockReasonUnpreparedImage)
	}

	blocks = buildTranscriptBlocks(messages, transcriptBlockOptions{
		PreparedImageMessageIndexes: map[int]struct{}{0: {}},
	})
	if len(blocks) != 1 || !blocks[0].Compactable {
		t.Fatalf("prepared image block = %+v, want one compactable block", blocks)
	}
}

func TestSelectTranscriptPrefixUsesContinuousOldestBlocksAndRetainsTail(t *testing.T) {
	blocks := []transcriptBlock{
		{Start: 2, End: 3, EstimatedTokens: 30, Compactable: true},
		{Start: 3, End: 6, EstimatedTokens: 35, Compactable: true},
		{Start: 6, End: 7, EstimatedTokens: 100, Compactable: true},
	}

	selection, ok := selectTranscriptPrefix(blocks, 50)
	if !ok {
		t.Fatal("selection not found")
	}
	if selection.Start != 2 || selection.End != 6 || selection.BlockCount != 2 {
		t.Fatalf("selection = %+v, want blocks [2,6)", selection)
	}
	if selection.EstimatedTokens != 65 || !selection.ReachedTarget {
		t.Fatalf("selection tokens = %+v, want 65 and reached target", selection)
	}

	selection, ok = selectTranscriptPrefix(blocks, 1000)
	if !ok {
		t.Fatal("partial selection not found")
	}
	if selection.End != 6 || selection.BlockCount != 2 {
		t.Fatalf("selection retained no tail: %+v", selection)
	}
	if selection.ReachedTarget {
		t.Fatalf("selection unexpectedly reached target: %+v", selection)
	}
}

func TestSelectTranscriptPrefixStopsBeforeUnsafeBlock(t *testing.T) {
	blocks := []transcriptBlock{
		{Start: 0, End: 1, EstimatedTokens: 10, Compactable: true},
		{Start: 1, End: 3, EstimatedTokens: 50, Compactable: false},
		{Start: 3, End: 4, EstimatedTokens: 50, Compactable: true},
	}

	selection, ok := selectTranscriptPrefix(blocks, 40)
	if !ok {
		t.Fatal("safe partial selection not found")
	}
	if selection.Start != 0 || selection.End != 1 || selection.BlockCount != 1 {
		t.Fatalf("selection crossed unsafe block: %+v", selection)
	}
}

func TestTranscriptBlocksSurviveResumeSerialization(t *testing.T) {
	messages := []llm.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call_a", Name: "read_file"}}},
		{Role: "tool", ToolCallID: "call_a", Content: "result"},
	}
	want := buildTranscriptBlocks(messages, transcriptBlockOptions{FixedMessageCount: 1})

	raw, err := marshalResumeState(resumeStateV1{Messages: messages})
	if err != nil {
		t.Fatalf("marshal resume state: %v", err)
	}
	restored, err := unmarshalResumeState(raw)
	if err != nil {
		t.Fatalf("unmarshal resume state: %v", err)
	}
	got := buildTranscriptBlocks(restored.Messages, transcriptBlockOptions{FixedMessageCount: 1})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("blocks after resume = %#v, want %#v", got, want)
	}
}
