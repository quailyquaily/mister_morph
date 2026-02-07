package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestEnsureInstallMAEPIdentity_ReusesExistingIdentity(t *testing.T) {
	initViperDefaults()
	originalDirName := viper.GetString("maep.dir_name")
	viper.Set("maep.dir_name", "maep")
	t.Cleanup(func() {
		if originalDirName == "" {
			viper.Set("maep.dir_name", nil)
			return
		}
		viper.Set("maep.dir_name", originalDirName)
	})

	root := t.TempDir()

	first, created, err := ensureInstallMAEPIdentity(root)
	if err != nil {
		t.Fatalf("ensureInstallMAEPIdentity() first call error = %v", err)
	}
	if !created {
		t.Fatalf("ensureInstallMAEPIdentity() first call expected created=true")
	}

	second, created, err := ensureInstallMAEPIdentity(root)
	if err != nil {
		t.Fatalf("ensureInstallMAEPIdentity() second call error = %v", err)
	}
	if created {
		t.Fatalf("ensureInstallMAEPIdentity() second call expected created=false")
	}
	if second.PeerID != first.PeerID {
		t.Fatalf("peer_id changed after second init: got %s want %s", second.PeerID, first.PeerID)
	}
	if second.NodeUUID != first.NodeUUID {
		t.Fatalf("node_uuid changed after second init: got %s want %s", second.NodeUUID, first.NodeUUID)
	}
	if second.NodeID != first.NodeID {
		t.Fatalf("node_id changed after second init: got %s want %s", second.NodeID, first.NodeID)
	}

	identityPath := filepath.Join(root, "maep", "identity.json")
	if _, err := os.Stat(identityPath); err != nil {
		t.Fatalf("identity file missing at %s: %v", identityPath, err)
	}
}

func TestEnsureInstallMAEPIdentity_RespectsMaepDirName(t *testing.T) {
	initViperDefaults()
	originalDirName := viper.GetString("maep.dir_name")
	viper.Set("maep.dir_name", "custom-maep")
	t.Cleanup(func() {
		if originalDirName == "" {
			viper.Set("maep.dir_name", nil)
			return
		}
		viper.Set("maep.dir_name", originalDirName)
	})

	root := t.TempDir()
	if _, _, err := ensureInstallMAEPIdentity(root); err != nil {
		t.Fatalf("ensureInstallMAEPIdentity() error = %v", err)
	}

	identityPath := filepath.Join(root, "custom-maep", "identity.json")
	if _, err := os.Stat(identityPath); err != nil {
		t.Fatalf("identity file missing at %s: %v", identityPath, err)
	}
}
