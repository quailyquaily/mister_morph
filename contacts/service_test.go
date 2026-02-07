package contacts

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type mockSender struct {
	accepted bool
	deduped  bool
	calls    int
	err      error
}

type stubFeatureExtractor struct {
	features map[string]map[string]CandidateFeature
}

type stubPreferenceExtractor struct {
	byContact map[string]PreferenceFeatures
}

type stubNicknameGenerator struct {
	nicknameByContactID map[string]string
	confidence          float64
	err                 error
}

func (s *stubFeatureExtractor) EvaluateCandidateFeatures(ctx context.Context, contact Contact, candidates []ShareCandidate) (map[string]CandidateFeature, error) {
	return s.features[contact.ContactID], nil
}

func (s *stubPreferenceExtractor) EvaluateContactPreferences(ctx context.Context, contact Contact, candidates []ShareCandidate) (PreferenceFeatures, error) {
	return s.byContact[contact.ContactID], nil
}

func (s *stubNicknameGenerator) SuggestNickname(ctx context.Context, contact Contact) (string, float64, error) {
	if s.err != nil {
		return "", 0, s.err
	}
	return strings.TrimSpace(s.nicknameByContactID[contact.ContactID]), s.confidence, nil
}

func (m *mockSender) Send(ctx context.Context, contact Contact, decision ShareDecision) (bool, bool, error) {
	m.calls++
	if m.err != nil {
		return false, false, m.err
	}
	return m.accepted, m.deduped, nil
}

func TestServiceRunTickSelectsAndSends(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 7, 10, 0, 0, 0, time.UTC)

	contactA, err := svc.UpsertContact(ctx, Contact{
		ContactID:          "maep:a",
		Kind:               KindAgent,
		Status:             StatusActive,
		PeerID:             "12D3KooWA",
		TrustState:         "verified",
		UnderstandingDepth: 60,
		ReciprocityNorm:    0.8,
		TopicWeights: map[string]float64{
			"maep": 0.95,
		},
	}, now)
	if err != nil {
		t.Fatalf("UpsertContact(A) error = %v", err)
	}
	_, err = svc.UpsertContact(ctx, Contact{
		ContactID:          "maep:b",
		Kind:               KindAgent,
		Status:             StatusActive,
		PeerID:             "12D3KooWB",
		TrustState:         "verified",
		UnderstandingDepth: 30,
		ReciprocityNorm:    0.2,
		TopicWeights: map[string]float64{
			"maep": 0.10,
		},
	}, now)
	if err != nil {
		t.Fatalf("UpsertContact(B) error = %v", err)
	}
	_, err = svc.UpsertContact(ctx, Contact{
		ContactID:          "maep:tofu",
		Kind:               KindAgent,
		Status:             StatusActive,
		PeerID:             "12D3KooWTofu",
		TrustState:         "tofu",
		UnderstandingDepth: 50,
		ReciprocityNorm:    0.5,
	}, now)
	if err != nil {
		t.Fatalf("UpsertContact(TOFU) error = %v", err)
	}

	payload := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	if _, err := svc.AddCandidate(ctx, ShareCandidate{
		ItemID:        "cand-maep",
		Topic:         "maep",
		ContentType:   "text/plain",
		PayloadBase64: payload,
		DepthHint:     0.6,
	}, now); err != nil {
		t.Fatalf("AddCandidate(recent) error = %v", err)
	}
	if _, err := svc.AddCandidate(ctx, ShareCandidate{
		ItemID:        "cand-old",
		Topic:         "maep",
		ContentType:   "text/plain",
		PayloadBase64: payload,
		DepthHint:     0.6,
	}, now.Add(-96*time.Hour)); err != nil {
		t.Fatalf("AddCandidate(old) error = %v", err)
	}

	dryRun, err := svc.RunTick(ctx, now, TickOptions{
		MaxTargets:      1,
		FreshnessWindow: 72 * time.Hour,
		Send:            false,
	}, nil)
	if err != nil {
		t.Fatalf("RunTick(dry-run) error = %v", err)
	}
	if dryRun.Planned != 1 {
		t.Fatalf("RunTick(dry-run) planned mismatch: got %d want 1", dryRun.Planned)
	}
	if len(dryRun.Decisions) != 1 {
		t.Fatalf("RunTick(dry-run) decisions mismatch: got %d want 1", len(dryRun.Decisions))
	}
	if dryRun.Decisions[0].ContactID != contactA.ContactID {
		t.Fatalf("RunTick(dry-run) selected contact mismatch: got %s want %s", dryRun.Decisions[0].ContactID, contactA.ContactID)
	}

	sender := &mockSender{accepted: true, deduped: false}
	sendResult, err := svc.RunTick(ctx, now, TickOptions{
		MaxTargets:      1,
		FreshnessWindow: 72 * time.Hour,
		Send:            true,
	}, sender)
	if err != nil {
		t.Fatalf("RunTick(send) error = %v", err)
	}
	if sendResult.Sent != 1 {
		t.Fatalf("RunTick(send) sent mismatch: got %d want 1", sendResult.Sent)
	}
	if sender.calls != 1 {
		t.Fatalf("mock sender calls mismatch: got %d want 1", sender.calls)
	}
	updated, ok, err := store.GetContact(ctx, contactA.ContactID)
	if err != nil {
		t.Fatalf("GetContact(updated) error = %v", err)
	}
	if !ok {
		t.Fatalf("GetContact(updated) expected ok=true")
	}
	if updated.ShareCount <= contactA.ShareCount {
		t.Fatalf("share count not incremented: got %d want > %d", updated.ShareCount, contactA.ShareCount)
	}
}

