package contactsruntime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/contacts"
	mixinbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/mixin"
	"github.com/quailyquaily/mistermorph/internal/mixinapi"
	"github.com/quailyquaily/mistermorph/internal/testhttp"
)

type fakeMixinSenderClient struct {
	conversation mixinapi.Conversation
	readCalls    int
	batches      [][]mixinapi.MessageRequest
}

func (f *fakeMixinSenderClient) ReadConversation(context.Context, string) (mixinapi.Conversation, error) {
	f.readCalls++
	return f.conversation, nil
}

func (f *fakeMixinSenderClient) CreateContactConversation(context.Context, string) (mixinapi.Conversation, error) {
	return mixinapi.Conversation{}, nil
}

func (f *fakeMixinSenderClient) SendMessages(_ context.Context, messages []mixinapi.MessageRequest) error {
	f.batches = append(f.batches, append([]mixinapi.MessageRequest(nil), messages...))
	return nil
}

func TestBuildEnvelopePayload_RequiresSessionID(t *testing.T) {
	now := time.Date(2026, 2, 7, 4, 31, 30, 0, time.UTC)
	decision := contacts.ShareDecision{
		ContentType:    "text/plain",
		PayloadBase64:  base64.RawURLEncoding.EncodeToString([]byte("hello")),
		IdempotencyKey: "manual:1",
		ItemID:         "cand_1",
	}

	_, err := buildEnvelopePayload(decision, decision.ContentType, decision.PayloadBase64, now)
	if err == nil {
		t.Fatalf("expected error when session_id is missing")
	}
}

