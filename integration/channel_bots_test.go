package integration

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"testing"

	slackruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/slack"
	telegramruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/telegram"
)

func TestNewTelegramBotRequiresToken(t *testing.T) {
	rt := New(DefaultConfig())

	if _, err := rt.NewTelegramBot(TelegramOptions{}); err == nil {
		t.Fatalf("expected error when telegram bot token is missing")
	}
}

func TestNewSlackBotRequiresTokens(t *testing.T) {
	rt := New(DefaultConfig())

	if _, err := rt.NewSlackBot(SlackOptions{}); err == nil {
		t.Fatalf("expected error when slack tokens are missing")
	}
	if _, err := rt.NewSlackBot(SlackOptions{BotToken: "xoxb-1"}); err == nil {
		t.Fatalf("expected error when slack app token is missing")
	}
}

func TestNewMixinBotValidatesCredentials(t *testing.T) {
	rt := New(DefaultConfig())
	if _, err := rt.NewMixinBot(MixinOptions{}); err == nil {
		t.Fatal("expected error when Mixin credentials are missing")
	}
	opts := validMixinOptions()
	runner, err := rt.NewMixinBot(opts)
	if err != nil {
		t.Fatalf("NewMixinBot() error = %v", err)
	}
	if runner == nil {
		t.Fatal("NewMixinBot() runner = nil")
	}
}

func validMixinOptions() MixinOptions {
	return MixinOptions{
		ClientID:   "773e5e77-4107-45c2-b648-8fc722ed77f5",
		SessionID:  "a34c07a9-755d-4b54-94c5-e45e9a2dd43e",
		PrivateKey: hex.EncodeToString(bytes.Repeat([]byte{0x42}, ed25519.SeedSize)),
	}
}

func TestNewChannelBotRejectsInvalidRuntime(t *testing.T) {
	initErr := errors.New("invalid runtime config")
	rt := New(DefaultConfig())
	rt.snap.InitErr = initErr

	tests := []struct {
		name string
		new  func() (BotRunner, error)
	}{
		{
			name: "telegram",
			new: func() (BotRunner, error) {
				return rt.NewTelegramBot(TelegramOptions{BotToken: "test"})
			},
		},
		{
			name: "slack",
			new: func() (BotRunner, error) {
				return rt.NewSlackBot(SlackOptions{BotToken: "xoxb-test", AppToken: "xapp-test"})
			},
		},
		{
			name: "mixin",
			new: func() (BotRunner, error) {
				return rt.NewMixinBot(validMixinOptions())
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner, err := test.new()
			if runner != nil {
				t.Fatal("bot constructor returned a runner for an invalid runtime")
			}
			if !errors.Is(err, initErr) {
				t.Fatalf("bot constructor error = %v, want %v", err, initErr)
			}
		})
	}
}

func TestRunnerCloseAndReentrantGuard(t *testing.T) {
	rt := New(DefaultConfig())
	r, err := rt.NewSlackBot(SlackOptions{
		BotToken: "xoxb-1",
		AppToken: "xapp-1",
	})
	if err != nil {
		t.Fatalf("NewSlackBot() error = %v", err)
	}

	runner := r.(*slackBotRunner)
	runCtx, runCancel, err := runner.state.begin(context.Background(), "slack")
	if err != nil {
		t.Fatalf("beginRun() error = %v", err)
	}
	if runCtx == nil || runCancel == nil {
		t.Fatalf("beginRun() returned nil context/cancel")
	}
	if _, _, err := runner.state.begin(context.Background(), "slack"); err == nil {
		t.Fatalf("expected reentrant beginRun() to fail")
	}
	if err := runner.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case <-runCtx.Done():
		if !errors.Is(runCtx.Err(), context.Canceled) {
			t.Fatalf("run context error = %v, want context canceled", runCtx.Err())
		}
	default:
		t.Fatal("Close() did not cancel the run context")
	}
	runner.state.end(runCancel)
}

