package builtin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/contacts"
)

func TestResolveSendPayload_MessageTextAutoSessionID(t *testing.T) {
	now := time.Date(2026, 2, 10, 8, 0, 0, 0, time.UTC)
	contentType, payloadBase64, err := resolveSendPayload(map[string]any{
		"message_text": "hello",
	}, now)
	if err != nil {
		t.Fatalf("resolveSendPayload() error = %v", err)
	}
	if contentType != contactsSendContentType {
		t.Fatalf("content_type mismatch: got %q want %q", contentType, contactsSendContentType)
	}

	envelope := decodeEnvelopePayload(t, payloadBase64)
	if text, _ := envelope["text"].(string); text != "hello" {
		t.Fatalf("text mismatch: got %q want %q", text, "hello")
	}
	if sentAt, _ := envelope["sent_at"].(string); sentAt != now.Format(time.RFC3339) {
		t.Fatalf("sent_at mismatch: got %q want %q", sentAt, now.Format(time.RFC3339))
	}
	sessionID, _ := envelope["session_id"].(string)
	assertUUIDv7(t, sessionID)
}

func TestResolveSendPayload_MessageBase64AutoSessionID(t *testing.T) {
	now := time.Date(2026, 2, 10, 8, 0, 0, 0, time.UTC)
	raw, err := json.Marshal(map[string]any{
		"message_id": "msg_1",
		"text":       "hello",
		"sent_at":    now.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	contentType, payloadBase64, err := resolveSendPayload(map[string]any{
		"message_base64": base64.RawURLEncoding.EncodeToString(raw),
	}, now)
	if err != nil {
		t.Fatalf("resolveSendPayload() error = %v", err)
	}
	if contentType != contactsSendContentType {
		t.Fatalf("content_type mismatch: got %q want %q", contentType, contactsSendContentType)
	}

	envelope := decodeEnvelopePayload(t, payloadBase64)
	if messageID, _ := envelope["message_id"].(string); messageID != "msg_1" {
		t.Fatalf("message_id mismatch: got %q want %q", messageID, "msg_1")
	}
	sessionID, _ := envelope["session_id"].(string)
	assertUUIDv7(t, sessionID)
}

func TestResolveSendPayload_MessageBase64UsesParamSessionID(t *testing.T) {
	now := time.Date(2026, 2, 10, 8, 0, 0, 0, time.UTC)
	providedSessionID := mustUUIDv7(t)
	raw, err := json.Marshal(map[string]any{
		"message_id": "msg_2",
		"text":       "hello",
		"sent_at":    now.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	_, payloadBase64, err := resolveSendPayload(map[string]any{
		"message_base64": base64.RawURLEncoding.EncodeToString(raw),
		"session_id":     providedSessionID,
	}, now)
	if err != nil {
		t.Fatalf("resolveSendPayload() error = %v", err)
	}

	envelope := decodeEnvelopePayload(t, payloadBase64)
	if sessionID, _ := envelope["session_id"].(string); sessionID != providedSessionID {
		t.Fatalf("session_id mismatch: got %q want %q", sessionID, providedSessionID)
	}
}

func TestResolveSendPayload_RejectsInvalidSessionID(t *testing.T) {
	_, _, err := resolveSendPayload(map[string]any{
		"message_text": "hello",
		"session_id":   "not-a-uuidv7",
	}, time.Date(2026, 2, 10, 8, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatalf("resolveSendPayload() expected error for invalid session_id")
	}
	if !strings.Contains(err.Error(), "uuid_v7") {
		t.Fatalf("resolveSendPayload() error mismatch: got %q", err.Error())
	}
}

func TestWithContactsSendRuntimeContextNormalizesAndDedupesTargets(t *testing.T) {
	ctx := WithContactsSendRuntimeContext(context.Background(), ContactsSendRuntimeContext{
		ForbiddenTargetIDs: []string{" tg:@Alice ", "tg:@alice", "slack:t1:d2"},
	})
	runtime, ok := ContactsSendRuntimeContextFromContext(ctx)
	if !ok {
		t.Fatalf("ContactsSendRuntimeContextFromContext() expected ok=true")
	}
	if len(runtime.ForbiddenTargetIDs) != 2 {
		t.Fatalf("forbidden_target_ids len = %d, want 2", len(runtime.ForbiddenTargetIDs))
	}
	if runtime.ForbiddenTargetIDs[0] != "tg:@alice" {
		t.Fatalf("forbidden_target_ids[0] = %q, want %q", runtime.ForbiddenTargetIDs[0], "tg:@alice")
	}
	if runtime.ForbiddenTargetIDs[1] != "slack:T1:D2" {
		t.Fatalf("forbidden_target_ids[1] = %q, want %q", runtime.ForbiddenTargetIDs[1], "slack:T1:D2")
	}
}

func TestContactsSendToolRejectsCurrentConversationCounterpartByContactID(t *testing.T) {
	tool := NewContactsSendTool(ContactsSendToolOptions{Enabled: true})
	ctx := WithContactsSendRuntimeContext(context.Background(), ContactsSendRuntimeContext{
		ForbiddenTargetIDs: []string{"tg:@alice", "tg:28036192"},
	})
	_, err := tool.Execute(ctx, map[string]any{
		"contact_id":   "tg:@Alice",
		"message_text": "hello",
	})
	if err == nil {
		t.Fatalf("Execute() expected error for blocked current counterpart")
	}
	if !strings.Contains(err.Error(), "matches current conversation counterpart") {
		t.Fatalf("Execute() error mismatch: got %q", err.Error())
	}
}

func TestContactsSendToolRejectsCurrentConversationCounterpartByChatID(t *testing.T) {
	tool := NewContactsSendTool(ContactsSendToolOptions{Enabled: true})
	ctx := WithContactsSendRuntimeContext(context.Background(), ContactsSendRuntimeContext{
		ForbiddenTargetIDs: []string{"line:Ucurrent"},
	})
	_, err := tool.Execute(ctx, map[string]any{
		"contact_id":   "line_user:Uother",
		"chat_id":      "line:Ucurrent",
		"message_text": "hello",
	})
	if err == nil {
		t.Fatalf("Execute() expected error for blocked current chat target")
	}
	if !strings.Contains(err.Error(), "matches current conversation counterpart") {
		t.Fatalf("Execute() error mismatch: got %q", err.Error())
	}
}

func TestParseContactsSendContactIDsSplitsAndDedupes(t *testing.T) {
	ids, err := parseContactsSendContactIDs(map[string]any{
		"contact_id": " tg:@john_wick, tg:@rose, tg:@john_wick ",
	})
	if err != nil {
		t.Fatalf("parseContactsSendContactIDs() error = %v", err)
	}
	want := []string{"tg:@john_wick", "tg:@rose"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("ids = %#v, want %#v", ids, want)
	}
}

func TestPlanContactsSendBatchUsesSharedTelegramChatAndMentions(t *testing.T) {
	recipients := []contactsSendRecipient{
		{
			ContactID: "tg:@john_wick",
			Contact: contacts.Contact{
				ContactID:       "tg:@john_wick",
				TGUsername:      "john_wick",
				TGGroupChatIDs:  []int64{-1001},
				TGPrivateChatID: 2001,
			},
		},
		{
			ContactID: "tg:@rose",
			Contact: contacts.Contact{
				ContactID:       "tg:@rose",
				TGUsername:      "rose",
				TGGroupChatIDs:  []int64{-1001},
				TGPrivateChatID: 2002,
			},
		},
		{
			ContactID: "tg:@ada",
			Contact: contacts.Contact{
				ContactID:      "tg:@ada",
				TGUsername:     "ada",
				TGGroupChatIDs: []int64{-2001},
			},
		},
	}

	plan, err := planContactsSendBatch(recipients)
	if err != nil {
		t.Fatalf("planContactsSendBatch() error = %v", err)
	}
	if len(plan) != 2 {
		t.Fatalf("plan len = %d, want 2: %#v", len(plan), plan)
	}
	if plan[0].ChatID != "tg:-1001" {
		t.Fatalf("first chat_id = %q, want tg:-1001", plan[0].ChatID)
	}
	if got := plan[0].Text("Hello, world"); got != "@john_wick @rose Hello, world" {
		t.Fatalf("first text = %q", got)
	}
	if plan[1].ChatID != "tg:-2001" {
		t.Fatalf("second chat_id = %q, want tg:-2001", plan[1].ChatID)
	}
	if got := plan[1].Text("Hello, world"); got != "@ada Hello, world" {
		t.Fatalf("second text = %q", got)
	}
}

func TestContactsSendMentionSyntaxForSlackAndLark(t *testing.T) {
	slack := contactsSendMentionForContact(contacts.Contact{
		ContactID:   "slack:T111:U222",
		SlackTeamID: "T111",
		SlackUserID: "U222",
	}, contacts.ChannelSlack)
	if slack != "<@U222>" {
		t.Fatalf("slack mention = %q, want <@U222>", slack)
	}

	lark := contactsSendMentionForContact(contacts.Contact{
		ContactID:       "lark_user:ou_123",
		ContactNickname: "Ada",
		LarkOpenID:      "ou_123",
	}, contacts.ChannelLark)
	if lark != `<at user_id="ou_123">Ada</at>` {
		t.Fatalf("lark mention = %q", lark)
	}
}

func TestPlanContactsSendBatchUsesSharedSlackChatAndMentions(t *testing.T) {
	recipients := []contactsSendRecipient{
		{
			ContactID: "slack:T111:U222",
			Contact: contacts.Contact{
				ContactID:       "slack:T111:U222",
				SlackTeamID:     "T111",
				SlackUserID:     "U222",
				SlackChannelIDs: []string{"C999"},
			},
		},
		{
			ContactID: "slack:T111:U333",
			Contact: contacts.Contact{
				ContactID:       "slack:T111:U333",
				SlackTeamID:     "T111",
				SlackUserID:     "U333",
				SlackChannelIDs: []string{"C999"},
			},
		},
	}

	plan, err := planContactsSendBatch(recipients)
	if err != nil {
		t.Fatalf("planContactsSendBatch() error = %v", err)
	}
	if len(plan) != 1 {
		t.Fatalf("plan len = %d, want 1: %#v", len(plan), plan)
	}
	if plan[0].ChatID != "slack:T111:C999" {
		t.Fatalf("chat_id = %q, want slack:T111:C999", plan[0].ChatID)
	}
	if got := plan[0].Text("Hello, world"); got != "<@U222> <@U333> Hello, world" {
		t.Fatalf("text = %q", got)
	}
}

func TestPlanContactsSendBatchUsesSharedLarkChatAndMentions(t *testing.T) {
	recipients := []contactsSendRecipient{
		{
			ContactID: "lark_user:ou_222",
			Contact: contacts.Contact{
				ContactID:       "lark_user:ou_222",
				ContactNickname: "John",
				LarkOpenID:      "ou_222",
				LarkChatIDs:     []string{"oc_999"},
			},
		},
		{
			ContactID: "lark_user:ou_333",
			Contact: contacts.Contact{
				ContactID:       "lark_user:ou_333",
				ContactNickname: "Rose",
				LarkOpenID:      "ou_333",
				LarkChatIDs:     []string{"oc_999"},
			},
		},
	}

	plan, err := planContactsSendBatch(recipients)
	if err != nil {
		t.Fatalf("planContactsSendBatch() error = %v", err)
	}
	if len(plan) != 1 {
		t.Fatalf("plan len = %d, want 1: %#v", len(plan), plan)
	}
	if plan[0].ChatID != "lark:oc_999" {
		t.Fatalf("chat_id = %q, want lark:oc_999", plan[0].ChatID)
	}
	want := `<at user_id="ou_222">John</at> <at user_id="ou_333">Rose</at> Hello, world`
	if got := plan[0].Text("Hello, world"); got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestExecuteContactsSendBatchLoopsAndMentionsSharedTelegramChats(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC)
	store := contacts.NewFileStore(filepath.Join(t.TempDir(), "contacts"))
	svc := contacts.NewService(store)
	for _, contact := range []contacts.Contact{
		{
			ContactID:      "tg:@john_wick",
			Kind:           contacts.KindHuman,
			Channel:        contacts.ChannelTelegram,
			TGUsername:     "john_wick",
			TGGroupChatIDs: []int64{-1001},
		},
		{
			ContactID:      "tg:@rose",
			Kind:           contacts.KindHuman,
			Channel:        contacts.ChannelTelegram,
			TGUsername:     "rose",
			TGGroupChatIDs: []int64{-1001},
		},
		{
			ContactID:      "tg:@ada",
			Kind:           contacts.KindHuman,
			Channel:        contacts.ChannelTelegram,
			TGUsername:     "ada",
			TGGroupChatIDs: []int64{-2001},
		},
	} {
		if _, err := svc.UpsertContact(ctx, contact, now); err != nil {
			t.Fatalf("UpsertContact(%q) error = %v", contact.ContactID, err)
		}
	}

	sender := &recordingContactsSendSender{}
	contactIDs, err := parseContactsSendContactIDs(map[string]any{
		"contact_id": "tg:@john_wick,tg:@rose,tg:@ada",
	})
	if err != nil {
		t.Fatalf("parseContactsSendContactIDs() error = %v", err)
	}
	_, err = executeContactsSendResolved(ctx, map[string]any{
		"contact_id":   "tg:@john_wick,tg:@rose,tg:@ada",
		"message_text": "Hello, world",
	}, contactIDs, "", svc, sender, now)
	if err != nil {
		t.Fatalf("executeContactsSendResolved() error = %v", err)
	}

	if len(sender.calls) != 2 {
		t.Fatalf("sender calls len = %d, want 2", len(sender.calls))
	}
	if sender.calls[0].decision.ChatID != "tg:-1001" {
		t.Fatalf("first chat_id = %q, want tg:-1001", sender.calls[0].decision.ChatID)
	}
	if got := decodeEnvelopePayload(t, sender.calls[0].decision.PayloadBase64)["text"]; got != "@john_wick @rose Hello, world" {
		t.Fatalf("first text = %v", got)
	}
	if sender.calls[1].decision.ChatID != "tg:-2001" {
		t.Fatalf("second chat_id = %q, want tg:-2001", sender.calls[1].decision.ChatID)
	}
	if got := decodeEnvelopePayload(t, sender.calls[1].decision.PayloadBase64)["text"]; got != "@ada Hello, world" {
		t.Fatalf("second text = %v", got)
	}
	for _, contactID := range []string{"tg:@john_wick", "tg:@rose", "tg:@ada"} {
		contact, ok, err := store.GetContact(ctx, contactID)
		if err != nil {
			t.Fatalf("GetContact(%q) error = %v", contactID, err)
		}
		if !ok {
			t.Fatalf("GetContact(%q) ok = false", contactID)
		}
		if contact.LastInteractionAt == nil || !contact.LastInteractionAt.Equal(now) {
			t.Fatalf("%s LastInteractionAt = %v, want %v", contactID, contact.LastInteractionAt, now)
		}
		if contact.CooldownUntil != nil {
			t.Fatalf("%s CooldownUntil = %v, want nil", contactID, contact.CooldownUntil)
		}
	}
}

func TestExecuteContactsSendBatchRecordsFailureCooldownForAllMergedRecipients(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 2, 10, 12, 15, 0, 0, time.UTC)
	store := contacts.NewFileStore(filepath.Join(t.TempDir(), "contacts"))
	svc := contacts.NewServiceWithOptions(store, contacts.ServiceOptions{FailureCooldown: time.Hour})
	for _, contact := range []contacts.Contact{
		{
			ContactID:      "tg:@john_wick",
			Kind:           contacts.KindHuman,
			Channel:        contacts.ChannelTelegram,
			TGUsername:     "john_wick",
			TGGroupChatIDs: []int64{-1001},
		},
		{
			ContactID:      "tg:@rose",
			Kind:           contacts.KindHuman,
			Channel:        contacts.ChannelTelegram,
			TGUsername:     "rose",
			TGGroupChatIDs: []int64{-1001},
		},
	} {
		if _, err := svc.UpsertContact(ctx, contact, now); err != nil {
			t.Fatalf("UpsertContact(%q) error = %v", contact.ContactID, err)
		}
	}

	sender := &recordingContactsSendSender{err: errors.New("send failed")}
	contactIDs, err := parseContactsSendContactIDs(map[string]any{
		"contact_id": "tg:@john_wick,tg:@rose",
	})
	if err != nil {
		t.Fatalf("parseContactsSendContactIDs() error = %v", err)
	}
	_, err = executeContactsSendResolved(ctx, map[string]any{
		"contact_id":   "tg:@john_wick,tg:@rose",
		"message_text": "Hello, world",
	}, contactIDs, "", svc, sender, now)
	if err != nil {
		t.Fatalf("executeContactsSendResolved() error = %v", err)
	}

	wantCooldown := now.Add(time.Hour)
	for _, contactID := range []string{"tg:@john_wick", "tg:@rose"} {
		contact, ok, err := store.GetContact(ctx, contactID)
		if err != nil {
			t.Fatalf("GetContact(%q) error = %v", contactID, err)
		}
		if !ok {
			t.Fatalf("GetContact(%q) ok = false", contactID)
		}
		if contact.CooldownUntil == nil || !contact.CooldownUntil.Equal(wantCooldown) {
			t.Fatalf("%s CooldownUntil = %v, want %v", contactID, contact.CooldownUntil, wantCooldown)
		}
		if contact.LastInteractionAt != nil {
			t.Fatalf("%s LastInteractionAt = %v, want nil", contactID, contact.LastInteractionAt)
		}
	}
}

func TestExecuteContactsSendSinglePrefixesTelegramMention(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 2, 10, 12, 30, 0, 0, time.UTC)
	store := contacts.NewFileStore(filepath.Join(t.TempDir(), "contacts"))
	svc := contacts.NewService(store)
	if _, err := svc.UpsertContact(ctx, contacts.Contact{
		ContactID:       "tg:@ballcatcat",
		Kind:            contacts.KindHuman,
		Channel:         contacts.ChannelTelegram,
		TGUsername:      "ballcatcat",
		TGPrivateChatID: 28036192,
	}, now); err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}

	sender := &recordingContactsSendSender{}
	contactIDs, err := parseContactsSendContactIDs(map[string]any{
		"contact_id": "tg:@ballcatcat",
	})
	if err != nil {
		t.Fatalf("parseContactsSendContactIDs() error = %v", err)
	}
	_, err = executeContactsSendResolved(ctx, map[string]any{
		"contact_id":   "tg:@ballcatcat",
		"message_text": "看电视哦！👀",
	}, contactIDs, "", svc, sender, now)
	if err != nil {
		t.Fatalf("executeContactsSendResolved() error = %v", err)
	}

	if len(sender.calls) != 1 {
		t.Fatalf("sender calls len = %d, want 1", len(sender.calls))
	}
	if got := decodeEnvelopePayload(t, sender.calls[0].decision.PayloadBase64)["text"]; got != "@ballcatcat 看电视哦！👀" {
		t.Fatalf("text = %v", got)
	}
}

func TestExecuteContactsSendSingleChatIDTarget(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC)
	contactsDir := filepath.Join(t.TempDir(), "contacts")
	store := contacts.NewFileStore(contactsDir)
	svc := contacts.NewService(store)
	sender := &recordingContactsSendSender{}

	_, err := executeContactsSendResolved(ctx, map[string]any{
		"contact_id":   "tg:-100123",
		"chat_id":      "tg:-100123",
		"message_text": "hello room",
	}, []string{"tg:-100123"}, "tg:-100123", svc, sender, now)
	if err != nil {
		t.Fatalf("executeContactsSendResolved() error = %v", err)
	}
	if len(sender.calls) != 1 {
		t.Fatalf("sender calls = %d, want 1", len(sender.calls))
	}
	call := sender.calls[0]
	if call.contact.ContactID != "tg:-100123" || call.contact.Channel != contacts.ChannelTelegram || len(call.contact.TGGroupChatIDs) != 1 || call.contact.TGGroupChatIDs[0] != -100123 {
		t.Fatalf("contact mismatch: %#v", call.contact)
	}
	if call.decision.ContactID != "tg:-100123" || call.decision.ChatID != "tg:-100123" {
		t.Fatalf("decision mismatch: %#v", call.decision)
	}
	if _, ok, err := store.GetContact(ctx, "tg:-100123"); err != nil {
		t.Fatalf("GetContact() error = %v", err)
	} else if ok {
		t.Fatalf("synthetic chat target should not be persisted as contact")
	}
}

func TestExecuteContactsSendSingleChatIDTargetUsesContactIDWhenChatIDEmpty(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC)
	contactsDir := filepath.Join(t.TempDir(), "contacts")
	svc := contacts.NewService(contacts.NewFileStore(contactsDir))
	sender := &recordingContactsSendSender{}

	_, err := executeContactsSendResolved(ctx, map[string]any{
		"contact_id":   "tg:-100123_77",
		"message_text": "hello topic",
	}, []string{"tg:-100123_77"}, "", svc, sender, now)
	if err != nil {
		t.Fatalf("executeContactsSendResolved() error = %v", err)
	}
	if len(sender.calls) != 1 {
		t.Fatalf("sender calls = %d, want 1", len(sender.calls))
	}
	if sender.calls[0].decision.ChatID != "tg:-100123_77" {
		t.Fatalf("decision chat_id = %q, want tg:-100123_77", sender.calls[0].decision.ChatID)
	}
}

