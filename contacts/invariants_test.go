package contacts

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type mockSender struct {
	accepted bool
	deduped  bool
	err      error
	calls    int
	contact  Contact
	decision ShareDecision
}

func (m *mockSender) Send(ctx context.Context, contact Contact, decision ShareDecision) (bool, bool, error) {
	_ = ctx
	m.contact = contact
	m.decision = decision
	m.calls++
	return m.accepted, m.deduped, m.err
}

func TestInvariantOutboxIdempotency(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 8, 21, 0, 0, 0, time.UTC)

	if _, err := svc.UpsertContact(ctx, Contact{
		ContactID:       "tg:10086",
		Kind:            KindHuman,
		Channel:         ChannelTelegram,
		TGPrivateChatID: 10086,
	}, now); err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}

	sender := &mockSender{accepted: true}
	payload := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	decision := ShareDecision{
		ContactID:      "tg:10086",
		ItemID:         "manual_item_1",
		ContentType:    "text/plain",
		PayloadBase64:  payload,
		IdempotencyKey: "manual:key1",
	}

	if _, err := svc.SendDecision(ctx, now, decision, sender); err != nil {
		t.Fatalf("SendDecision(first) error = %v", err)
	}
	second, err := svc.SendDecision(ctx, now.Add(1*time.Minute), decision, sender)
	if err != nil {
		t.Fatalf("SendDecision(second) error = %v", err)
	}
	if sender.calls != 1 {
		t.Fatalf("sender calls mismatch: got %d want 1", sender.calls)
	}
	if !second.Deduped {
		t.Fatalf("second send expected deduped outcome, got=%+v", second)
	}
}

func TestSendDecisionWithLegacyOutboxRecordMissingAttempts(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 8, 21, 15, 0, 0, time.UTC)

	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "bus_outbox.json"),
		[]byte("{\"version\":1,\"records\":[{\"channel\":\"telegram\",\"idempotency_key\":\"legacy:k0\",\"status\":\"sent\",\"created_at\":\"2026-02-08T20:00:00Z\",\"updated_at\":\"2026-02-08T20:00:00Z\",\"sent_at\":\"2026-02-08T20:00:00Z\"}]}\n"),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := svc.UpsertContact(ctx, Contact{
		ContactID:       "tg:10087",
		Kind:            KindHuman,
		Channel:         ChannelTelegram,
		TGPrivateChatID: 10087,
	}, now); err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}

	sender := &mockSender{accepted: true}
	payload := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	outcome, err := svc.SendDecision(ctx, now, ShareDecision{
		ContactID:      "tg:10087",
		ItemID:         "manual_item_legacy",
		ContentType:    "text/plain",
		PayloadBase64:  payload,
		IdempotencyKey: "manual:key-legacy",
	}, sender)
	if err != nil {
		t.Fatalf("SendDecision() error = %v", err)
	}
	if sender.calls != 1 {
		t.Fatalf("sender calls mismatch: got %d want 1", sender.calls)
	}
	if !outcome.Accepted {
		t.Fatalf("outcome accepted mismatch: got %+v", outcome)
	}
}

func TestSendDecisionResolvesTelegramUsernameToPrivateChatContact(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 10, 11, 30, 0, 0, time.UTC)

	if _, err := svc.UpsertContact(ctx, Contact{
		ContactID:       "tg:777",
		Kind:            KindHuman,
		Channel:         ChannelTelegram,
		TGUsername:      "trinity",
		TGPrivateChatID: 777,
	}, now); err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}

	sender := &mockSender{accepted: true}
	payload := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	outcome, err := svc.SendDecision(ctx, now, ShareDecision{
		ContactID:      "tg:@trinity",
		ItemID:         "manual_item_2",
		ContentType:    "application/json",
		PayloadBase64:  payload,
		IdempotencyKey: "manual:key2",
	}, sender)
	if err != nil {
		t.Fatalf("SendDecision() error = %v", err)
	}
	if sender.calls != 1 {
		t.Fatalf("sender calls mismatch: got %d want 1", sender.calls)
	}
	if sender.contact.ContactID != "tg:777" {
		t.Fatalf("sender contact mismatch: got %q want %q", sender.contact.ContactID, "tg:777")
	}
	if sender.decision.ContactID != "tg:@trinity" {
		t.Fatalf("sender decision contact_id mismatch: got %q want %q", sender.decision.ContactID, "tg:@trinity")
	}
	if outcome.ContactID != "tg:777" {
		t.Fatalf("outcome contact_id mismatch: got %q want %q", outcome.ContactID, "tg:777")
	}
}

