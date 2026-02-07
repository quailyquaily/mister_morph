package telegramcmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/maep"
	"github.com/quailyquaily/mistermorph/tools"
)

func TestShouldAutoReplyMAEPTopic(t *testing.T) {
	cases := []struct {
		topic string
		want  bool
	}{
		{topic: "dm.checkin.v1", want: true},
		{topic: "chat.message", want: true},
		{topic: "share.proactive.v1", want: true},
		{topic: "dm.reply.v1", want: true},
		{topic: "", want: false},
	}
	for _, tc := range cases {
		if got := shouldAutoReplyMAEPTopic(tc.topic); got != tc.want {
			t.Fatalf("shouldAutoReplyMAEPTopic(%q)=%v want=%v", tc.topic, got, tc.want)
		}
	}
}

func TestExtractMAEPTask_TextPlain(t *testing.T) {
	task, sessionID := extractMAEPTask(maep.DataPushEvent{
		ContentType:  "text/plain",
		PayloadBytes: []byte(" hi there "),
	})
	if task != "hi there" {
		t.Fatalf("task mismatch: got %q", task)
	}
	if sessionID != "" {
		t.Fatalf("session_id should be empty, got %q", sessionID)
	}
}

func TestExtractMAEPTask_JSONPayload(t *testing.T) {
	payload := map[string]any{
		"text":       "hello from peer",
		"session_id": "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f456",
	}
	raw, _ := json.Marshal(payload)
	task, sessionID := extractMAEPTask(maep.DataPushEvent{
		ContentType:   "application/json",
		PayloadBase64: base64.RawURLEncoding.EncodeToString(raw),
	})
	if task != "hello from peer" {
		t.Fatalf("task mismatch: got %q", task)
	}
	if sessionID != "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f456" {
		t.Fatalf("session_id mismatch: got %q", sessionID)
	}
}

func TestMAEPSessionKey(t *testing.T) {
	got := maepSessionKey("peerA", "chat.message", "")
	if got != "peerA::dialogue.v1" {
		t.Fatalf("unexpected key without session_id: %q", got)
	}
	got = maepSessionKey("peerA", "chat.message", "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f456")
	if got != "peerA::session:0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f456" {
		t.Fatalf("unexpected key with session_id: %q", got)
	}
}

func TestAllowMAEPSessionTurn_MaxTurnsAndCooldown(t *testing.T) {
	now := time.Date(2026, 2, 7, 4, 0, 0, 0, time.UTC)
	state := maepSessionState{TurnCount: 6}
	next, allowed := allowMAEPSessionTurn(now, state, 6, 10*time.Minute)
	if allowed {
		t.Fatalf("expected blocked when turn_count reached max")
	}
	if next.CooldownUntil.IsZero() {
		t.Fatalf("expected cooldown to be set")
	}

	next2, allowed2 := allowMAEPSessionTurn(now.Add(5*time.Minute), next, 6, 10*time.Minute)
	if allowed2 {
		t.Fatalf("expected blocked during cooldown")
	}
	if !next2.CooldownUntil.Equal(next.CooldownUntil) {
		t.Fatalf("cooldown changed unexpectedly")
	}

	next3, allowed3 := allowMAEPSessionTurn(now.Add(11*time.Minute), next2, 6, 10*time.Minute)
	if !allowed3 {
		t.Fatalf("expected allowed after cooldown expiry")
	}
	if next3.TurnCount != 0 {
		t.Fatalf("expected turn_count reset after cooldown, got %d", next3.TurnCount)
	}
	if next3.InterestLevel != defaultMAEPInterestLevel {
		t.Fatalf("expected interest reset after cooldown, got %.3f", next3.InterestLevel)
	}
}

