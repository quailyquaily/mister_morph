package consolecmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRuntimeConfigPollerWaitsForActiveReloadAfterCancel(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	reloadStarted := make(chan struct{})
	releaseReload := make(chan struct{})
	srv := &server{
		cfg: serveConfig{configPath: configPath},
		reloadRuntimeConfigFunc: func() error {
			close(reloadStarted)
			<-releaseReload
			return nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	srv.startRuntimeConfigPoller(ctx)
	if err := os.WriteFile(configPath, []byte("llm:\n  model: changed\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}

	select {
	case <-reloadStarted:
	case <-time.After(2 * consoleConfigPollInterval):
		t.Fatal("runtime config reload did not start")
	}
	cancel()
	pollerDone := make(chan struct{})
	go func() {
		srv.runtimeConfigPollerWG.Wait()
		close(pollerDone)
	}()
	select {
	case <-pollerDone:
		t.Fatal("config poller exited before the active reload returned")
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseReload)
	select {
	case <-pollerDone:
	case <-time.After(time.Second):
		t.Fatal("config poller did not exit after the active reload returned")
	}
}
