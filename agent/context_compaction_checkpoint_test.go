package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/llm"
)

const validCheckpointJSON = `{
  "summary": "Inspected the repository and started the implementation.",
  "user_intent": ["Implement context compaction without splitting tool exchanges."],
  "references": {
    "files": ["workspace_dir/agent/engine.go"],
    "directories": ["workspace_dir/agent"],
    "urls": []
  },
  "progress": {
    "completed": ["Read the design document."],
    "in_progress": ["Implement checkpoint validation."],
    "pending": ["Run all tests."]
  },
  "intermediate_results": ["Transcript blocks are atomic."]
}`

func TestParseCheckpointContentAcceptsCompleteSchema(t *testing.T) {
	content, err := parseCheckpointContent([]byte(validCheckpointJSON), checkpointValidationOptions{
		RequireUserIntent: true,
	})
	if err != nil {
		t.Fatalf("parse checkpoint: %v", err)
	}
	if content.Summary == "" || len(content.UserIntent) != 1 {
		t.Fatalf("checkpoint content = %+v", content)
	}
	if got := content.References.Files; len(got) != 1 || got[0] != "workspace_dir/agent/engine.go" {
		t.Fatalf("files = %#v", got)
	}
}

func TestParseCheckpointContentRejectsMissingOrNullRequiredFields(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "missing summary", raw: `{"user_intent":["goal"],"references":{"files":[],"directories":[],"urls":[]},"progress":{"completed":[],"in_progress":[],"pending":[]},"intermediate_results":[]}`},
		{name: "missing files", raw: `{"summary":"s","user_intent":[],"references":{"directories":[],"urls":[]},"progress":{"completed":[],"in_progress":[],"pending":[]},"intermediate_results":[]}`},
		{name: "null user intent", raw: `{"summary":"s","user_intent":null,"references":{"files":[],"directories":[],"urls":[]},"progress":{"completed":[],"in_progress":[],"pending":[]},"intermediate_results":[]}`},
		{name: "missing progress", raw: `{"summary":"s","user_intent":[],"references":{"files":[],"directories":[],"urls":[]},"intermediate_results":[]}`},
		{name: "unknown field", raw: `{"summary":"s","user_intent":[],"references":{"files":[],"directories":[],"urls":[]},"progress":{"completed":[],"in_progress":[],"pending":[]},"intermediate_results":[],"extra":true}`},
		{name: "runtime-owned kind", raw: `{"kind":"context_checkpoint","summary":"s","user_intent":[],"references":{"files":[],"directories":[],"urls":[]},"progress":{"completed":[],"in_progress":[],"pending":[]},"intermediate_results":[]}`},
		{name: "empty summary and intent", raw: `{"summary":"","user_intent":[],"references":{"files":[],"directories":[],"urls":[]},"progress":{"completed":[],"in_progress":[],"pending":[]},"intermediate_results":[]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseCheckpointContent([]byte(tt.raw), checkpointValidationOptions{}); err == nil {
				t.Fatal("parse checkpoint error = nil")
			}
		})
	}
}

func TestParseCheckpointContentRequiresIntentForCompactedUserMessage(t *testing.T) {
	raw := `{"summary":"summary only","user_intent":[],"references":{"files":[],"directories":[],"urls":[]},"progress":{"completed":[],"in_progress":[],"pending":[]},"intermediate_results":[]}`
	if _, err := parseCheckpointContent([]byte(raw), checkpointValidationOptions{RequireUserIntent: true}); err == nil {
		t.Fatal("parse checkpoint error = nil")
	}
}

func TestParseCheckpointContentRequiresPreparedImageReferences(t *testing.T) {
	if _, err := parseCheckpointContent([]byte(validCheckpointJSON), checkpointValidationOptions{
		RequiredFileReferences: []string{"workspace_dir/.mistermorph/context-images/image.png"},
	}); err == nil {
		t.Fatal("parse checkpoint error = nil")
	}
}

func TestParseCheckpointContentRejectsOversizedJSON(t *testing.T) {
	raw := []byte(strings.Repeat("x", maxCheckpointJSONBytes+1))
	if _, err := parseCheckpointContent(raw, checkpointValidationOptions{}); err == nil {
		t.Fatal("parse checkpoint error = nil")
	}
}

func TestBuildCheckpointMessageUsesUserRoleAndRuntimeWrapper(t *testing.T) {
	content, err := parseCheckpointContent([]byte(validCheckpointJSON), checkpointValidationOptions{})
	if err != nil {
		t.Fatalf("parse checkpoint: %v", err)
	}
	message, err := buildCheckpointMessage(content)
	if err != nil {
		t.Fatalf("build checkpoint message: %v", err)
	}
	if message.Role != "user" {
		t.Fatalf("role = %q, want user", message.Role)
	}
	var envelope struct {
		RuntimeContext checkpointContent `json:"runtime_context"`
		Instruction    string            `json:"instruction"`
	}
	if err := json.Unmarshal([]byte(message.Content), &envelope); err != nil {
		t.Fatalf("decode checkpoint message: %v", err)
	}
	if envelope.RuntimeContext.Summary != content.Summary {
		t.Fatalf("summary = %q, want %q", envelope.RuntimeContext.Summary, content.Summary)
	}
	if envelope.Instruction != checkpointContinueInstruction {
		t.Fatalf("instruction = %q, want %q", envelope.Instruction, checkpointContinueInstruction)
	}
}

func TestBuildCheckpointMessageRejectsOversizedFinalJSON(t *testing.T) {
	chunk := strings.Repeat("x", maxCheckpointStringBytes)
	content := checkpointContent{
		Summary:    chunk,
		UserIntent: []string{chunk, chunk, chunk, chunk},
		References: checkpointReferences{
			Files:       []string{},
			Directories: []string{},
			URLs:        []string{},
		},
		Progress: checkpointProgress{
			Completed:  []string{},
			InProgress: []string{},
			Pending:    []string{},
		},
		IntermediateResults: []string{},
	}
	if _, err := buildCheckpointMessage(content); err == nil {
		t.Fatal("build checkpoint message error = nil")
	}
}

func TestReplaceMessagesWithCheckpointPreservesFixedAndTail(t *testing.T) {
	messages := []llm.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "runtime meta"},
		{Role: "user", Content: "old task"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call_a", Name: "read_file"}}},
		{Role: "tool", ToolCallID: "call_a", Content: "result"},
		{Role: "user", Content: "recent steer"},
		{Role: "assistant", Content: "recent response"},
	}
	original := append([]llm.Message(nil), messages...)
	checkpoint := llm.Message{Role: "user", Content: `{"runtime_context":{"kind":"context_checkpoint"}}`}

	got, err := replaceMessagesWithCheckpoint(messages, 2, transcriptSelection{Start: 2, End: 5}, checkpoint)
	if err != nil {
		t.Fatalf("replace messages: %v", err)
	}
	want := []llm.Message{messages[0], messages[1], checkpoint, messages[5], messages[6]}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages after replacement = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(messages, original) {
		t.Fatalf("input messages mutated: %#v", messages)
	}
}

func TestReplaceMessagesWithCheckpointRejectsInvalidSelectionOrRole(t *testing.T) {
	messages := []llm.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "old"},
		{Role: "assistant", Content: "tail"},
	}
	tests := []struct {
		name       string
		selection  transcriptSelection
		checkpoint llm.Message
	}{
		{name: "touches fixed", selection: transcriptSelection{Start: 0, End: 2}, checkpoint: llm.Message{Role: "user", Content: "checkpoint"}},
		{name: "empty selection", selection: transcriptSelection{Start: 1, End: 1}, checkpoint: llm.Message{Role: "user", Content: "checkpoint"}},
		{name: "wrong role", selection: transcriptSelection{Start: 1, End: 2}, checkpoint: llm.Message{Role: "assistant", Content: "checkpoint"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := replaceMessagesWithCheckpoint(messages, 1, tt.selection, tt.checkpoint); err == nil {
				t.Fatal("replace messages error = nil")
			}
		})
	}
}

func TestReplaceMessagesWithCheckpointRejectsIncompleteRetainedToolExchange(t *testing.T) {
	messages := []llm.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "old"},
		{Role: "tool", ToolCallID: "missing_call", Content: "orphan result"},
	}
	checkpoint := llm.Message{Role: "user", Content: "checkpoint"}
	if _, err := replaceMessagesWithCheckpoint(messages, 1, transcriptSelection{Start: 1, End: 2}, checkpoint); err == nil {
		t.Fatal("replace messages error = nil")
	}
}

func TestPrepareCompactionImagesPersistsBase64AndUsesFileReference(t *testing.T) {
	workspaceDir := t.TempDir()
	rawImage := []byte("not-a-real-png-but-stable-test-data")
	messages := []llm.Message{{
		Role:    "user",
		Content: "inspect the image",
		Parts: []llm.Part{
			{Type: llm.PartTypeText, Text: "inspect the image"},
			{Type: llm.PartTypeImageBase64, MIMEType: "image/png", DataBase64: base64.StdEncoding.EncodeToString(rawImage)},
		},
	}}
	original := messages[0].Parts[1].DataBase64

	prepared, err := prepareCompactionImages(
		context.Background(),
		messages,
		pathroots.New(workspaceDir, "", ""),
	)
	if err != nil {
		t.Fatalf("prepare images: %v", err)
	}
	references := prepared.ReferencesByMessage[0]
	imageParts := prepared.ImagePartsByMessage[0]
	if len(references) != 1 || len(imageParts) != 1 {
		t.Fatalf("prepared images = %+v", prepared)
	}
	ref := references[0]
	if !strings.HasPrefix(ref.Path, "workspace_dir/.mistermorph/context-images/") {
		t.Fatalf("reference path = %q", ref.Path)
	}
	if ref.MIMEType != "image/png" || ref.SHA256 == "" || ref.Bytes != int64(len(rawImage)) {
		t.Fatalf("reference = %+v", ref)
	}
	localPath := filepath.Join(workspaceDir, filepath.FromSlash(strings.TrimPrefix(ref.Path, "workspace_dir/")))
	gotRaw, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read persisted image: %v", err)
	}
	if !reflect.DeepEqual(gotRaw, rawImage) {
		t.Fatalf("persisted image = %q, want %q", gotRaw, rawImage)
	}
	info, err := os.Stat(localPath)
	if err != nil {
		t.Fatalf("stat persisted image: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("image mode = %o, want 600", info.Mode().Perm())
	}
	if _, ok := prepared.PreparedMessageIndexes[0]; !ok {
		t.Fatal("message index 0 not marked prepared")
	}
	if got := prepared.Messages[0].Parts; len(got) != 2 || got[1].Type != llm.PartTypeText || !strings.Contains(got[1].Text, ref.Path) {
		t.Fatalf("serialized message parts = %#v", got)
	}
	if strings.Contains(prepared.Messages[0].Parts[1].Text, original) {
		t.Fatal("serialized message still contains base64")
	}
	if messages[0].Parts[1].DataBase64 != original {
		t.Fatal("input image part mutated")
	}
}

func TestPrepareCompactionImagesRejectsRemoteImageWithoutRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("remote-image"))
	}))
	defer server.Close()

	prepared, err := prepareCompactionImages(context.Background(), []llm.Message{{
		Role: "user",
		Parts: []llm.Part{{
			Type: llm.PartTypeImageURL,
			URL:  server.URL + "/image.png",
		}},
	}}, pathroots.New(t.TempDir(), "", ""))
	if err != nil {
		t.Fatalf("prepare images: %v", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
	if prepared.Failures[0] == nil {
		t.Fatal("remote image message has no failure")
	}
	if _, ok := prepared.PreparedMessageIndexes[0]; ok {
		t.Fatal("remote image message marked prepared")
	}
}

func TestPrepareCompactionImagesLeavesFailedMessageUnprepared(t *testing.T) {
	prepared, err := prepareCompactionImages(context.Background(), []llm.Message{{
		Role: "user",
		Parts: []llm.Part{{
			Type:       llm.PartTypeImageBase64,
			MIMEType:   "image/png",
			DataBase64: "not-base64",
		}},
	}}, pathroots.New(t.TempDir(), "", ""))
	if err != nil {
		t.Fatalf("prepare images: %v", err)
	}
	if _, ok := prepared.PreparedMessageIndexes[0]; ok {
		t.Fatal("failed image message marked prepared")
	}
	if prepared.Failures[0] == nil {
		t.Fatal("failed image message has no failure")
	}
}
