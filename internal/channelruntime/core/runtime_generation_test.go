package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	"github.com/spf13/viper"
)

type generationTestSource struct {
	mu         sync.Mutex
	current    agentsettings.Reader
	candidate  agentsettings.Reader
	loadErr    error
	configPath string
}

func (s *generationTestSource) CurrentReader() agentsettings.Reader {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

func (s *generationTestSource) ReplaceReader(reader agentsettings.Reader) {
	s.mu.Lock()
	s.current = reader
	s.mu.Unlock()
}

func (s *generationTestSource) ConfigPath() string { return s.configPath }

func (s *generationTestSource) LoadCandidate() (agentsettings.Reader, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.candidate, s.loadErr
}

func generationTestReader(model string) agentsettings.Reader {
	reader := viper.New()
	reader.Set("llm.model", model)
	return agentsettings.NewReaderSnapshot(reader)
}

func TestRuntimeGenerationManagerSwitchesNewTasksAndRetiresOldAfterRelease(t *testing.T) {
	source := &generationTestSource{
		current:   generationTestReader("old-model"),
		candidate: generationTestReader("new-model"),
	}
	var mu sync.Mutex
	cleaned := map[string]int{}
	manager, err := NewRuntimeGenerationManager(context.Background(), RuntimeGenerationManagerOptions{
		Source: source,
		Build: func(_ context.Context, reader agentsettings.Reader) (ChannelRuntimeBundle, error) {
			model := reader.GetString("llm.model")
			return ChannelRuntimeBundle{Cleanup: func() {
				mu.Lock()
				cleaned[model]++
				mu.Unlock()
			}}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewRuntimeGenerationManager() error = %v", err)
	}
	defer manager.Close()

	oldLease, err := manager.Capture()
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	if got := oldLease.Reader().GetString("llm.model"); got != "old-model" {
		t.Fatalf("old lease model = %q", got)
	}
	if err := manager.Reload(context.Background()); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	newLease, err := manager.Capture()
	if err != nil {
		t.Fatalf("Capture() after reload error = %v", err)
	}
	if got := newLease.Reader().GetString("llm.model"); got != "new-model" {
		t.Fatalf("new lease model = %q", got)
	}
	if got := source.CurrentReader().GetString("llm.model"); got != "new-model" {
		t.Fatalf("source current model = %q", got)
	}

	mu.Lock()
	oldCleanedBeforeRelease := cleaned["old-model"]
	mu.Unlock()
	if oldCleanedBeforeRelease != 0 {
		t.Fatalf("old generation cleaned while leased = %d", oldCleanedBeforeRelease)
	}
	oldLease.Release()
	mu.Lock()
	oldCleanedAfterRelease := cleaned["old-model"]
	mu.Unlock()
	if oldCleanedAfterRelease != 1 {
		t.Fatalf("old generation cleanup count = %d, want 1", oldCleanedAfterRelease)
	}
	newLease.Release()
}

func TestRuntimeGenerationManagerSkipsEquivalentCandidate(t *testing.T) {
	source := &generationTestSource{
		current:   generationTestReader("same-model"),
		candidate: generationTestReader("same-model"),
	}
	buildCount := 0
	manager, err := NewRuntimeGenerationManager(context.Background(), RuntimeGenerationManagerOptions{
		Source: source,
		Build: func(context.Context, agentsettings.Reader) (ChannelRuntimeBundle, error) {
			buildCount++
			return ChannelRuntimeBundle{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewRuntimeGenerationManager() error = %v", err)
	}
	defer manager.Close()

	if err := manager.Reload(context.Background()); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if buildCount != 1 {
		t.Fatalf("build count = %d, want only the initial build", buildCount)
	}
	lease, err := manager.Capture()
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	defer lease.Release()
	if got := lease.Generation(); got != 1 {
		t.Fatalf("generation = %d, want 1", got)
	}
}

func TestRuntimeGenerationManagerKeepsCurrentWhenBuildFails(t *testing.T) {
	source := &generationTestSource{
		current:   generationTestReader("working-model"),
		candidate: generationTestReader("broken-model"),
	}
	manager, err := NewRuntimeGenerationManager(context.Background(), RuntimeGenerationManagerOptions{
		Source: source,
		Build: func(_ context.Context, reader agentsettings.Reader) (ChannelRuntimeBundle, error) {
			if reader.GetString("llm.model") == "broken-model" {
				return ChannelRuntimeBundle{}, errors.New("invalid candidate")
			}
			return ChannelRuntimeBundle{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewRuntimeGenerationManager() error = %v", err)
	}
	defer manager.Close()

	if err := manager.Reload(context.Background()); err == nil {
		t.Fatal("Reload() error = nil, want candidate build error")
	}
	lease, err := manager.Capture()
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	defer lease.Release()
	if got := lease.Reader().GetString("llm.model"); got != "working-model" {
		t.Fatalf("current model after failed build = %q", got)
	}
	if got := source.CurrentReader().GetString("llm.model"); got != "working-model" {
		t.Fatalf("source model after failed build = %q", got)
	}
}

func TestRuntimeGenerationManagerPollsConfigChanges(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("model: old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := &generationTestSource{
		current:    generationTestReader("old-model"),
		candidate:  generationTestReader("new-model"),
		configPath: configPath,
	}
	manager, err := NewRuntimeGenerationManager(context.Background(), RuntimeGenerationManagerOptions{
		Source:       source,
		PollInterval: 10 * time.Millisecond,
		Build: func(_ context.Context, reader agentsettings.Reader) (ChannelRuntimeBundle, error) {
			return ChannelRuntimeBundle{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	manager.Start(context.Background())

	// Let the poller record the initial fingerprint before changing the file.
	time.Sleep(30 * time.Millisecond)
	if err := os.WriteFile(configPath, []byte("model: new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		lease, captureErr := manager.Capture()
		if captureErr != nil {
			t.Fatal(captureErr)
		}
		model := lease.Reader().GetString("llm.model")
		generation := lease.Generation()
		lease.Release()
		if generation > 1 && model == "new-model" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("config poller did not switch to the candidate generation")
}

func TestRuntimeGenerationManagerDetectsConfigChangeBeforePollerStarts(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("model: old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := &generationTestSource{
		current:    generationTestReader("old-model"),
		candidate:  generationTestReader("new-model"),
		configPath: configPath,
	}
	manager, err := NewRuntimeGenerationManager(context.Background(), RuntimeGenerationManagerOptions{
		Source:       source,
		PollInterval: 10 * time.Millisecond,
		Build: func(_ context.Context, reader agentsettings.Reader) (ChannelRuntimeBundle, error) {
			return ChannelRuntimeBundle{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	if err := os.WriteFile(configPath, []byte("model: new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager.Start(context.Background())

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		lease, captureErr := manager.Capture()
		if captureErr != nil {
			t.Fatal(captureErr)
		}
		model := lease.Reader().GetString("llm.model")
		generation := lease.Generation()
		lease.Release()
		if generation > 1 && model == "new-model" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("config change made before Start was not applied")
}

func TestRuntimeGenerationReloadKeepsControlPlaneAvailable(t *testing.T) {
	source := &generationTestSource{
		current:   generationTestReader("old-model"),
		candidate: generationTestReader("new-model"),
	}
	manager, err := NewRuntimeGenerationManager(context.Background(), RuntimeGenerationManagerOptions{
		Source: source,
		Build: func(_ context.Context, reader agentsettings.Reader) (ChannelRuntimeBundle, error) {
			return ChannelRuntimeBundle{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		lease, captureErr := manager.Capture()
		if captureErr != nil {
			http.Error(w, captureErr.Error(), http.StatusServiceUnavailable)
			return
		}
		defer lease.Release()
		_, _ = io.WriteString(w, lease.Reader().GetString("llm.model"))
	}))
	defer server.Close()

	readModel := func() string {
		t.Helper()
		response, getErr := http.Get(server.URL)
		if getErr != nil {
			t.Fatal(getErr)
		}
		defer response.Body.Close()
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("control plane status = %d, body = %s", response.StatusCode, body)
		}
		return string(body)
	}
	if got := readModel(); got != "old-model" {
		t.Fatalf("model before reload = %q", got)
	}
	if err := manager.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := readModel(); got != "new-model" {
		t.Fatalf("model after reload = %q", got)
	}
}
