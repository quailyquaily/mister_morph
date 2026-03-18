package consolecmd

import (
	"testing"

	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/spf13/viper"
)

func TestManagedRuntimeTaskStoreRespectsPersistenceTargets(t *testing.T) {
	prevTargets := append([]string(nil), viper.GetStringSlice("tasks.persistence_targets")...)
	prevStateDir := viper.GetString("file_state_dir")
	t.Cleanup(func() {
		viper.Set("tasks.persistence_targets", prevTargets)
		viper.Set("file_state_dir", prevStateDir)
	})
	viper.Set("file_state_dir", t.TempDir())

	viper.Set("tasks.persistence_targets", []string{"console"})
	telegramStore, err := newManagedRuntimeTaskStore(managedRuntimeTelegram, 10)
	if err != nil {
		t.Fatalf("newManagedRuntimeTaskStore(telegram) error = %v", err)
	}
	if _, ok := telegramStore.(*daemonruntime.MemoryStore); !ok {
		t.Fatalf("telegram store type = %T, want *daemonruntime.MemoryStore when target is not persisted", telegramStore)
	}

	viper.Set("tasks.persistence_targets", []string{"console", "telegram"})
	telegramPersisted, err := newManagedRuntimeTaskStore(managedRuntimeTelegram, 10)
	if err != nil {
		t.Fatalf("newManagedRuntimeTaskStore(telegram persisted) error = %v", err)
	}
	if _, ok := telegramPersisted.(*daemonruntime.FileTaskStore); !ok {
		t.Fatalf("telegram persisted store type = %T, want *daemonruntime.FileTaskStore", telegramPersisted)
	}

	viper.Set("tasks.persistence_targets", []string{"console", "slack"})
	slackPersisted, err := newManagedRuntimeTaskStore(managedRuntimeSlack, 10)
	if err != nil {
		t.Fatalf("newManagedRuntimeTaskStore(slack persisted) error = %v", err)
	}
	if _, ok := slackPersisted.(*daemonruntime.FileTaskStore); !ok {
		t.Fatalf("slack persisted store type = %T, want *daemonruntime.FileTaskStore", slackPersisted)
	}
}