func TestServiceRunTickUsesLLMFeaturesWhenProvided(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 7, 11, 0, 0, 0, time.UTC)

	_, err := svc.UpsertContact(ctx, Contact{
		ContactID:          "maep:a",
		Kind:               KindAgent,
		Status:             StatusActive,
		PeerID:             "12D3KooWA",
		TrustState:         "verified",
		UnderstandingDepth: 20,
		ReciprocityNorm:    0.1,
		TopicWeights: map[string]float64{
			"ops": 0.2,
		},
	}, now)
	if err != nil {
		t.Fatalf("UpsertContact(A) error = %v", err)
	}
	contactB, err := svc.UpsertContact(ctx, Contact{
		ContactID:          "maep:b",
		Kind:               KindAgent,
		Status:             StatusActive,
		PeerID:             "12D3KooWB",
		TrustState:         "verified",
		UnderstandingDepth: 20,
		ReciprocityNorm:    0.1,
		TopicWeights: map[string]float64{
			"ops": 0.2,
		},
	}, now)
	if err != nil {
		t.Fatalf("UpsertContact(B) error = %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	if _, err := svc.AddCandidate(ctx, ShareCandidate{
		ItemID:        "cand-ops",
		Topic:         "ops",
		ContentType:   "text/plain",
		PayloadBase64: payload,
		DepthHint:     0.4,
	}, now); err != nil {
		t.Fatalf("AddCandidate() error = %v", err)
	}

	extractor := &stubFeatureExtractor{
		features: map[string]map[string]CandidateFeature{
			"maep:a": {
				"cand-ops": {HasOverlapSemantic: true, OverlapSemantic: 0.1, Confidence: 0.9},
			},
			"maep:b": {
				"cand-ops": {HasOverlapSemantic: true, OverlapSemantic: 0.95, Confidence: 0.9},
			},
		},
	}

	result, err := svc.RunTick(ctx, now, TickOptions{
		MaxTargets:       1,
		FreshnessWindow:  72 * time.Hour,
		Send:             false,
		FeatureExtractor: extractor,
	}, nil)
	if err != nil {
		t.Fatalf("RunTick() error = %v", err)
	}
	if len(result.Decisions) != 1 {
		t.Fatalf("RunTick decisions mismatch: got %d want 1", len(result.Decisions))
	}
	if result.Decisions[0].ContactID != contactB.ContactID {
		t.Fatalf("RunTick selected contact mismatch: got %s want %s", result.Decisions[0].ContactID, contactB.ContactID)
	}
}