func TestSendDecisionTelegramUsernameWithoutPrivateChatID(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 10, 11, 45, 0, 0, time.UTC)

	if _, err := svc.UpsertContact(ctx, Contact{
		ContactID:      "contact:trinity",
		Kind:           KindHuman,
		Channel:        ChannelTelegram,
		TGUsername:     "trinity",
		TGGroupChatIDs: []int64{-1007788},
	}, now); err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}

	sender := &mockSender{accepted: true}
	payload := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	outcome, err := svc.SendDecision(ctx, now, ShareDecision{
		ContactID:      "tg:@trinity",
		ItemID:         "manual_item_3",
		ContentType:    "application/json",
		PayloadBase64:  payload,
		IdempotencyKey: "manual:key3",
	}, sender)
	if err != nil {
		t.Fatalf("SendDecision() error = %v", err)
	}
	if sender.calls != 1 {
		t.Fatalf("sender calls = %d, want 1", sender.calls)
	}
	if sender.decision.ContactID != "tg:@trinity" {
		t.Fatalf("sender decision contact_id = %q, want %q", sender.decision.ContactID, "tg:@trinity")
	}
	if outcome.ContactID != "contact:trinity" {
		t.Fatalf("outcome contact_id = %q, want %q", outcome.ContactID, "contact:trinity")
	}
}

func TestSendDecisionUnknownProtocolNotFoundSuggestsProtocolChannel(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 10, 11, 50, 0, 0, time.UTC)

	sender := &mockSender{accepted: true}
	payload := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	_, err := svc.SendDecision(ctx, now, ShareDecision{
		ContactID:      "aqua:abc123",
		ItemID:         "manual_item_3b",
		ContentType:    "application/json",
		PayloadBase64:  payload,
		IdempotencyKey: "manual:key3b",
	}, sender)
	if err == nil {
		t.Fatalf("SendDecision() expected contact not found error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "hint: protocol") {
		t.Fatalf("missing protocol hint prefix, got %q", msg)
	}
	if !strings.Contains(msg, "'abc123'") {
		t.Fatalf("missing contact peer id in hint, got %q", msg)
	}
	if !strings.Contains(msg, "protocol/tool 'aqua'") {
		t.Fatalf("missing protocol delivery hint, got %q", msg)
	}
	if sender.calls != 0 {
		t.Fatalf("sender should not be called, got %d", sender.calls)
	}
}

func TestSendDecisionChatIDHintAllowsTelegramChannelResolution(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC)

	if _, err := svc.UpsertContact(ctx, Contact{
		ContactID: "contact:neo",
		Kind:      KindHuman,
		Channel:   ChannelTelegram,
	}, now); err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}

	sender := &mockSender{accepted: true}
	payload := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	outcome, err := svc.SendDecision(ctx, now, ShareDecision{
		ContactID:      "contact:neo",
		ChatID:         "tg:-1007788",
		ItemID:         "manual_item_4",
		ContentType:    "application/json",
		PayloadBase64:  payload,
		IdempotencyKey: "manual:key4",
	}, sender)
	if err != nil {
		t.Fatalf("SendDecision() error = %v", err)
	}
	if !outcome.Accepted {
		t.Fatalf("outcome accepted mismatch: got %+v", outcome)
	}
	if sender.calls != 1 {
		t.Fatalf("sender calls mismatch: got %d want 1", sender.calls)
	}
	if sender.decision.ChatID != "tg:-1007788" {
		t.Fatalf("sender decision chat_id mismatch: got %q want %q", sender.decision.ChatID, "tg:-1007788")
	}
}

