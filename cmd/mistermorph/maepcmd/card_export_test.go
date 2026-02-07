package maepcmd

import (
	"bytes"
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/maep"
)

func TestResolveCardExportAddresses_UsesConfiguredListenAddrs(t *testing.T) {
	ctx := context.Background()
	svc := maep.NewService(maep.NewFileStore(filepath.Join(t.TempDir(), "maep")))
	identity, _, err := svc.EnsureIdentity(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("EnsureIdentity() error = %v", err)
	}

	got, err := resolveCardExportAddresses(ctx, svc, nil, []string{"/ip4/127.0.0.1/tcp/4101"})
	if err != nil {
		t.Fatalf("resolveCardExportAddresses() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("resolveCardExportAddresses() len=%d want=1", len(got))
	}
	wantSuffix := "/p2p/" + identity.PeerID
	if !strings.HasSuffix(got[0], wantSuffix) {
		t.Fatalf("resolved address missing peer suffix: got %q want suffix %q", got[0], wantSuffix)
	}
}

func TestResolveCardExportAddresses_ConfigPeerIDMismatch(t *testing.T) {
	ctx := context.Background()
	svc := maep.NewService(maep.NewFileStore(filepath.Join(t.TempDir(), "maep")))
	if _, _, err := svc.EnsureIdentity(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("EnsureIdentity() error = %v", err)
	}
	otherSvc := maep.NewService(maep.NewFileStore(filepath.Join(t.TempDir(), "maep-other")))
	otherIdentity, _, err := otherSvc.EnsureIdentity(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("EnsureIdentity(other) error = %v", err)
	}

	_, err = resolveCardExportAddresses(ctx, svc, nil, []string{"/ip4/127.0.0.1/tcp/4101/p2p/" + otherIdentity.PeerID})
	if err == nil {
		t.Fatalf("resolveCardExportAddresses() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "mismatched /p2p/") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveCardExportAddresses_ExplicitAddressesTakePriority(t *testing.T) {
	ctx := context.Background()
	svc := maep.NewService(maep.NewFileStore(filepath.Join(t.TempDir(), "maep")))
	if _, _, err := svc.EnsureIdentity(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("EnsureIdentity() error = %v", err)
	}

	explicit := []string{"/ip4/127.0.0.1/tcp/4102/p2p/12D3KooWExplicitPeer"}
	got, err := resolveCardExportAddresses(ctx, svc, explicit, []string{"/ip4/127.0.0.1/tcp/4101"})
	if err != nil {
		t.Fatalf("resolveCardExportAddresses() error = %v", err)
	}
	if len(got) != 1 || got[0] != explicit[0] {
		t.Fatalf("explicit addresses not preserved: got=%v want=%v", got, explicit)
	}
}

func TestResolveCardExportAddresses_InteractiveSelection(t *testing.T) {
	ctx := context.Background()
	svc := maep.NewService(maep.NewFileStore(filepath.Join(t.TempDir(), "maep")))
	identity, _, err := svc.EnsureIdentity(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("EnsureIdentity() error = %v", err)
	}

	in := strings.NewReader("2\n")
	var out bytes.Buffer
	got, err := resolveCardExportAddressesWithPrompt(
		ctx,
		svc,
		nil,
		[]string{"/ip4/127.0.0.1/tcp/4101", "/ip4/127.0.0.1/tcp/4102"},
		in,
		&out,
		true,
	)
	if err != nil {
		t.Fatalf("resolveCardExportAddressesWithPrompt() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("resolveCardExportAddressesWithPrompt() len=%d want=1", len(got))
	}
	want := "/ip4/127.0.0.1/tcp/4102/p2p/" + identity.PeerID
	if got[0] != want {
		t.Fatalf("unexpected selected address: got=%q want=%q", got[0], want)
	}
	if !strings.Contains(out.String(), "Select one dialable address") {
		t.Fatalf("expected prompt output, got=%q", out.String())
	}
}

func TestResolveCardExportAddresses_NonInteractiveMultipleValidRequiresExplicitAddress(t *testing.T) {
	ctx := context.Background()
	svc := maep.NewService(maep.NewFileStore(filepath.Join(t.TempDir(), "maep")))
	if _, _, err := svc.EnsureIdentity(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("EnsureIdentity() error = %v", err)
	}

	_, err := resolveCardExportAddressesWithPrompt(
		ctx,
		svc,
		nil,
		[]string{"/ip4/127.0.0.1/tcp/4101", "/ip4/127.0.0.1/tcp/4102"},
		nil,
		nil,
		false,
	)
	if err == nil {
		t.Fatalf("resolveCardExportAddressesWithPrompt() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "multiple dialable addresses found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExpandConfiguredDialAddressesWithIPs_ReplacesUnspecifiedIPv4(t *testing.T) {
	input := []string{
		"/ip4/0.0.0.0/tcp/4021",
	}
	out := expandConfiguredDialAddressesWithIPs(input, []net.IP{
		net.ParseIP("192.168.1.10"),
		net.ParseIP("127.0.0.1"),
	})
	if len(out) < 2 {
		t.Fatalf("expandConfiguredDialAddressesWithIPs() len=%d want>=2", len(out))
	}
	foundLAN := false
	foundLoopback := false
	for _, addr := range out {
		if strings.Contains(addr, "/ip4/192.168.1.10/") {
			foundLAN = true
		}
		if strings.Contains(addr, "/ip4/127.0.0.1/") {
			foundLoopback = true
		}
	}
	if !foundLAN {
		t.Fatalf("expected expanded LAN address, got=%v", out)
	}
	if !foundLoopback {
		t.Fatalf("expected expanded loopback address, got=%v", out)
	}
}