func TestServiceSetContactStatus(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC)

	_, err := svc.UpsertContact(ctx, Contact{
		ContactID:          "tg:@alice",
		Kind:               KindHuman,
		Status:             StatusActive,
		SubjectID:          "tg:@alice",
		UnderstandingDepth: 18,
		ReciprocityNorm:    0.4,
	}, now)
	if err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}

	updated, err := svc.SetContactStatus(ctx, "tg:@alice", StatusInactive)
	if err != nil {
		t.Fatalf("SetContactStatus() error = %v", err)
	}
	if updated.Status != StatusInactive {
		t.Fatalf("SetContactStatus status mismatch: got %s want %s", updated.Status, StatusInactive)
	}

	stored, ok, err := svc.GetContact(ctx, "tg:@alice")
	if err != nil {
		t.Fatalf("GetContact() error = %v", err)
	}
	if !ok {
		t.Fatalf("GetContact expected ok=true")
	}
	if stored.Status != StatusInactive {
		t.Fatalf("stored status mismatch: got %s want %s", stored.Status, StatusInactive)
	}
}

func TestServiceUpsertContactDoesNotAutoAssignNickname(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 7, 13, 0, 0, 0, time.UTC)

	created, err := svc.UpsertContact(ctx, Contact{
		ContactID:          "tg:@alice",
		Kind:               KindHuman,
		Status:             StatusActive,
		SubjectID:          "tg:@alice",
		UnderstandingDepth: defaultAutoNicknameDepthThreshold + 5,
		ReciprocityNorm:    0.4,
	}, now)
	if err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}
	if strings.TrimSpace(created.ContactNickname) != "" {
		t.Fatalf("contact nickname mismatch: got %q want empty", created.ContactNickname)
	}
}

func TestServiceRunTickAssignsNicknameViaGenerator(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 7, 13, 30, 0, 0, time.UTC)

	created, err := svc.UpsertContact(ctx, Contact{
		ContactID:          "tg:@alice",
		Kind:               KindHuman,
		Status:             StatusActive,
		SubjectID:          "tg:@alice",
		UnderstandingDepth: defaultAutoNicknameDepthThreshold + 5,
		ReciprocityNorm:    0.4,
	}, now)
	if err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	if _, err := svc.AddCandidate(ctx, ShareCandidate{
		ItemID:        "cand-hi",
		Topic:         "chat",
		ContentType:   "text/plain",
		PayloadBase64: payload,
	}, now); err != nil {
		t.Fatalf("AddCandidate() error = %v", err)
	}
	gen := &stubNicknameGenerator{
		nicknameByContactID: map[string]string{created.ContactID: "Alice"},
		confidence:          0.92,
	}
	_, err = svc.RunTick(ctx, now, TickOptions{
		Send:                false,
		EnableHumanContacts: true,
		NicknameGenerator:   gen,
	}, nil)
	if err != nil {
		t.Fatalf("RunTick() error = %v", err)
	}
	updated, ok, err := svc.GetContact(ctx, created.ContactID)
	if err != nil {
		t.Fatalf("GetContact() error = %v", err)
	}
	if !ok {
		t.Fatalf("GetContact expected ok=true")
	}
	if updated.ContactNickname != "Alice" {
		t.Fatalf("contact nickname mismatch: got %q want %q", updated.ContactNickname, "Alice")
	}
	audits, err := svc.ListAuditEvents(ctx, "", created.ContactID, auditActionNicknameAutoAssigned, 10)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	if len(audits) == 0 {
		t.Fatalf("auto nickname audit expected at least 1 record")
	}
}