func TestBuildEnvelopePayload_FromJSON(t *testing.T) {
	now := time.Date(2026, 2, 7, 4, 32, 0, 0, time.UTC)
	payload := map[string]any{
		"text":       "pong",
		"session_id": "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f456",
		"reply_to":   "msg_prev",
	}
	payloadRaw, _ := json.Marshal(payload)
	decision := contacts.ShareDecision{
		ContentType:    "application/json",
		PayloadBase64:  base64.RawURLEncoding.EncodeToString(payloadRaw),
		IdempotencyKey: "manual:2",
		ItemID:         "",
	}

	raw, err := buildEnvelopePayload(decision, decision.ContentType, decision.PayloadBase64, now)
	if err != nil {
		t.Fatalf("buildEnvelopePayload() error = %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if got := envelope["text"]; got != "pong" {
		t.Fatalf("text mismatch: got %v want pong", got)
	}
	if got := envelope["session_id"]; got != "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f456" {
		t.Fatalf("session_id mismatch: got %v", got)
	}
	if got := envelope["reply_to"]; got != "msg_prev" {
		t.Fatalf("reply_to mismatch: got %v want msg_prev", got)
	}
}

func TestBuildEnvelopePayload_InvalidPayload(t *testing.T) {
	now := time.Date(2026, 2, 7, 4, 34, 0, 0, time.UTC)
	decision := contacts.ShareDecision{
		PayloadBase64: "***invalid***",
	}

	if _, err := buildEnvelopePayload(decision, "application/json", decision.PayloadBase64, now); err == nil {
		t.Fatalf("expected error for invalid payload_base64")
	}
}

func TestResolveLineTarget(t *testing.T) {
	target, err := ResolveLineTarget(contacts.Contact{
		ContactID:   "line_user:U001",
		Channel:     contacts.ChannelLine,
		LineUserID:  "U001",
		LineChatIDs: []string{"Cgroup001"},
	})
	if err != nil {
		t.Fatalf("ResolveLineTarget() error = %v", err)
	}
	if target.ChatID != "U001" {
		t.Fatalf("ResolveLineTarget() mismatch: target=%+v", target)
	}
}

func TestResolveLineTargetWithChatIDHint(t *testing.T) {
	target, err := ResolveLineTargetWithChatID(contacts.Contact{
		ContactID:   "line_user:U001",
		Channel:     contacts.ChannelLine,
		LineUserID:  "U001",
		LineChatIDs: []string{"Cgroup001"},
	}, "line:Cgroup001")
	if err != nil {
		t.Fatalf("ResolveLineTargetWithChatID() error = %v", err)
	}
	if target.ChatID != "Cgroup001" {
		t.Fatalf("ResolveLineTargetWithChatID() mismatch: target=%+v", target)
	}
}

func TestResolveLarkTarget(t *testing.T) {
	target, err := ResolveLarkTarget(contacts.Contact{
		ContactID:  "lark_user:ou_001",
		Channel:    contacts.ChannelLark,
		LarkOpenID: "ou_001",
		LarkChatIDs: []string{
			"oc_group001",
		},
	})
	if err != nil {
		t.Fatalf("ResolveLarkTarget() error = %v", err)
	}
	if target.ReceiveIDType != "open_id" || target.ReceiveID != "ou_001" {
		t.Fatalf("ResolveLarkTarget() mismatch: target=%+v", target)
	}
}

func TestResolveLarkTargetWithChatIDHint(t *testing.T) {
	target, err := ResolveLarkTargetWithChatID(contacts.Contact{
		ContactID:  "lark_user:ou_001",
		Channel:    contacts.ChannelLark,
		LarkOpenID: "ou_001",
		LarkChatIDs: []string{
			"oc_group001",
		},
	}, "lark:oc_group001")
	if err != nil {
		t.Fatalf("ResolveLarkTargetWithChatID() error = %v", err)
	}
	if target.ReceiveIDType != "chat_id" || target.ReceiveID != "oc_group001" {
		t.Fatalf("ResolveLarkTargetWithChatID() mismatch: target=%+v", target)
	}
}

func TestResolveMixinTarget(t *testing.T) {
	const userID = "773e5e77-4107-45c2-b648-8fc722ed77f5"
	const chatID = "8f7059b9-b1b2-4ed8-a99f-4ac2f07a9a34"
	contact := contacts.Contact{
		ContactID: "mixin:" + userID, Channel: contacts.ChannelMixin,
		MixinUserID: userID, MixinChatIDs: []string{chatID},
	}
	target, err := ResolveMixinTarget(contact)
	if err != nil || target.UserID != userID || target.ConversationID != "" {
		t.Fatalf("ResolveMixinTarget() = %#v, %v", target, err)
	}
	target, err = ResolveMixinTargetWithChatID(contact, "mixin:"+chatID)
	if err != nil || target.ConversationID != chatID || target.UserID != "" {
		t.Fatalf("ResolveMixinTargetWithChatID() = %#v, %v", target, err)
	}
}

func TestNewRoutingSenderDefersMixinKeystoreLoad(t *testing.T) {
	sender, err := NewRoutingSender(context.Background(), SenderOptions{MixinKeystoreFile: "/missing/mixin-keystore.json"})
	if err != nil {
		t.Fatalf("NewRoutingSender() error = %v", err)
	}
	defer sender.Close()
}

func TestSendMixinTargetExpandsConversationParticipants(t *testing.T) {
	const conversationID = "8f7059b9-b1b2-4ed8-a99f-4ac2f07a9a34"
	const botID = "773e5e77-4107-45c2-b648-8fc722ed77f5"
	const messageID = "7ea3356d-3c57-4ef5-a49c-71f2ba7cb312"
	participants := []mixinapi.ConversationParticipant{{UserID: botID}}
	for index := range 205 {
		participants = append(participants, mixinapi.ConversationParticipant{
			UserID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(strconv.Itoa(index))).String(),
		})
	}
	participants = append(participants, participants[1])
	client := &fakeMixinSenderClient{conversation: mixinapi.Conversation{
		ConversationID: conversationID,
		Category:       mixinapi.ConversationCategoryGroup,
		Participants:   participants,
	}}
	sender := &RoutingSender{mixinClient: client, mixinBotID: botID}

	if err := sender.sendMixinTarget(context.Background(), mixinbus.DeliveryTarget{
		ConversationID: conversationID,
	}, "hello", mixinbus.SendTextOptions{MessageID: messageID}); err != nil {
		t.Fatalf("sendMixinTarget() error = %v", err)
	}
	if client.readCalls != 1 {
		t.Fatalf("ReadConversation() calls = %d, want 1", client.readCalls)
	}
	if len(client.batches) != 3 || len(client.batches[0]) != 100 || len(client.batches[1]) != 100 || len(client.batches[2]) != 5 {
		t.Fatalf("batch sizes = %d/%d/%d", len(client.batches[0]), len(client.batches[1]), len(client.batches[2]))
	}
	seenRecipients := make(map[string]struct{}, 205)
	seenMessages := make(map[string]struct{}, 205)
	for _, batch := range client.batches {
		for _, message := range batch {
			if message.ConversationID != conversationID || message.RecipientID == "" || message.RecipientID == botID {
				t.Fatalf("message target = %#v", message)
			}
			if _, found := seenRecipients[message.RecipientID]; found {
				t.Fatalf("duplicate recipient_id %q", message.RecipientID)
			}
			seenRecipients[message.RecipientID] = struct{}{}
			if _, found := seenMessages[message.MessageID]; found {
				t.Fatalf("duplicate message_id %q", message.MessageID)
			}
			seenMessages[message.MessageID] = struct{}{}
		}
	}
}

