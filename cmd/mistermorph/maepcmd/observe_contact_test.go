package maepcmd

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/maep"
)

func TestObserveMAEPContact_WithoutMAEPContactFallback(t *testing.T) {
	ctx := context.Background()
	maepSvc := maep.NewService(maep.NewFileStore(filepath.Join(t.TempDir(), "maep")))
	contactsSvc := contacts.NewService(contacts.NewFileStore(filepath.Join(t.TempDir(), "contacts")))
	now := time.Date(2026, 2, 7, 16, 0, 0, 0, time.UTC)

	event := maep.DataPushEvent{
		FromPeerID:     "12D3KooWExamplePeer",
		Topic:          "share.proactive.v1",
		IdempotencyKey: "k1",
		ReceivedAt:     now,
	}
	if err := observeMAEPContact(ctx, maepSvc, contactsSvc, event, now); err != nil {
		t.Fatalf("observeMAEPContact() error = %v", err)
	}

	item, ok, err := contactsSvc.GetContact(ctx, "maep:12D3KooWExamplePeer")
	if err != nil {
		t.Fatalf("GetContact() error = %v", err)
	}
	if !ok {
		t.Fatalf("GetContact expected ok=true")
	}
	if item.Kind != contacts.KindAgent {
		t.Fatalf("kind mismatch: got %s want %s", item.Kind, contacts.KindAgent)
	}
	if item.PeerID != "12D3KooWExamplePeer" {
		t.Fatalf("peer_id mismatch: got %s", item.PeerID)
	}
	if item.LastInteractionAt == nil {
		t.Fatalf("last_interaction_at expected non-nil")
	}
}