func TestSendDecisionInvalidChatIDHintFailsBeforeSend(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 10, 12, 10, 0, 0, time.UTC)

	if _, err := svc.UpsertContact(ctx, Contact{
		ContactID: "contact:neo",
		Kind:      KindHuman,
		Channel:   ChannelTelegram,
	}, now); err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}

	sender := &mockSender{accepted: true}
	payload := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	_, err := svc.SendDecision(ctx, now, ShareDecision{
		ContactID:      "contact:neo",
		ChatID:         "tg:@neo",
		ItemID:         "manual_item_5",
		ContentType:    "application/json",
		PayloadBase64:  payload,
		IdempotencyKey: "manual:key5",
	}, sender)
	if err == nil {
		t.Fatalf("SendDecision() expected invalid chat_id error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "invalid chat_id") {
		t.Fatalf("SendDecision() error mismatch: got %q", err.Error())
	}
	if sender.calls != 0 {
		t.Fatalf("sender should not be called, got %d", sender.calls)
	}
}

func TestSendDecisionChatIDHintAllowsSlackChannelResolution(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 10, 12, 20, 0, 0, time.UTC)

	if _, err := svc.UpsertContact(ctx, Contact{
		ContactID: "contact:neo",
		Kind:      KindHuman,
		Channel:   ChannelTelegram,
	}, now); err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}

	sender := &mockSender{accepted: true}
	payload := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	outcome, err := svc.SendDecision(ctx, now, ShareDecision{
		ContactID:      "contact:neo",
		ChatID:         "slack:T111:C222",
		ItemID:         "manual_item_6",
		ContentType:    "application/json",
		PayloadBase64:  payload,
		IdempotencyKey: "manual:key6",
	}, sender)
	if err != nil {
		t.Fatalf("SendDecision() error = %v", err)
	}
	if !outcome.Accepted {
		t.Fatalf("outcome accepted mismatch: got %+v", outcome)
	}
	if sender.calls != 1 {
		t.Fatalf("sender calls mismatch: got %d want 1", sender.calls)
	}
	outbox, ok, err := store.GetBusOutboxRecord(ctx, ChannelSlack, "manual:key6")
	if err != nil {
		t.Fatalf("GetBusOutboxRecord(slack) error = %v", err)
	}
	if !ok {
		t.Fatalf("expected slack outbox record")
	}
	if outbox.Channel != ChannelSlack {
		t.Fatalf("outbox channel mismatch: got %q want %q", outbox.Channel, ChannelSlack)
	}
}

func TestSendDecisionPrefersSlackTargetWhenNoChatIDHint(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 10, 12, 30, 0, 0, time.UTC)

	if _, err := svc.UpsertContact(ctx, Contact{
		ContactID:        "slack:T111:U222",
		Kind:             KindHuman,
		Channel:          ChannelSlack,
		TGPrivateChatID:  90001,
		SlackTeamID:      "T111",
		SlackUserID:      "U222",
		SlackDMChannelID: "D333",
	}, now); err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}

	sender := &mockSender{accepted: true}
	payload := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	outcome, err := svc.SendDecision(ctx, now, ShareDecision{
		ContactID:      "slack:T111:U222",
		ItemID:         "manual_item_7",
		ContentType:    "application/json",
		PayloadBase64:  payload,
		IdempotencyKey: "manual:key7",
	}, sender)
	if err != nil {
		t.Fatalf("SendDecision() error = %v", err)
	}
	if !outcome.Accepted {
		t.Fatalf("outcome accepted mismatch: got %+v", outcome)
	}
	if sender.calls != 1 {
		t.Fatalf("sender calls mismatch: got %d want 1", sender.calls)
	}
	if _, ok, err := store.GetBusOutboxRecord(ctx, ChannelSlack, "manual:key7"); err != nil {
		t.Fatalf("GetBusOutboxRecord(slack) error = %v", err)
	} else if !ok {
		t.Fatalf("expected slack outbox record")
	}
	if _, ok, err := store.GetBusOutboxRecord(ctx, ChannelTelegram, "manual:key7"); err != nil {
		t.Fatalf("GetBusOutboxRecord(telegram) error = %v", err)
	} else if ok {
		t.Fatalf("did not expect telegram outbox record when slack target exists")
	}
}
