package contactscmd

import "testing"

func TestResolveSyncMAEPTargetContactID_PrefersExistingNodeID(t *testing.T) {
	existingByNodeID := map[string]string{
		"maep:node-1": "custom-contact-id",
	}
	got := resolveSyncMAEPTargetContactID(existingByNodeID, "maep:node-1", "12D3KooWPeer")
	if got != "custom-contact-id" {
		t.Fatalf("resolveSyncMAEPTargetContactID() = %q, want %q", got, "custom-contact-id")
	}
}

func TestResolveSyncMAEPTargetContactID_FallsBackToCanonical(t *testing.T) {
	got := resolveSyncMAEPTargetContactID(nil, "maep:node-2", "12D3KooWPeer")
	if got != "maep:node-2" {
		t.Fatalf("resolveSyncMAEPTargetContactID() = %q, want %q", got, "maep:node-2")
	}

	got = resolveSyncMAEPTargetContactID(nil, "", "12D3KooWPeer")
	if got != "maep:12D3KooWPeer" {
		t.Fatalf("resolveSyncMAEPTargetContactID() = %q, want %q", got, "maep:12D3KooWPeer")
	}
}