func TestApplyMAEPFeedback_UpdatesInterestAndLowRounds(t *testing.T) {
	state := maepSessionState{InterestLevel: 0.6}
	next := applyMAEPFeedback(state, maepFeedbackClassification{
		SignalPositive: 0.0,
		SignalNegative: 1.0,
		SignalBored:    0.0,
		NextAction:     "continue",
		Confidence:     1.0,
	})
	if next.InterestLevel >= 0.6 {
		t.Fatalf("expected interest decrease, got %.3f", next.InterestLevel)
	}
	if next.LowInterestRounds != 0 {
		t.Fatalf("unexpected low_interest_rounds: %d", next.LowInterestRounds)
	}

	next = applyMAEPFeedback(next, maepFeedbackClassification{
		SignalPositive: 0.0,
		SignalNegative: 1.0,
		SignalBored:    1.0,
		NextAction:     "continue",
		Confidence:     1.0,
	})
	if next.InterestLevel >= maepInterestStopThreshold {
		t.Fatalf("expected low interest, got %.3f", next.InterestLevel)
	}
	if next.LowInterestRounds != 1 {
		t.Fatalf("expected low_interest_rounds=1, got %d", next.LowInterestRounds)
	}
}

func TestMaybeLimitMAEPSessionByFeedback(t *testing.T) {
	now := time.Date(2026, 2, 7, 5, 0, 0, 0, time.UTC)

	state := maepSessionState{InterestLevel: 0.6}
	next, blocked, reason := maybeLimitMAEPSessionByFeedback(now, state, maepFeedbackClassification{
		NextAction: "wrap_up",
		Confidence: 0.9,
	}, 10*time.Minute)
	if !blocked || reason != "feedback_wrap_up" {
		t.Fatalf("expected wrap_up block, got blocked=%v reason=%q", blocked, reason)
	}
	if next.CooldownUntil.IsZero() {
		t.Fatalf("expected cooldown on wrap_up")
	}

	state = maepSessionState{InterestLevel: 0.2, LowInterestRounds: 2}
	next, blocked, reason = maybeLimitMAEPSessionByFeedback(now, state, maepFeedbackClassification{
		NextAction: "continue",
		Confidence: 1,
	}, 10*time.Minute)
	if !blocked || reason != "feedback_low_interest" {
		t.Fatalf("expected low_interest block, got blocked=%v reason=%q", blocked, reason)
	}
	if next.CooldownUntil.IsZero() {
		t.Fatalf("expected cooldown on low_interest")
	}
}

func TestClassifyMAEPFeedback_NilClientReturnsDefault(t *testing.T) {
	got, err := classifyMAEPFeedback(context.Background(), nil, "", nil, "hello")
	if err != nil {
		t.Fatalf("classifyMAEPFeedback() error = %v", err)
	}
	if got.NextAction != "continue" {
		t.Fatalf("next_action mismatch: got %q", got.NextAction)
	}
}

type fakeTool struct {
	name string
}

func (f fakeTool) Name() string { return f.name }

func (f fakeTool) Description() string { return "fake" }

func (f fakeTool) ParameterSchema() string { return "{}" }

func (f fakeTool) Execute(ctx context.Context, params map[string]any) (string, error) { return "", nil }

func TestBuildMAEPRegistry_DisablesContactsSend(t *testing.T) {
	base := tools.NewRegistry()
	base.Register(fakeTool{name: "contacts_send"})
	base.Register(fakeTool{name: "read_file"})
	base.Register(fakeTool{name: "echo"})

	reg := buildMAEPRegistry(base)
	if _, ok := reg.Get("contacts_send"); ok {
		t.Fatalf("contacts_send should be disabled for MAEP inbound runs")
	}
	if _, ok := reg.Get("read_file"); !ok {
		t.Fatalf("expected read_file to remain enabled")
	}
	if _, ok := reg.Get("echo"); !ok {
		t.Fatalf("expected echo to remain enabled")
	}
}

func TestApplyMAEPReplyPromptRules(t *testing.T) {
	spec := agent.PromptSpec{
		Rules: []string{"baseline"},
	}
	applyMAEPReplyPromptRules(&spec)
	joined := strings.Join(spec.Rules, "\n")
	if !strings.Contains(joined, "sent verbatim to a remote peer") {
		t.Fatalf("expected maep reply rule to be appended")
	}
	if !strings.Contains(joined, "Never mention topics/protocol labels") {
		t.Fatalf("expected protocol metadata rule to be appended")
	}
}
