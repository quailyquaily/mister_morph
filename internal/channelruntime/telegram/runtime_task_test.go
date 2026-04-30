package telegram

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
)

func TestShouldWriteMemory(t *testing.T) {
	orchestrator := &memoryruntime.Orchestrator{}

	tests := []struct {
		name          string
		publishText   bool
		memoryEnabled bool
		orchestrator  *memoryruntime.Orchestrator
		subjectID     string
		want          bool
	}{
		{
			name:          "skip when output is not published",
			publishText:   false,
			memoryEnabled: true,
			orchestrator:  orchestrator,
			subjectID:     "tg:1",
			want:          false,
		},
		{
			name:          "skip when memory is disabled",
			publishText:   true,
			memoryEnabled: false,
			orchestrator:  orchestrator,
			subjectID:     "tg:1",
			want:          false,
		},
		{
			name:          "skip when orchestrator is missing",
			publishText:   true,
			memoryEnabled: true,
			orchestrator:  nil,
			subjectID:     "tg:1",
			want:          false,
		},
		{
			name:          "write when subject is resolved",
			publishText:   true,
			memoryEnabled: true,
			orchestrator:  orchestrator,
			subjectID:     "tg:1",
			want:          true,
		},
		{
			name:          "skip when subject is empty",
			publishText:   true,
			memoryEnabled: true,
			orchestrator:  orchestrator,
			subjectID:     "",
			want:          false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldWriteMemory(tc.publishText, tc.memoryEnabled, tc.orchestrator, tc.subjectID)
			if got != tc.want {
				t.Fatalf("shouldWriteMemory() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestContactsSendRuntimeContextForTelegramPrivateChat(t *testing.T) {
	ctx := contactsSendRuntimeContextForTelegram(telegramJob{
		ChatID:       28036192,
		ChatType:     "private",
		FromUserID:   42,
		FromUsername: "@Alice",
	})
	if len(ctx.ForbiddenTargetIDs) != 3 {
		t.Fatalf("forbidden_target_ids len = %d, want 3", len(ctx.ForbiddenTargetIDs))
	}
	if ctx.ForbiddenTargetIDs[0] != "tg:@Alice" {
		t.Fatalf("forbidden_target_ids[0] = %q, want %q", ctx.ForbiddenTargetIDs[0], "tg:@Alice")
	}
	if ctx.ForbiddenTargetIDs[1] != "tg:42" {
		t.Fatalf("forbidden_target_ids[1] = %q, want %q", ctx.ForbiddenTargetIDs[1], "tg:42")
	}
	if ctx.ForbiddenTargetIDs[2] != "tg:28036192" {
		t.Fatalf("forbidden_target_ids[2] = %q, want %q", ctx.ForbiddenTargetIDs[2], "tg:28036192")
	}
}

func TestTelegramPlanProgressLineProgrammaticFormat(t *testing.T) {
	plan := &agent.Plan{
		Steps: []agent.PlanStep{
			{Step: "scan repo", Status: agent.PlanStatusCompleted},
			{Step: "patch bug", Status: agent.PlanStatusInProgress},
		},
	}
	msg := telegramPlanProgressText(
		plan,
		agent.PlanStepUpdate{
			CompletedIndex: 0,
			CompletedStep:  "scan repo",
			StartedIndex:   1,
			StartedStep:    "patch bug",
		},
	)
	if msg != "patch bug" {
		t.Fatalf("message = %q, want %q", msg, "patch bug")
	}
}

func TestTelegramPlanProgressLineChineseFallbackByPlanStep(t *testing.T) {
	plan := &agent.Plan{
		Steps: []agent.PlanStep{
			{Step: "检查日志", Status: agent.PlanStatusCompleted},
			{Step: "修复问题", Status: agent.PlanStatusPending},
		},
	}
	msg := telegramPlanProgressText(
		plan,
		agent.PlanStepUpdate{
			CompletedIndex: 0,
			CompletedStep:  "",
			StartedIndex:   1,
			StartedStep:    "",
		},
	)
	if msg != "修复问题" {
		t.Fatalf("message = %q, want %q", msg, "修复问题")
	}
}

func TestTelegramPlanProgressLineForPlanCreatedUsesStartedStep(t *testing.T) {
	plan := &agent.Plan{
		Steps: []agent.PlanStep{
			{Step: "collect data", Status: agent.PlanStatusInProgress},
			{Step: "summarize", Status: agent.PlanStatusPending},
		},
	}
	msg := telegramPlanProgressText(
		plan,
		agent.PlanStepUpdate{
			CompletedIndex: -1,
			StartedIndex:   0,
			StartedStep:    "collect data",
			Reason:         "plan_created",
		},
	)
	if msg != "collect data" {
		t.Fatalf("message = %q, want %q", msg, "collect data")
	}
}

func TestTelegramPlanProgressLineForFinalStepDoesNotAppendDuplicateLine(t *testing.T) {
	plan := &agent.Plan{
		Steps: []agent.PlanStep{
			{Step: "ship fix", Status: agent.PlanStatusCompleted},
		},
	}
	msg := telegramPlanProgressText(
		plan,
		agent.PlanStepUpdate{
			CompletedIndex: 0,
			CompletedStep:  "ship fix",
			StartedIndex:   -1,
			StartedStep:    "",
			Reason:         "tool_success",
		},
	)
	if msg != "" {
		t.Fatalf("message = %q, want empty string", msg)
	}
}

func TestBuildTelegramCurrentMessageWithImageParts(t *testing.T) {
	orig := encodeImageToWebP
	encodeImageToWebP = func(raw []byte) ([]byte, error) { return []byte("webp-bytes"), nil }
	t.Cleanup(func() { encodeImageToWebP = orig })

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "x.jpg")
	if err := os.WriteFile(imgPath, []byte("abc"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	msg, err := buildTelegramCurrentMessage("history", "gpt-5.2", []string{imgPath}, nil)
	if err != nil {
		t.Fatalf("buildTelegramCurrentMessage() error = %v", err)
	}
	if msg.Role != "user" {
		t.Fatalf("role = %q, want user", msg.Role)
	}
	if msg.Content != "history" {
		t.Fatalf("content = %q, want history", msg.Content)
	}
	if len(msg.Parts) != 2 {
		t.Fatalf("parts len = %d, want 2", len(msg.Parts))
	}
	if msg.Parts[0].Type != "text" || msg.Parts[0].Text != "history" {
		t.Fatalf("text part mismatch: %+v", msg.Parts[0])
	}
	if msg.Parts[1].Type != "image_base64" {
		t.Fatalf("image part type = %q, want image_base64", msg.Parts[1].Type)
	}
	if msg.Parts[1].MIMEType != "image/webp" {
		t.Fatalf("image part mime = %q, want image/webp", msg.Parts[1].MIMEType)
	}
	if msg.Parts[1].DataBase64 != base64.StdEncoding.EncodeToString([]byte("webp-bytes")) {
		t.Fatalf("image part data mismatch")
	}
}

func TestBuildTelegramPromptMessagesSeparatesHistoryAndCurrent(t *testing.T) {
	orig := encodeImageToWebP
	encodeImageToWebP = func(raw []byte) ([]byte, error) { return []byte("webp-bytes"), nil }
	t.Cleanup(func() { encodeImageToWebP = orig })

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "x.jpg")
	if err := os.WriteFile(imgPath, []byte("abc"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	historyMsg, currentMsg, err := buildTelegramPromptMessages([]chathistory.ChatHistoryItem{{
		Channel:   chathistory.ChannelTelegram,
		Kind:      chathistory.KindInboundUser,
		MessageID: "101",
		SentAt:    time.Date(2026, 3, 8, 9, 0, 0, 0, time.UTC),
		Text:      "earlier",
	}}, telegramJob{
		ChatID:          42,
		MessageID:       102,
		SentAt:          time.Date(2026, 3, 8, 9, 2, 0, 0, time.UTC),
		ChatType:        "private",
		FromUserID:      7,
		FromUsername:    "alice",
		FromDisplayName: "Alice",
		Text:            "latest",
		ImagePaths:      []string{imgPath},
	}, "gpt-5.2", true, nil)
	if err != nil {
		t.Fatalf("buildTelegramPromptMessages() error = %v", err)
	}
	if historyMsg == nil {
		t.Fatalf("historyMsg = nil")
	}
	if strings.Contains(historyMsg.Content, "\"text\": \"latest\"") {
		t.Fatalf("history should not contain latest message: %s", historyMsg.Content)
	}
	if !strings.Contains(historyMsg.Content, "\"text\": \"earlier\"") {
		t.Fatalf("history should contain prior message: %s", historyMsg.Content)
	}
	if currentMsg == nil {
		t.Fatalf("currentMsg = nil")
	}
	if !strings.Contains(currentMsg.Content, "\"text\": \"latest\"") {
		t.Fatalf("current message should contain latest text: %s", currentMsg.Content)
	}
	if len(historyMsg.Parts) != 0 {
		t.Fatalf("history parts len = %d, want 0", len(historyMsg.Parts))
	}
	if len(currentMsg.Parts) != 2 {
		t.Fatalf("current parts len = %d, want 2", len(currentMsg.Parts))
	}
}

func TestBuildTelegramPromptMessagesOmitsEmptyHistory(t *testing.T) {
	historyMsg, currentMsg, err := buildTelegramPromptMessages(nil, telegramJob{
		ChatID:          42,
		MessageID:       102,
		SentAt:          time.Date(2026, 3, 8, 9, 2, 0, 0, time.UTC),
		ChatType:        "private",
		FromUserID:      7,
		FromUsername:    "alice",
		FromDisplayName: "Alice",
		Text:            "latest",
	}, "gpt-5.2", false, nil)
	if err != nil {
		t.Fatalf("buildTelegramPromptMessages() error = %v", err)
	}
	if historyMsg != nil {
		t.Fatalf("historyMsg should be nil when history is empty")
	}
	if currentMsg == nil || !strings.Contains(currentMsg.Content, "\"text\": \"latest\"") {
		t.Fatalf("current message should still be present: %#v", currentMsg)
	}
}

func TestBuildTelegramCurrentMessageSkipsMissingAndCapsCount(t *testing.T) {
	dir := t.TempDir()
	paths := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		path := filepath.Join(dir, "img_"+string(rune('a'+i))+".png")
		if err := os.WriteFile(path, []byte("ok"), 0o600); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", path, err)
		}
		paths = append(paths, path)
	}

	msg, err := buildTelegramCurrentMessage("history", "grok-4", append([]string{"/missing.png"}, paths...), nil)
	if err != nil {
		t.Fatalf("buildTelegramCurrentMessage() error = %v", err)
	}
	if len(msg.Parts) != 4 {
		t.Fatalf("parts len = %d, want 4 (1 text + 3 images)", len(msg.Parts))
	}
	if msg.Parts[0].Type != "text" {
		t.Fatalf("parts[0] type = %q, want text", msg.Parts[0].Type)
	}
	for i := 1; i < len(msg.Parts); i++ {
		if msg.Parts[i].MIMEType != "image/png" {
			t.Fatalf("parts[%d] mime = %q, want image/png", i, msg.Parts[i].MIMEType)
		}
	}
}

func TestBuildTelegramCurrentMessageUnsupportedModelSkipsImageParts(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "x.jpg")
	if err := os.WriteFile(imgPath, []byte("abc"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	msg, err := buildTelegramCurrentMessage("history", "qwen-max", []string{imgPath}, nil)
	if err != nil {
		t.Fatalf("buildTelegramCurrentMessage() error = %v", err)
	}
	if len(msg.Parts) != 0 {
		t.Fatalf("parts len = %d, want 0", len(msg.Parts))
	}
	if msg.Content != "history" {
		t.Fatalf("content = %q, want history", msg.Content)
	}
}

func TestBuildTelegramCurrentMessageReturnsErrorWhenImageTooLarge(t *testing.T) {
	orig := encodeImageToWebP
	encodeImageToWebP = func(raw []byte) ([]byte, error) { return raw, nil }
	t.Cleanup(func() { encodeImageToWebP = orig })

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "too_large.jpg")
	f, err := os.Create(imgPath)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := f.Truncate(telegramLLMMaxImageBytes + 1); err != nil {
		_ = f.Close()
		t.Fatalf("Truncate() error = %v", err)
	}
	_ = f.Close()

	_, err = buildTelegramCurrentMessage("history", "gpt-5.2", []string{imgPath}, nil)
	if err == nil {
		t.Fatalf("buildTelegramCurrentMessage() expected error")
	}
	if !strings.Contains(err.Error(), "图片太大") {
		t.Fatalf("error = %q, want contains 图片太大", err.Error())
	}
}

func TestBuildTelegramCurrentMessageUsesWebPForSupportedModel(t *testing.T) {
	orig := encodeImageToWebP
	encodeImageToWebP = func(raw []byte) ([]byte, error) { return []byte("webp-bytes"), nil }
	t.Cleanup(func() { encodeImageToWebP = orig })

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "x.jpg")
	if err := os.WriteFile(imgPath, []byte("abc"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	msg, err := buildTelegramCurrentMessage("history", "gpt-5.2", []string{imgPath}, nil)
	if err != nil {
		t.Fatalf("buildTelegramCurrentMessage() error = %v", err)
	}
	if len(msg.Parts) != 2 {
		t.Fatalf("parts len = %d, want 2", len(msg.Parts))
	}
	if msg.Parts[1].MIMEType != "image/webp" {
		t.Fatalf("mime = %q, want image/webp", msg.Parts[1].MIMEType)
	}
	if msg.Parts[1].DataBase64 != base64.StdEncoding.EncodeToString([]byte("webp-bytes")) {
		t.Fatalf("data mismatch")
	}
}

func TestBuildTelegramCurrentMessageDoesNotForceWebPForUnsupportedModel(t *testing.T) {
	orig := encodeImageToWebP
	encodeImageToWebP = func(raw []byte) ([]byte, error) { return []byte("unexpected"), nil }
	t.Cleanup(func() { encodeImageToWebP = orig })

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "x.jpg")
	if err := os.WriteFile(imgPath, []byte("abc"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	msg, err := buildTelegramCurrentMessage("history", "grok-4", []string{imgPath}, nil)
	if err != nil {
		t.Fatalf("buildTelegramCurrentMessage() error = %v", err)
	}
	if len(msg.Parts) != 2 {
		t.Fatalf("parts len = %d, want 2", len(msg.Parts))
	}
	if msg.Parts[1].MIMEType != "image/jpeg" {
		t.Fatalf("mime = %q, want image/jpeg", msg.Parts[1].MIMEType)
	}
	if msg.Parts[1].DataBase64 != base64.StdEncoding.EncodeToString([]byte("abc")) {
		t.Fatalf("data mismatch")
	}
}

func TestBuildTelegramCurrentMessageSkipsUnsupportedImageFormats(t *testing.T) {
	orig := encodeImageToWebP
	encodeImageToWebP = func(raw []byte) ([]byte, error) { return []byte("unexpected"), nil }
	t.Cleanup(func() { encodeImageToWebP = orig })

	dir := t.TempDir()
	gifPath := filepath.Join(dir, "x.gif")
	if err := os.WriteFile(gifPath, []byte("gif-bytes"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	unknownPath := filepath.Join(dir, "x.bin")
	if err := os.WriteFile(unknownPath, []byte("unknown-bytes"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	msg, err := buildTelegramCurrentMessage("history", "gpt-5.2", []string{gifPath, unknownPath}, nil)
	if err != nil {
		t.Fatalf("buildTelegramCurrentMessage() error = %v", err)
	}
	if len(msg.Parts) != 0 {
		t.Fatalf("parts len = %d, want 0", len(msg.Parts))
	}
	if msg.Content != "history" {
		t.Fatalf("content = %q, want history", msg.Content)
	}
}