func TestExecuteContactsSendSingleChatIDTargetRejectsMismatchedHint(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC)
	contactsDir := filepath.Join(t.TempDir(), "contacts")
	svc := contacts.NewService(contacts.NewFileStore(contactsDir))
	sender := &recordingContactsSendSender{}

	_, err := executeContactsSendResolved(ctx, map[string]any{
		"contact_id":   "tg:-100123",
		"chat_id":      "tg:-100456",
		"message_text": "hello room",
	}, []string{"tg:-100123"}, "tg:-100456", svc, sender, now)
	if err == nil {
		t.Fatalf("executeContactsSendResolved() expected mismatched chat_id error")
	}
	if !strings.Contains(err.Error(), "chat_id must be empty or match contact_id") {
		t.Fatalf("executeContactsSendResolved() error mismatch: %q", err.Error())
	}
	if len(sender.calls) != 0 {
		t.Fatalf("sender calls = %d, want 0", len(sender.calls))
	}
}

func TestContactsSendBaseMessageTextRejectsInvalidEnvelope(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"message_id": "msg_1",
		"text":       "hello",
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	_, err = contactsSendBaseMessageText(map[string]any{
		"message_base64": base64.RawURLEncoding.EncodeToString(raw),
	})
	if err == nil {
		t.Fatal("contactsSendBaseMessageText() expected error")
	}
	if !strings.Contains(err.Error(), "sent_at") {
		t.Fatalf("contactsSendBaseMessageText() error = %q", err.Error())
	}
}

type recordingContactsSendSender struct {
	calls []recordingContactsSendCall
	err   error
}

type recordingContactsSendCall struct {
	contact  contacts.Contact
	decision contacts.ShareDecision
}

func (s *recordingContactsSendSender) Send(_ context.Context, contact contacts.Contact, decision contacts.ShareDecision) (bool, bool, error) {
	s.calls = append(s.calls, recordingContactsSendCall{
		contact:  contact,
		decision: decision,
	})
	if s.err != nil {
		return false, false, s.err
	}
	return true, false, nil
}

func decodeEnvelopePayload(t *testing.T, payloadBase64 string) map[string]any {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(payloadBase64)
	if err != nil {
		t.Fatalf("base64 decode error = %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return envelope
}

func mustUUIDv7(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7() error = %v", err)
	}
	return id.String()
}

func assertUUIDv7(t *testing.T, value string) {
	t.Helper()
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		t.Fatalf("uuid.Parse() error = %v", err)
	}
	if parsed.Version() != uuid.Version(7) {
		t.Fatalf("uuid version mismatch: got %d want %d", parsed.Version(), 7)
	}
}
