package mixin

import (
	"context"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/mixinapi"
)

func TestMixinPairTargetResolvesExistingAgentByIdentityNumber(t *testing.T) {
	store := contacts.NewFileStore(t.TempDir())
	if err := store.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	service := contacts.NewService(store)
	if _, err := service.UpsertContact(context.Background(), contacts.Contact{
		ContactID: "mixin:11111111-1111-1111-1111-111111111111", Kind: contacts.KindAgent,
		Channel: contacts.ChannelMixin, MixinUserID: "11111111-1111-1111-1111-111111111111",
		MixinIdentityNumber: "7000123456", ContactNickname: "Agent B",
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	peer, err := mixinPairTarget(context.Background(), service, "@7000123456")
	if err != nil {
		t.Fatalf("mixinPairTarget() error = %v", err)
	}
	if peer.ID != "mixin:@7000123456" || peer.Contact.MixinUserID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("peer = %#v", peer)
	}
}

func TestMixinInboundAgentPeerAndAuthorization(t *testing.T) {
	peer := mixinInboundAgentPeer(mixinapi.User{
		UserID: "11111111-1111-1111-1111-111111111111", IdentityNumber: "7000123456",
		FullName: "Agent B", AppID: "22222222-2222-2222-2222-222222222222",
	}, "33333333-3333-3333-3333-333333333333")
	if peer.ID != "mixin:11111111-1111-1111-1111-111111111111" || peer.Contact.Kind != contacts.KindAgent || len(peer.Contact.MixinChatIDs) != 1 {
		t.Fatalf("peer = %#v", peer)
	}
	allowed := map[string]bool{"44444444-4444-4444-4444-444444444444": true}
	if !mixinConversationAuthorized(allowed, "44444444-4444-4444-4444-444444444444", true, false) {
		t.Fatal("allowlisted group was rejected")
	}
	if !mixinConversationAuthorized(allowed, "33333333-3333-3333-3333-333333333333", false, true) {
		t.Fatal("paired private Agent was rejected after transient profile failure")
	}
	if mixinConversationAuthorized(allowed, "33333333-3333-3333-3333-333333333333", false, false) {
		t.Fatal("unpaired private Agent bypassed allowlist")
	}
}
