package telegram

import "testing"

func TestTelegramApprovalCallbackDataRoundTrip(t *testing.T) {
	data := telegramApprovalCallbackData("apr_123", true)
	id, approved, ok := parseTelegramApprovalCallbackData(data)
	if !ok || !approved || id != "apr_123" {
		t.Fatalf("parse approve data = id %q approved %v ok %v, want apr_123 true true", id, approved, ok)
	}

	data = telegramApprovalCallbackData("apr_123", false)
	id, approved, ok = parseTelegramApprovalCallbackData(data)
	if !ok || approved || id != "apr_123" {
		t.Fatalf("parse deny data = id %q approved %v ok %v, want apr_123 false true", id, approved, ok)
	}
}

func TestParseTelegramApprovalCallbackDataRejectsInvalid(t *testing.T) {
	for _, raw := range []string{"", "x:a:apr_1", "ap:x:apr_1", "ap:a:", "ap:a"} {
		if _, _, ok := parseTelegramApprovalCallbackData(raw); ok {
			t.Fatalf("parseTelegramApprovalCallbackData(%q) ok=true, want false", raw)
		}
	}
}

func TestTelegramApprovalResultText(t *testing.T) {
	if got := telegramApprovalResultText(false); got != "Approval denied. Task canceled." {
		t.Fatalf("deny text = %q", got)
	}
	if got := telegramApprovalResultText(true); got != "Approved. Resuming task." {
		t.Fatalf("approve text = %q", got)
	}
}

func TestTelegramApprovalCallbackMessageTarget(t *testing.T) {
	chatID, threadID, ok := telegramApprovalCallbackMessageTarget(&telegramCallbackQuery{
		Message: &telegramMessage{
			MessageThreadID: 456,
			Chat:            &telegramChat{ID: 123},
		},
	})
	if !ok || chatID != 123 || threadID != 456 {
		t.Fatalf("target = chat %d thread %d ok %v, want 123 456 true", chatID, threadID, ok)
	}
}

func TestTelegramApprovalCallbackMessageTargetRejectsMissingChat(t *testing.T) {
	if _, _, ok := telegramApprovalCallbackMessageTarget(&telegramCallbackQuery{}); ok {
		t.Fatal("target ok=true, want false")
	}
}