func TestServiceRunTickAllowsTelegramHumanWithoutPeerID(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 7, 14, 0, 0, 0, time.UTC)

	_, err := svc.UpsertContact(ctx, Contact{
		ContactID:          "tg:@alice",
		Kind:               KindHuman,
		Status:             StatusActive,
		SubjectID:          "tg:@alice",
		UnderstandingDepth: 25,
		ReciprocityNorm:    0.4,
	}, now)
	if err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	if _, err := svc.AddCandidate(ctx, ShareCandidate{
		ItemID:        "cand-human",
		Topic:         "chat",
		ContentType:   "text/plain",
		PayloadBase64: payload,
	}, now); err != nil {
		t.Fatalf("AddCandidate() error = %v", err)
	}
	result, err := svc.RunTick(ctx, now, TickOptions{
		Send:                false,
		EnableHumanContacts: true,
	}, nil)
	if err != nil {
		t.Fatalf("RunTick() error = %v", err)
	}
	if len(result.Decisions) == 0 {
		t.Fatalf("RunTick decisions expected non-empty")
	}
	if result.Decisions[0].ContactID != "tg:@alice" {
		t.Fatalf("RunTick first decision contact mismatch: got %s want %s", result.Decisions[0].ContactID, "tg:@alice")
	}
}

func TestServiceRunTickNicknameGeneratorFailureIsAudited(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 7, 14, 30, 0, 0, time.UTC)

	created, err := svc.UpsertContact(ctx, Contact{
		ContactID:          "tg:@bob",
		Kind:               KindHuman,
		Status:             StatusActive,
		SubjectID:          "tg:@bob",
		UnderstandingDepth: defaultAutoNicknameDepthThreshold + 2,
		ReciprocityNorm:    0.4,
	}, now)
	if err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	if _, err := svc.AddCandidate(ctx, ShareCandidate{
		ItemID:        "cand-bob",
		Topic:         "chat",
		ContentType:   "text/plain",
		PayloadBase64: payload,
	}, now); err != nil {
		t.Fatalf("AddCandidate() error = %v", err)
	}
	_, err = svc.RunTick(ctx, now, TickOptions{
		Send:                false,
		EnableHumanContacts: true,
		NicknameGenerator:   &stubNicknameGenerator{err: fmt.Errorf("llm unavailable")},
	}, nil)
	if err != nil {
		t.Fatalf("RunTick() error = %v", err)
	}
	audits, err := svc.ListAuditEvents(ctx, "", created.ContactID, auditActionNicknameAutoFailed, 10)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	if len(audits) == 0 {
		t.Fatalf("auto nickname failed audit expected at least 1 record")
	}
}

func TestServiceRunTickDecisionCarriesSourceChatHints(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 7, 15, 0, 0, 0, time.UTC)

	_, err := svc.UpsertContact(ctx, Contact{
		ContactID:          "tg:id:1001",
		Kind:               KindHuman,
		Status:             StatusActive,
		SubjectID:          "tg:id:1001",
		UnderstandingDepth: 30,
		ReciprocityNorm:    0.4,
		TelegramChats: []TelegramChatRef{
			{ChatID: 1001, ChatType: "private"},
			{ChatID: -10055, ChatType: "group"},
		},
	}, now)
	if err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	if _, err := svc.AddCandidate(ctx, ShareCandidate{
		ItemID:         "cand-group",
		Topic:          "chat",
		ContentType:    "text/plain",
		PayloadBase64:  payload,
		SourceChatID:   -10055,
		SourceChatType: "group",
	}, now); err != nil {
		t.Fatalf("AddCandidate() error = %v", err)
	}
	result, err := svc.RunTick(ctx, now, TickOptions{
		Send:                false,
		EnableHumanContacts: true,
	}, nil)
	if err != nil {
		t.Fatalf("RunTick() error = %v", err)
	}
	if len(result.Decisions) == 0 {
		t.Fatalf("RunTick decisions expected non-empty")
	}
	if result.Decisions[0].SourceChatID != -10055 || result.Decisions[0].SourceChatType != "group" {
		t.Fatalf("decision source chat hint mismatch: got id=%d type=%q", result.Decisions[0].SourceChatID, result.Decisions[0].SourceChatType)
	}
}

