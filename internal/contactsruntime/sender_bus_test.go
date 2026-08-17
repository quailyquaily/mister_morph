package contactsruntime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/contacts"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	linebus "github.com/quailyquaily/mistermorph/internal/bus/adapters/line"
	slackbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/slack"
	telegrambus "github.com/quailyquaily/mistermorph/internal/bus/adapters/telegram"
)

func TestRoutingSenderSendTelegramViaBus(t *testing.T) {
	ctx := context.Background()

	var (
		mu        sync.Mutex
		gotTarget any
		gotText   string
	)
	sendText := func(ctx context.Context, target any, text string, opts telegrambus.SendTextOptions) error {
		mu.Lock()
		defer mu.Unlock()
		gotTarget = target
		gotText = text
		return nil
	}

	sender := newRoutingSenderForBusTest(t, sendText)
	contentType, payloadBase64 := testEnvelopePayload(t, "hello telegram")
	accepted, deduped, err := sender.Send(ctx, contacts.Contact{
		ContactID:       "tg:12345",
		Kind:            contacts.KindHuman,
		Channel:         contacts.ChannelTelegram,
		TGPrivateChatID: 12345,
	}, contacts.ShareDecision{
		ContactID:      "tg:12345",
		ItemID:         "cand_1",
		ContentType:    contentType,
		PayloadBase64:  payloadBase64,
		IdempotencyKey: "manual:tg:1",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !accepted {
		t.Fatalf("accepted mismatch: got %v want true", accepted)
	}
	if deduped {
		t.Fatalf("deduped mismatch: got %v want false", deduped)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotText != "hello telegram" {
		t.Fatalf("text mismatch: got %q want %q", gotText, "hello telegram")
	}
	wantTarget := telegrambus.DeliveryTarget{ChatID: 12345}
	if gotTarget != wantTarget {
		t.Fatalf("target mismatch: got %#v want %#v", gotTarget, wantTarget)
	}
}

func TestRoutingSenderSendTelegramUsernameAliasPrefersPrivateChatID(t *testing.T) {
	ctx := context.Background()
	var gotTarget any
	sender := newRoutingSenderForBusTest(t, func(_ context.Context, target any, _ string, _ telegrambus.SendTextOptions) error {
		gotTarget = target
		return nil
	})
	contentType, payloadBase64 := testEnvelopePayload(t, "hello telegram")

	_, _, err := sender.Send(ctx, contacts.Contact{
		ContactID:       "tg:@alice",
		Kind:            contacts.KindHuman,
		Channel:         contacts.ChannelTelegram,
		TGUsername:      "alice",
		TGPrivateChatID: 12345,
	}, contacts.ShareDecision{
		ContactID:      "tg:@alice",
		ItemID:         "cand_alias_private",
		ContentType:    contentType,
		PayloadBase64:  payloadBase64,
		IdempotencyKey: "manual:tg:alias-private",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	wantTarget := telegrambus.DeliveryTarget{ChatID: 12345}
	if gotTarget != wantTarget {
		t.Fatalf("target = %#v, want %#v", gotTarget, wantTarget)
	}
}

func TestRoutingSenderSendTelegramViaBus_ChatIDHintMatchGroup(t *testing.T) {
	ctx := context.Background()

	var (
		mu        sync.Mutex
		gotTarget any
	)
	sendText := func(ctx context.Context, target any, text string, opts telegrambus.SendTextOptions) error {
		mu.Lock()
		defer mu.Unlock()
		gotTarget = target
		return nil
	}

	sender := newRoutingSenderForBusTest(t, sendText)
	contentType, payloadBase64 := testEnvelopePayload(t, "hello telegram")
	_, _, err := sender.Send(ctx, contacts.Contact{
		ContactID:       "tg:@alice",
		Kind:            contacts.KindHuman,
		Channel:         contacts.ChannelTelegram,
		TGPrivateChatID: 12345,
		TGGroupChatIDs:  []int64{-1007788},
	}, contacts.ShareDecision{
		ContactID:      "tg:@alice",
		ChatID:         "tg:-1007788",
		ItemID:         "cand_hint_group",
		ContentType:    contentType,
		PayloadBase64:  payloadBase64,
		IdempotencyKey: "manual:tg:hint-group",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	wantTarget := telegrambus.DeliveryTarget{ChatID: -1007788}
	if gotTarget != wantTarget {
		t.Fatalf("target mismatch: got %#v want %#v", gotTarget, wantTarget)
	}
}

func TestRoutingSenderSendTelegramViaBus_ChatIDHintMatchGroupTopic(t *testing.T) {
	ctx := context.Background()

	var (
		mu        sync.Mutex
		gotTarget any
	)
	sendText := func(ctx context.Context, target any, text string, opts telegrambus.SendTextOptions) error {
		mu.Lock()
		defer mu.Unlock()
		gotTarget = target
		return nil
	}

	sender := newRoutingSenderForBusTest(t, sendText)
	contentType, payloadBase64 := testEnvelopePayload(t, "hello telegram")
	_, _, err := sender.Send(ctx, contacts.Contact{
		ContactID:       "tg:@alice",
		Kind:            contacts.KindHuman,
		Channel:         contacts.ChannelTelegram,
		TGPrivateChatID: 12345,
		TGGroupChatIDs:  []int64{-1007788},
	}, contacts.ShareDecision{
		ContactID:      "tg:@alice",
		ChatID:         "tg:-1007788_901",
		ItemID:         "cand_hint_group_topic",
		ContentType:    contentType,
		PayloadBase64:  payloadBase64,
		IdempotencyKey: "manual:tg:hint-group-topic",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	wantTarget := telegrambus.DeliveryTarget{ChatID: -1007788, MessageThreadID: 901}
	if gotTarget != wantTarget {
		t.Fatalf("target mismatch: got %#v want %#v", gotTarget, wantTarget)
	}
}

func TestRoutingSenderSendTelegramViaBus_ChatIDHintFallsBackToPrivate(t *testing.T) {
	ctx := context.Background()

	var (
		mu        sync.Mutex
		gotTarget any
	)
	sendText := func(ctx context.Context, target any, text string, opts telegrambus.SendTextOptions) error {
		mu.Lock()
		defer mu.Unlock()
		gotTarget = target
		return nil
	}

	sender := newRoutingSenderForBusTest(t, sendText)
	contentType, payloadBase64 := testEnvelopePayload(t, "hello telegram")
	_, _, err := sender.Send(ctx, contacts.Contact{
		ContactID:       "tg:@alice",
		Kind:            contacts.KindHuman,
		Channel:         contacts.ChannelTelegram,
		TGPrivateChatID: 12345,
		TGGroupChatIDs:  []int64{-1007788},
	}, contacts.ShareDecision{
		ContactID:      "tg:@alice",
		ChatID:         "tg:-1009999",
		ItemID:         "cand_hint_private",
		ContentType:    contentType,
		PayloadBase64:  payloadBase64,
		IdempotencyKey: "manual:tg:hint-private",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	wantTarget := telegrambus.DeliveryTarget{ChatID: 12345}
	if gotTarget != wantTarget {
		t.Fatalf("target mismatch: got %#v want %#v", gotTarget, wantTarget)
	}
}

func TestRoutingSenderSendTelegramViaBus_ChatIDHintNoPrivateFallback(t *testing.T) {
	ctx := context.Background()

	calls := 0
	sendText := func(ctx context.Context, target any, text string, opts telegrambus.SendTextOptions) error {
		calls++
		return nil
	}

	sender := newRoutingSenderForBusTest(t, sendText)
	contentType, payloadBase64 := testEnvelopePayload(t, "hello telegram")
	_, _, err := sender.Send(ctx, contacts.Contact{
		ContactID:      "tg:@alice",
		Kind:           contacts.KindHuman,
		Channel:        contacts.ChannelTelegram,
		TGGroupChatIDs: []int64{-1007788},
	}, contacts.ShareDecision{
		ContactID:      "tg:@alice",
		ChatID:         "tg:-1009999",
		ItemID:         "cand_hint_error",
		ContentType:    contentType,
		PayloadBase64:  payloadBase64,
		IdempotencyKey: "manual:tg:hint-error",
	})
	if err == nil {
		t.Fatalf("Send() expected error when chat_id hint misses and no private fallback")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "no tg_private_chat_id fallback") {
		t.Fatalf("Send() error mismatch: got %q", err.Error())
	}
	if calls != 0 {
		t.Fatalf("send calls mismatch: got %d want 0", calls)
	}
}

func TestRoutingSenderSendSlackViaBus_WithDMTarget(t *testing.T) {
	ctx := context.Background()

	var (
		mu     sync.Mutex
		got    slackbus.DeliveryTarget
		gotRaw any
		gotTxt string
	)
	sendSlack := func(ctx context.Context, target any, text string, opts slackbus.SendTextOptions) error {
		mu.Lock()
		defer mu.Unlock()
		gotRaw = target
		gotTxt = text
		deliveryTarget, ok := target.(slackbus.DeliveryTarget)
		if !ok {
			return fmt.Errorf("target type mismatch: %T", target)
		}
		got = deliveryTarget
		return nil
	}
	sendTelegram := func(ctx context.Context, target any, text string, opts telegrambus.SendTextOptions) error {
		return fmt.Errorf("unexpected telegram send: target=%v text=%q", target, text)
	}

	sender := newRoutingSenderForBusTest(t, sendTelegram, sendSlack)
	contentType, payloadBase64 := testEnvelopePayload(t, "hello slack")
	accepted, deduped, err := sender.Send(ctx, contacts.Contact{
		ContactID:        "slack:T111:U222",
		Kind:             contacts.KindHuman,
		Channel:          contacts.ChannelSlack,
		SlackTeamID:      "T111",
		SlackUserID:      "U222",
		SlackDMChannelID: "D333",
	}, contacts.ShareDecision{
		ContactID:      "slack:T111:U222",
		ItemID:         "cand_slack_1",
		ContentType:    contentType,
		PayloadBase64:  payloadBase64,
		IdempotencyKey: "manual:slack:1",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !accepted {
		t.Fatalf("accepted mismatch: got %v want true", accepted)
	}
	if deduped {
		t.Fatalf("deduped mismatch: got %v want false", deduped)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotRaw == nil {
		t.Fatalf("expected slack send target")
	}
	if got.TeamID != "T111" || got.ChannelID != "D333" {
		t.Fatalf("slack target mismatch: got=%+v want team=T111 channel=D333", got)
	}
	if gotTxt != "hello slack" {
		t.Fatalf("text mismatch: got %q want %q", gotTxt, "hello slack")
	}
}

func TestRoutingSenderSendSlackViaBus_WithChatIDHint(t *testing.T) {
	ctx := context.Background()

	var (
		mu  sync.Mutex
		got slackbus.DeliveryTarget
	)
	sendSlack := func(ctx context.Context, target any, text string, opts slackbus.SendTextOptions) error {
		mu.Lock()
		defer mu.Unlock()
		deliveryTarget, ok := target.(slackbus.DeliveryTarget)
		if !ok {
			return fmt.Errorf("target type mismatch: %T", target)
		}
		got = deliveryTarget
		return nil
	}

	sender := newRoutingSenderForBusTest(
		t,
		func(ctx context.Context, target any, text string, opts telegrambus.SendTextOptions) error {
			return fmt.Errorf("unexpected telegram send: target=%v text=%q", target, text)
		},
		sendSlack,
	)
	contentType, payloadBase64 := testEnvelopePayload(t, "hello slack by hint")
	_, _, err := sender.Send(ctx, contacts.Contact{
		ContactID: "contact:any",
		Kind:      contacts.KindHuman,
		Channel:   contacts.ChannelTelegram,
	}, contacts.ShareDecision{
		ContactID:      "contact:any",
		ChatID:         "slack:T999:C888",
		ItemID:         "cand_slack_2",
		ContentType:    contentType,
		PayloadBase64:  payloadBase64,
		IdempotencyKey: "manual:slack:2",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got.TeamID != "T999" || got.ChannelID != "C888" {
		t.Fatalf("slack target mismatch: got=%+v want team=T999 channel=C888", got)
	}
}

func TestRoutingSenderSendLineViaBus(t *testing.T) {
	ctx := context.Background()

	var (
		mu      sync.Mutex
		got     linebus.DeliveryTarget
		gotRaw  any
		gotText string
	)
	sendLine := func(ctx context.Context, target any, text string, opts linebus.SendTextOptions) error {
		mu.Lock()
		defer mu.Unlock()
		gotRaw = target
		gotText = text
		deliveryTarget, ok := target.(linebus.DeliveryTarget)
		if !ok {
			return fmt.Errorf("target type mismatch: %T", target)
		}
		got = deliveryTarget
		return nil
	}

	sender := newRoutingSenderForBusTestWithChannels(
		t,
		func(ctx context.Context, target any, text string, opts telegrambus.SendTextOptions) error {
			return fmt.Errorf("unexpected telegram send: target=%v text=%q", target, text)
		},
		func(ctx context.Context, target any, text string, opts slackbus.SendTextOptions) error {
			return fmt.Errorf("unexpected slack send: target=%v text=%q", target, text)
		},
		sendLine,
	)
	contentType, payloadBase64 := testEnvelopePayload(t, "hello line")
	accepted, deduped, err := sender.Send(ctx, contacts.Contact{
		ContactID:   "line_user:U123",
		Kind:        contacts.KindHuman,
		Channel:     contacts.ChannelLine,
		LineUserID:  "U123",
		LineChatIDs: []string{"Cgroup001"},
	}, contacts.ShareDecision{
		ContactID:      "line_user:U123",
		ItemID:         "cand_line_1",
		ContentType:    contentType,
		PayloadBase64:  payloadBase64,
		IdempotencyKey: "manual:line:1",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !accepted {
		t.Fatalf("accepted mismatch: got %v want true", accepted)
	}
	if deduped {
		t.Fatalf("deduped mismatch: got %v want false", deduped)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotRaw == nil {
		t.Fatalf("expected line send target")
	}
	if got.ChatID != "U123" {
		t.Fatalf("line target mismatch: got=%+v want chat=U123", got)
	}
	if gotText != "hello line" {
		t.Fatalf("text mismatch: got %q want %q", gotText, "hello line")
	}
}

func TestRoutingSenderSendLineViaBus_WithChatIDHint(t *testing.T) {
	ctx := context.Background()

	var (
		mu  sync.Mutex
		got linebus.DeliveryTarget
	)
	sendLine := func(ctx context.Context, target any, text string, opts linebus.SendTextOptions) error {
		mu.Lock()
		defer mu.Unlock()
		deliveryTarget, ok := target.(linebus.DeliveryTarget)
		if !ok {
			return fmt.Errorf("target type mismatch: %T", target)
		}
		got = deliveryTarget
		return nil
	}

	sender := newRoutingSenderForBusTestWithChannels(
		t,
		func(ctx context.Context, target any, text string, opts telegrambus.SendTextOptions) error {
			return fmt.Errorf("unexpected telegram send: target=%v text=%q", target, text)
		},
		func(ctx context.Context, target any, text string, opts slackbus.SendTextOptions) error {
			return fmt.Errorf("unexpected slack send: target=%v text=%q", target, text)
		},
		sendLine,
	)
	contentType, payloadBase64 := testEnvelopePayload(t, "hello line by hint")
	_, _, err := sender.Send(ctx, contacts.Contact{
		ContactID:   "line_user:U123",
		Kind:        contacts.KindHuman,
		Channel:     contacts.ChannelLine,
		LineUserID:  "U123",
		LineChatIDs: []string{"Cgroup001"},
	}, contacts.ShareDecision{
		ContactID:      "line_user:U123",
		ChatID:         "line:Cgroup001",
		ItemID:         "cand_line_2",
		ContentType:    contentType,
		PayloadBase64:  payloadBase64,
		IdempotencyKey: "manual:line:2",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got.ChatID != "Cgroup001" {
		t.Fatalf("line target mismatch: got=%+v want chat=Cgroup001", got)
	}
}

func TestRoutingSenderSendFailsWithoutIdempotencyKey(t *testing.T) {
	ctx := context.Background()

	sender := newRoutingSenderForBusTest(t, func(ctx context.Context, target any, text string, opts telegrambus.SendTextOptions) error {
		return nil
	})
	contentType, payloadBase64 := testEnvelopePayload(t, "hello")
	_, _, err := sender.Send(ctx, contacts.Contact{
		ContactID:       "tg:12345",
		Kind:            contacts.KindHuman,
		Channel:         contacts.ChannelTelegram,
		TGPrivateChatID: 12345,
	}, contacts.ShareDecision{
		ContactID:     "tg:12345",
		ItemID:        "cand_3",
		ContentType:   contentType,
		PayloadBase64: payloadBase64,
	})
	if err == nil {
		t.Fatalf("Send() expected error for empty idempotency_key")
	}
	if got := err.Error(); got != "idempotency_key is required" {
		t.Fatalf("Send() error mismatch: got %q want %q", got, "idempotency_key is required")
	}
}

func TestRoutingSenderSendAgentWithUsernameTarget(t *testing.T) {
	ctx := context.Background()
	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("Decode() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer server.Close()

	sender, err := NewRoutingSender(ctx, SenderOptions{
		TelegramBotToken: "token",
		TelegramBaseURL:  server.URL,
	})
	if err != nil {
		t.Fatalf("NewRoutingSender() error = %v", err)
	}
	defer sender.Close()
	contentType, payloadBase64 := testEnvelopePayload(t, "hello")
	accepted, deduped, err := sender.Send(ctx, contacts.Contact{
		ContactID:  "tg:@alice",
		Kind:       contacts.KindAgent,
		Channel:    contacts.ChannelTelegram,
		TGUsername: "alice",
	}, contacts.ShareDecision{
		ContactID:      "tg:@alice",
		ItemID:         "cand_4",
		ContentType:    contentType,
		PayloadBase64:  payloadBase64,
		IdempotencyKey: "manual:tg:@alice",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !accepted || deduped {
		t.Fatalf("Send() = accepted %v, deduped %v; want true, false", accepted, deduped)
	}
	if gotPath != "/bottoken/sendMessage" {
		t.Fatalf("request path = %q, want %q", gotPath, "/bottoken/sendMessage")
	}
	if gotBody["chat_id"] != "@alice" {
		t.Fatalf("chat_id = %#v, want %q", gotBody["chat_id"], "@alice")
	}
	if gotBody["text"] != "hello" {
		t.Fatalf("text = %#v, want %q", gotBody["text"], "hello")
	}
}

func newRoutingSenderForBusTest(t *testing.T, sendText telegrambus.SendTextFunc, slackSendText ...slackbus.SendTextFunc) *RoutingSender {
	t.Helper()

	if sendText == nil {
		t.Fatalf("sendText is required")
	}
	sendSlack := func(ctx context.Context, target any, text string, opts slackbus.SendTextOptions) error {
		return fmt.Errorf("unexpected slack send: target=%v text=%q", target, text)
	}
	if len(slackSendText) > 0 && slackSendText[0] != nil {
		sendSlack = slackSendText[0]
	}
	sendLine := func(ctx context.Context, target any, text string, opts linebus.SendTextOptions) error {
		return fmt.Errorf("unexpected line send: target=%v text=%q", target, text)
	}
	return newRoutingSenderForBusTestWithChannels(t, sendText, sendSlack, sendLine)
}

func newRoutingSenderForBusTestWithChannels(
	t *testing.T,
	sendTelegram telegrambus.SendTextFunc,
	sendSlack slackbus.SendTextFunc,
	sendLine linebus.SendTextFunc,
) *RoutingSender {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	bus, err := busruntime.NewInproc(busruntime.InprocOptions{
		MaxInFlight: 8,
		Logger:      logger,
	})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}

	telegramDelivery, err := telegrambus.NewDeliveryAdapter(telegrambus.DeliveryAdapterOptions{
		SendText: sendTelegram,
	})
	if err != nil {
		t.Fatalf("NewDeliveryAdapter(telegram) error = %v", err)
	}
	slackDelivery, err := slackbus.NewDeliveryAdapter(slackbus.DeliveryAdapterOptions{
		SendText: sendSlack,
	})
	if err != nil {
		t.Fatalf("NewDeliveryAdapter(slack) error = %v", err)
	}
	lineDelivery, err := linebus.NewDeliveryAdapter(linebus.DeliveryAdapterOptions{
		SendText: sendLine,
	})
	if err != nil {
		t.Fatalf("NewDeliveryAdapter(line) error = %v", err)
	}

	sender := &RoutingSender{
		bus:              bus,
		telegramDelivery: telegramDelivery,
		slackDelivery:    slackDelivery,
		lineDelivery:     lineDelivery,
		pending:          make(map[string]chan deliveryResult),
	}

	busHandler := func(deliverCtx context.Context, msg busruntime.BusMessage) error {
		if msg.Direction != busruntime.DirectionOutbound {
			deliverErr := fmt.Errorf("unsupported direction: %s", msg.Direction)
			if err := sender.completePending(msg.ID, deliveryResult{err: deliverErr}); err != nil {
				return err
			}
			return deliverErr
		}
		var (
			accepted   bool
			deduped    bool
			deliverErr error
		)
		switch msg.Channel {
		case busruntime.ChannelTelegram:
			accepted, deduped, deliverErr = sender.telegramDelivery.Deliver(deliverCtx, msg)
		case busruntime.ChannelSlack:
			accepted, deduped, deliverErr = sender.slackDelivery.Deliver(deliverCtx, msg)
		case busruntime.ChannelLine:
			accepted, deduped, deliverErr = sender.lineDelivery.Deliver(deliverCtx, msg)
		default:
			deliverErr = fmt.Errorf("unsupported outbound channel: %s", msg.Channel)
		}
		if err := sender.completePending(msg.ID, deliveryResult{
			accepted: accepted,
			deduped:  deduped,
			err:      deliverErr,
		}); err != nil {
			return err
		}
		return deliverErr
	}
	for _, topic := range busruntime.AllTopics() {
		if err := sender.bus.Subscribe(topic, busHandler); err != nil {
			t.Fatalf("Subscribe(%s) error = %v", topic, err)
		}
	}

	t.Cleanup(func() {
		_ = sender.Close()
	})
	return sender
}

func testEnvelopePayload(t *testing.T, text string) (string, string) {
	t.Helper()

	sessionID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7() error = %v", err)
	}
	payloadRaw, err := json.Marshal(map[string]any{
		"text":       text,
		"session_id": sessionID.String(),
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return "application/json", base64.RawURLEncoding.EncodeToString(payloadRaw)
}