func TestSendMixinTargetSplitsConversationBatchByRequestSize(t *testing.T) {
	const conversationID = "8f7059b9-b1b2-4ed8-a99f-4ac2f07a9a34"
	const botID = "773e5e77-4107-45c2-b648-8fc722ed77f5"
	client := &fakeMixinSenderClient{conversation: mixinapi.Conversation{
		ConversationID: conversationID,
		Category:       mixinapi.ConversationCategoryGroup,
		Participants: []mixinapi.ConversationParticipant{
			{UserID: botID},
			{UserID: "0fd3794c-a497-4c5b-8845-c82c9d7814d1"},
			{UserID: "6c12f32e-07bf-4a9a-bb39-a8c08886f608"},
		},
	}}
	sender := &RoutingSender{mixinClient: client, mixinBotID: botID}

	if err := sender.sendMixinTarget(context.Background(), mixinbus.DeliveryTarget{
		ConversationID: conversationID,
	}, strings.Repeat("x", 60<<10), mixinbus.SendTextOptions{
		MessageID: "7ea3356d-3c57-4ef5-a49c-71f2ba7cb312",
	}); err != nil {
		t.Fatalf("sendMixinTarget() error = %v", err)
	}
	if len(client.batches) != 2 {
		t.Fatalf("batch count = %d, want 2", len(client.batches))
	}
	if len(client.batches[0]) != 1 || len(client.batches[1]) != 1 {
		t.Fatalf("batch sizes = %d/%d, want 1/1", len(client.batches[0]), len(client.batches[1]))
	}
}

func TestRoutingSenderSendLarkDirect(t *testing.T) {
	ctx := context.Background()

	var tokenHits int32
	var messageHits int32
	var gotAuth string
	var gotReceiveIDType string
	var gotReceiveID string
	var gotContent string

	serverURL := testhttp.WithDefaultTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/auth/v3/tenant_access_token/internal":
			atomic.AddInt32(&tokenHits, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"tenant_token_1","expire":7200}`))
		case r.URL.Path == "/im/v1/messages":
			atomic.AddInt32(&messageHits, 1)
			gotAuth = r.Header.Get("Authorization")
			gotReceiveIDType = r.URL.Query().Get("receive_id_type")
			var payload struct {
				ReceiveID string `json:"receive_id"`
				Content   string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			gotReceiveID = payload.ReceiveID
			gotContent = payload.Content
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"message_id":"om_001"}}`))
		default:
			http.NotFound(w, r)
		}
	}))

	sender, err := NewRoutingSender(ctx, SenderOptions{
		LarkAppID:         "cli_test",
		LarkAppSecret:     "secret_test",
		LarkBaseURL:       serverURL,
		MixinKeystoreFile: "/missing/mixin-keystore.json",
	})
	if err != nil {
		t.Fatalf("NewRoutingSender() error = %v", err)
	}
	defer sender.Close()

	contentType, payloadBase64 := testEnvelopePayload(t, "hello lark")
	accepted, deduped, err := sender.Send(ctx, contacts.Contact{
		ContactID:  "lark_user:ou_123",
		Kind:       contacts.KindHuman,
		Channel:    contacts.ChannelLark,
		LarkOpenID: "ou_123",
		LarkChatIDs: []string{
			"oc_group001",
		},
	}, contacts.ShareDecision{
		ContactID:      "lark_user:ou_123",
		ItemID:         "cand_lark_1",
		ContentType:    contentType,
		PayloadBase64:  payloadBase64,
		IdempotencyKey: "manual:lark:1",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !accepted || deduped {
		t.Fatalf("Send() accepted=%v deduped=%v, want true false", accepted, deduped)
	}
	if atomic.LoadInt32(&tokenHits) != 1 {
		t.Fatalf("token hits = %d, want 1", atomic.LoadInt32(&tokenHits))
	}
	if atomic.LoadInt32(&messageHits) != 1 {
		t.Fatalf("message hits = %d, want 1", atomic.LoadInt32(&messageHits))
	}
	if gotAuth != "Bearer tenant_token_1" {
		t.Fatalf("authorization mismatch: got %q", gotAuth)
	}
	if gotReceiveIDType != "open_id" {
		t.Fatalf("receive_id_type mismatch: got %q want open_id", gotReceiveIDType)
	}
	if gotReceiveID != "ou_123" {
		t.Fatalf("receive_id mismatch: got %q want ou_123", gotReceiveID)
	}
	if !strings.Contains(gotContent, "hello lark") {
		t.Fatalf("content mismatch: got %q", gotContent)
	}
}