func TestServiceRunTickHumanDisabledSkipsHumanContacts(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 7, 15, 30, 0, 0, time.UTC)

	_, err := svc.UpsertContact(ctx, Contact{
		ContactID:          "tg:@alice",
		Kind:               KindHuman,
		Status:             StatusActive,
		SubjectID:          "tg:@alice",
		UnderstandingDepth: 20,
		ReciprocityNorm:    0.4,
	}, now)
	if err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	if _, err := svc.AddCandidate(ctx, ShareCandidate{
		ItemID:        "cand-human",
		Topic:         "chat",
		ContentType:   "text/plain",
		PayloadBase64: payload,
	}, now); err != nil {
		t.Fatalf("AddCandidate() error = %v", err)
	}
	result, err := svc.RunTick(ctx, now, TickOptions{
		Send:                false,
		EnableHumanContacts: false,
	}, nil)
	if err != nil {
		t.Fatalf("RunTick() error = %v", err)
	}
	if len(result.Decisions) != 0 {
		t.Fatalf("RunTick decisions mismatch: got %d want 0", len(result.Decisions))
	}
}

func TestServiceRunTickHumanSendDisabledSkipsHumanWhenSend(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 7, 16, 0, 0, 0, time.UTC)

	_, err := svc.UpsertContact(ctx, Contact{
		ContactID:          "tg:@alice",
		Kind:               KindHuman,
		Status:             StatusActive,
		SubjectID:          "tg:@alice",
		UnderstandingDepth: 20,
		ReciprocityNorm:    0.4,
	}, now)
	if err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	if _, err := svc.AddCandidate(ctx, ShareCandidate{
		ItemID:        "cand-human",
		Topic:         "chat",
		ContentType:   "text/plain",
		PayloadBase64: payload,
	}, now); err != nil {
		t.Fatalf("AddCandidate() error = %v", err)
	}
	result, err := svc.RunTick(ctx, now, TickOptions{
		Send:                true,
		EnableHumanContacts: true,
		EnableHumanSend:     false,
	}, &mockSender{accepted: true})
	if err != nil {
		t.Fatalf("RunTick() error = %v", err)
	}
	if len(result.Decisions) != 0 {
		t.Fatalf("RunTick decisions mismatch: got %d want 0", len(result.Decisions))
	}
}

func TestServiceRunTickHumanPublicDisabledSkipsPublicCandidate(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 7, 16, 30, 0, 0, time.UTC)

	_, err := svc.UpsertContact(ctx, Contact{
		ContactID:          "tg:id:1001",
		Kind:               KindHuman,
		Status:             StatusActive,
		SubjectID:          "tg:id:1001",
		UnderstandingDepth: 20,
		ReciprocityNorm:    0.4,
		TelegramChats: []TelegramChatRef{
			{ChatID: -100999, ChatType: "group"},
		},
	}, now)
	if err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	if _, err := svc.AddCandidate(ctx, ShareCandidate{
		ItemID:         "cand-group",
		Topic:          "chat",
		ContentType:    "text/plain",
		PayloadBase64:  payload,
		SourceChatID:   -100999,
		SourceChatType: "group",
	}, now); err != nil {
		t.Fatalf("AddCandidate() error = %v", err)
	}
	result, err := svc.RunTick(ctx, now, TickOptions{
		Send:                  true,
		EnableHumanContacts:   true,
		EnableHumanSend:       true,
		EnableHumanPublicSend: false,
	}, &mockSender{accepted: true})
	if err != nil {
		t.Fatalf("RunTick() error = %v", err)
	}
	if len(result.Decisions) != 0 {
		t.Fatalf("RunTick decisions mismatch: got %d want 0", len(result.Decisions))
	}
}