func TestTelegramRuntimeHooksBridge(t *testing.T) {
	var inbound TelegramInboundEvent
	var outbound TelegramOutboundEvent
	var errEvent TelegramErrorEvent

	r := &telegramBotRunner{
		opts: TelegramOptions{
			Hooks: TelegramHooks{
				OnInbound: func(event TelegramInboundEvent) { inbound = event },
				OnOutbound: func(event TelegramOutboundEvent) {
					outbound = event
				},
				OnError: func(event TelegramErrorEvent) { errEvent = event },
			},
		},
	}
	hooks := r.runtimeHooks()
	expectedErr := errors.New("boom")

	hooks.OnInbound(context.Background(), telegramruntime.InboundEvent{
		ChatID:       11,
		MessageID:    22,
		ChatType:     "private",
		FromUserID:   33,
		Text:         "hello",
		MentionUsers: []string{"u1"},
	})
	hooks.OnOutbound(context.Background(), telegramruntime.OutboundEvent{
		ChatID:           11,
		ReplyToMessageID: 22,
		Text:             "ok",
		CorrelationID:    "c1",
		Kind:             "message",
	})
	hooks.OnError(context.Background(), telegramruntime.ErrorEvent{
		Stage:     "test_stage",
		ChatID:    11,
		MessageID: 22,
		Err:       expectedErr,
	})

	if inbound.ChatID != 11 || inbound.MessageID != 22 || inbound.Text != "hello" || len(inbound.MentionUsers) != 1 {
		t.Fatalf("unexpected inbound event: %#v", inbound)
	}
	if outbound.ChatID != 11 || outbound.ReplyToMessageID != 22 || outbound.CorrelationID != "c1" {
		t.Fatalf("unexpected outbound event: %#v", outbound)
	}
	if errEvent.Stage != "test_stage" || errEvent.Err != expectedErr {
		t.Fatalf("unexpected error event: %#v", errEvent)
	}
}

func TestSlackRuntimeHooksBridge(t *testing.T) {
	var inbound SlackInboundEvent
	var outbound SlackOutboundEvent
	var errEvent SlackErrorEvent

	r := &slackBotRunner{
		opts: SlackOptions{
			Hooks: SlackHooks{
				OnInbound: func(event SlackInboundEvent) { inbound = event },
				OnOutbound: func(event SlackOutboundEvent) {
					outbound = event
				},
				OnError: func(event SlackErrorEvent) { errEvent = event },
			},
		},
	}
	hooks := r.runtimeHooks()
	expectedErr := errors.New("boom")

	hooks.OnInbound(context.Background(), slackruntime.InboundEvent{
		ConversationKey: "slack:t:c",
		TeamID:          "T",
		ChannelID:       "C",
		MessageTS:       "1.0",
		UserID:          "U",
		Text:            "hello",
	})
	hooks.OnOutbound(context.Background(), slackruntime.OutboundEvent{
		ConversationKey: "slack:t:c",
		TeamID:          "T",
		ChannelID:       "C",
		ThreadTS:        "1.0",
		Text:            "ok",
		CorrelationID:   "c1",
		Kind:            "message",
	})
	hooks.OnError(context.Background(), slackruntime.ErrorEvent{
		Stage:           "test_stage",
		ConversationKey: "slack:t:c",
		TeamID:          "T",
		ChannelID:       "C",
		MessageTS:       "1.0",
		Err:             expectedErr,
	})

	if inbound.ConversationKey != "slack:t:c" || inbound.TeamID != "T" || inbound.Text != "hello" {
		t.Fatalf("unexpected inbound event: %#v", inbound)
	}
	if outbound.ConversationKey != "slack:t:c" || outbound.CorrelationID != "c1" || outbound.Kind != "message" {
		t.Fatalf("unexpected outbound event: %#v", outbound)
	}
	if errEvent.Stage != "test_stage" || errEvent.Err != expectedErr {
		t.Fatalf("unexpected error event: %#v", errEvent)
	}
}
