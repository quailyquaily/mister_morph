package contacts

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStoreContactsCandidatesAuditAndSessions(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	now := time.Date(2026, 2, 7, 10, 0, 0, 0, time.UTC)
	active := Contact{
		ContactID:          "maep:peer-active",
		Kind:               KindAgent,
		Status:             StatusActive,
		PeerID:             "12D3KooWactive",
		TrustState:         "verified",
		UnderstandingDepth: 40,
		ReciprocityNorm:    0.6,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	inactive := Contact{
		ContactID:          "ext:telegram:1001",
		Kind:               KindHuman,
		Status:             StatusInactive,
		SubjectID:          "ext:telegram:1001",
		UnderstandingDepth: 20,
		ReciprocityNorm:    0.3,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := store.PutContact(ctx, active); err != nil {
		t.Fatalf("PutContact(active) error = %v", err)
	}
	if err := store.PutContact(ctx, inactive); err != nil {
		t.Fatalf("PutContact(inactive) error = %v", err)
	}

	activeList, err := store.ListContacts(ctx, StatusActive)
	if err != nil {
		t.Fatalf("ListContacts(active) error = %v", err)
	}
	if len(activeList) != 1 || activeList[0].ContactID != active.ContactID {
		t.Fatalf("active contacts mismatch: got=%v", activeList)
	}

	inactiveList, err := store.ListContacts(ctx, StatusInactive)
	if err != nil {
		t.Fatalf("ListContacts(inactive) error = %v", err)
	}
	if len(inactiveList) != 1 || inactiveList[0].ContactID != inactive.ContactID {
		t.Fatalf("inactive contacts mismatch: got=%v", inactiveList)
	}

	candidate := ShareCandidate{
		ItemID:        "cand-1",
		Topic:         "distributed-systems",
		ContentType:   "text/plain",
		PayloadBase64: "aGVsbG8",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := store.PutCandidate(ctx, candidate); err != nil {
		t.Fatalf("PutCandidate() error = %v", err)
	}
	candidates, err := store.ListCandidates(ctx)
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].ItemID != candidate.ItemID {
		t.Fatalf("candidates mismatch: got=%v", candidates)
	}

	session := SessionState{
		SessionID:            "sess-1",
		ContactID:            active.ContactID,
		SessionInterestLevel: 0.6,
		TurnCount:            1,
		StartedAt:            now,
		UpdatedAt:            now,
	}
	if err := store.PutSessionState(ctx, session); err != nil {
		t.Fatalf("PutSessionState() error = %v", err)
	}
	states, err := store.ListSessionStates(ctx)
	if err != nil {
		t.Fatalf("ListSessionStates() error = %v", err)
	}
	if len(states) != 1 || states[0].SessionID != session.SessionID {
		t.Fatalf("session states mismatch: got=%v", states)
	}

	event := AuditEvent{
		EventID:   "evt-1",
		TickID:    "tick-1",
		Action:    "proactive_share_tick_start",
		ContactID: active.ContactID,
		CreatedAt: now,
	}
	if err := store.AppendAuditEvent(ctx, event); err != nil {
		t.Fatalf("AppendAuditEvent() error = %v", err)
	}
	events, err := store.ListAuditEvents(ctx, "tick-1", active.ContactID, "", 10)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].EventID != event.EventID {
		t.Fatalf("audit events mismatch: got=%v", events)
	}
}

func TestFileStoreLegacyDisplayNameMigratesToContactNickname(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	now := time.Date(2026, 2, 7, 11, 0, 0, 0, time.UTC)
	legacy := Contact{
		ContactID:   "tg:@alice",
		Kind:        KindHuman,
		Status:      StatusActive,
		SubjectID:   "tg:@alice",
		DisplayName: "Alice Legacy",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.PutContact(ctx, legacy); err != nil {
		t.Fatalf("PutContact(legacy) error = %v", err)
	}

	got, ok, err := store.GetContact(ctx, "tg:@alice")
	if err != nil {
		t.Fatalf("GetContact() error = %v", err)
	}
	if !ok {
		t.Fatalf("GetContact() expected ok=true")
	}
	if got.ContactNickname != "Alice Legacy" {
		t.Fatalf("contact nickname mismatch: got %q want %q", got.ContactNickname, "Alice Legacy")
	}
	if got.DisplayName != "" {
		t.Fatalf("display_name should be normalized away, got %q", got.DisplayName)
	}
}