func TestServiceUpdateFeedbackUpdatesTopicPreference(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 7, 17, 0, 0, 0, time.UTC)

	_, err := svc.UpsertContact(ctx, Contact{
		ContactID:          "tg:@alice",
		Kind:               KindHuman,
		Status:             StatusActive,
		SubjectID:          "tg:@alice",
		UnderstandingDepth: 20,
		ReciprocityNorm:    0.4,
		TopicWeights: map[string]float64{
			"chat": 0.20,
		},
	}, now)
	if err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}

	contact, session, err := svc.UpdateFeedback(ctx, now, FeedbackUpdateInput{
		ContactID: "tg:@alice",
		Signal:    FeedbackPositive,
		Topic:     "chat",
		Reason:    "liked_short_reply",
	})
	if err != nil {
		t.Fatalf("UpdateFeedback() error = %v", err)
	}
	if session.SessionInterestLevel <= 0.60 {
		t.Fatalf("session interest not updated: got %.3f", session.SessionInterestLevel)
	}
	got := contact.TopicWeights["chat"]
	if got <= 0.20 {
		t.Fatalf("topic preference not increased: got %.3f", got)
	}
	const expected = 0.28 // 0.20 + topicDelta(positive=0.08)
	if diff := got - expected; diff < -1e-9 || diff > 1e-9 {
		t.Fatalf("topic preference formula mismatch: got %.3f want %.3f", got, expected)
	}

	events, err := svc.ListAuditEvents(ctx, "", "tg:@alice", "contact_feedback_update", 10)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("expected contact_feedback_update audit event")
	}
	if events[0].Metadata["topic"] != "chat" {
		t.Fatalf("audit topic mismatch: got %q", events[0].Metadata["topic"])
	}
}

func TestServiceRunTickAppliesPreferenceExtraction(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewService(store)
	now := time.Date(2026, 2, 7, 18, 0, 0, 0, time.UTC)

	_, err := svc.UpsertContact(ctx, Contact{
		ContactID:          "maep:a",
		Kind:               KindAgent,
		Status:             StatusActive,
		PeerID:             "12D3KooWA",
		TrustState:         "verified",
		UnderstandingDepth: 30,
		ReciprocityNorm:    0.5,
		TopicWeights: map[string]float64{
			"maep": 0.20,
		},
	}, now)
	if err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	if _, err := svc.AddCandidate(ctx, ShareCandidate{
		ItemID:        "cand-1",
		Topic:         "maep",
		ContentType:   "text/plain",
		PayloadBase64: payload,
	}, now); err != nil {
		t.Fatalf("AddCandidate() error = %v", err)
	}

	result, err := svc.RunTick(ctx, now, TickOptions{
		MaxTargets:      1,
		FreshnessWindow: 72 * time.Hour,
		Send:            false,
		PreferenceExtractor: &stubPreferenceExtractor{byContact: map[string]PreferenceFeatures{
			"maep:a": {
				TopicAffinity: map[string]float64{
					"maep": 1.0,
				},
				PersonaBrief: "理性、谨慎，偏技术话题",
				PersonaTraits: map[string]float64{
					"analytical": 0.90,
				},
				Confidence: 1.0,
			},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("RunTick() error = %v", err)
	}
	if result.Planned != 1 {
		t.Fatalf("RunTick planned mismatch: got %d want 1", result.Planned)
	}

	contact, ok, err := svc.GetContact(ctx, "maep:a")
	if err != nil {
		t.Fatalf("GetContact() error = %v", err)
	}
	if !ok {
		t.Fatalf("GetContact() not found")
	}
	if got := contact.TopicWeights["maep"]; got < 0.359 || got > 0.361 {
		t.Fatalf("topic preference blend mismatch: got %.3f want ~0.360", got)
	}
	if got := contact.PersonaBrief; got != "理性、谨慎，偏技术话题" {
		t.Fatalf("persona brief mismatch: got %q", got)
	}
	if got := contact.PersonaTraits["analytical"]; got < 0.179 || got > 0.181 {
		t.Fatalf("persona trait mismatch: got %.3f want ~0.180", got)
	}

	events, err := svc.ListAuditEvents(ctx, "", "maep:a", "contact_preference_extract_applied", 10)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("expected contact_preference_extract_applied audit event")
	}
}

func TestServiceSendDecisionFailureUsesConfiguredCooldown(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewServiceWithOptions(store, ServiceOptions{
		FailureCooldown: 3 * time.Minute,
	})
	now := time.Date(2026, 2, 7, 19, 0, 0, 0, time.UTC)

	_, err := svc.UpsertContact(ctx, Contact{
		ContactID:          "maep:a",
		Kind:               KindAgent,
		Status:             StatusActive,
		PeerID:             "12D3KooWA",
		TrustState:         "verified",
		UnderstandingDepth: 30,
		ReciprocityNorm:    0.5,
	}, now)
	if err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}

	payload := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	outcome, err := svc.SendDecision(ctx, now, ShareDecision{
		ContactID:      "maep:a",
		Topic:          "share.proactive.v1",
		ContentType:    "text/plain",
		PayloadBase64:  payload,
		ItemID:         "manual_1",
		IdempotencyKey: "manual:key1",
	}, &mockSender{err: fmt.Errorf("dial failed")})
	if err != nil {
		t.Fatalf("SendDecision() error = %v", err)
	}
	if strings.TrimSpace(outcome.Error) == "" {
		t.Fatalf("SendDecision() expected outcome error")
	}

	updated, ok, err := svc.GetContact(ctx, "maep:a")
	if err != nil {
		t.Fatalf("GetContact() error = %v", err)
	}
	if !ok {
		t.Fatalf("GetContact() expected ok=true")
	}
	if updated.CooldownUntil == nil {
		t.Fatalf("cooldown expected non-nil")
	}
	want := now.Add(3 * time.Minute)
	if !updated.CooldownUntil.Equal(want) {
		t.Fatalf("cooldown mismatch: got %s want %s", updated.CooldownUntil.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestServiceRunTickSendFailureUsesConfiguredCooldown(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	svc := NewServiceWithOptions(store, ServiceOptions{
		FailureCooldown: 3 * time.Minute,
	})
	now := time.Date(2026, 2, 7, 19, 30, 0, 0, time.UTC)

	_, err := svc.UpsertContact(ctx, Contact{
		ContactID:          "maep:a",
		Kind:               KindAgent,
		Status:             StatusActive,
		PeerID:             "12D3KooWA",
		TrustState:         "verified",
		UnderstandingDepth: 50,
		ReciprocityNorm:    0.7,
		TopicWeights: map[string]float64{
			"maep": 0.9,
		},
	}, now)
	if err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	if _, err := svc.AddCandidate(ctx, ShareCandidate{
		ItemID:        "cand-1",
		Topic:         "maep",
		ContentType:   "text/plain",
		PayloadBase64: payload,
	}, now); err != nil {
		t.Fatalf("AddCandidate() error = %v", err)
	}

	_, err = svc.RunTick(ctx, now, TickOptions{
		MaxTargets:      1,
		FreshnessWindow: 72 * time.Hour,
		Send:            true,
	}, &mockSender{err: fmt.Errorf("dial failed")})
	if err != nil {
		t.Fatalf("RunTick() error = %v", err)
	}

	updated, ok, err := svc.GetContact(ctx, "maep:a")
	if err != nil {
		t.Fatalf("GetContact() error = %v", err)
	}
	if !ok {
		t.Fatalf("GetContact() expected ok=true")
	}
	if updated.CooldownUntil == nil {
		t.Fatalf("cooldown expected non-nil")
	}
	want := now.Add(3 * time.Minute)
	if !updated.CooldownUntil.Equal(want) {
		t.Fatalf("cooldown mismatch: got %s want %s", updated.CooldownUntil.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}
