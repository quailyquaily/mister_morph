package consolecmd

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestConsoleRuntimeRejectsPreparedGenerationAfterClose(t *testing.T) {
	stateDir := t.TempDir()
	cacheDir := t.TempDir()
	runtime := newConsoleGenerationTestRuntime(t, stateDir, cacheDir)
	next := consoleRuntimeBoundaryReader(stateDir, cacheDir)
	next.Set("llm.model", "prepared-before-close")
	next.Set("llm.api_key", "")
	prepared, err := runtime.prepareGeneration(next)
	if err != nil {
		t.Fatalf("prepareGeneration() error = %v", err)
	}
	t.Cleanup(prepared.cleanupNow)

	runtime.Close()
	err = runtime.applyPreparedGeneration(prepared)
	if !errors.Is(err, errConsoleExecutionClosed) {
		t.Fatalf("applyPreparedGeneration() after close error = %v, want %v", err, errConsoleExecutionClosed)
	}
	if got := runtime.currentGeneration(); got != nil {
		t.Fatalf("current generation after close = %p, want nil", got)
	}
}

func TestRetiredConsoleGenerationHandlerRejectsLateRequest(t *testing.T) {
	stateDir := t.TempDir()
	cacheDir := t.TempDir()
	runtime := newConsoleGenerationTestRuntime(t, stateDir, cacheDir)
	oldGeneration := runtime.currentGeneration()
	oldHandler := runtime.currentHandler()

	next := consoleRuntimeBoundaryReader(stateDir, cacheDir)
	next.Set("llm.model", "next-model")
	next.Set("llm.api_key", "")
	if err := runtime.ReloadAgentConfigFromReader(next); err != nil {
		t.Fatalf("ReloadAgentConfigFromReader() error = %v", err)
	}

	rec := httptest.NewRecorder()
	oldHandler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("old handler status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	oldGeneration.mu.Lock()
	refs := oldGeneration.refs
	cleaned := oldGeneration.cleaned
	oldGeneration.mu.Unlock()
	if refs != 0 || !cleaned {
		t.Fatalf("old generation after late request: refs=%d cleaned=%t, want refs=0 cleaned=true", refs, cleaned)
	}
}

func TestConsoleGenerationMarkRetiredSeparatesAdmissionFromCleanup(t *testing.T) {
	generation := &consoleLocalRuntimeGeneration{}

	shouldCleanup := generation.markRetired()
	if !shouldCleanup {
		t.Fatal("markRetired() = false, want true for an unreferenced generation")
	}
	if generation.tryAcquire() {
		t.Fatal("tryAcquire() succeeded after markRetired()")
	}
	generation.cleanupResources()
}

func TestConsoleGenerationCleanupWaitsForStartedHandler(t *testing.T) {
	stateDir := t.TempDir()
	cacheDir := t.TempDir()
	runtime := newConsoleGenerationTestRuntime(t, stateDir, cacheDir)
	oldGeneration := runtime.currentGeneration()

	started := make(chan struct{})
	unblock := make(chan struct{})
	var unblockOnce sync.Once
	releaseHandler := func() { unblockOnce.Do(func() { close(unblock) }) }
	defer releaseHandler()

	blockingHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-unblock
		w.WriteHeader(http.StatusNoContent)
	})
	runtime.handlerMu.Lock()
	runtime.handler = consoleGenerationHandler(oldGeneration, blockingHandler)
	runtime.handlerMu.Unlock()
	oldHandler := runtime.currentHandler()

	rec := httptest.NewRecorder()
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		oldHandler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/blocked", nil))
	}()
	waitConsoleGenerationTestSignal(t, started, "old handler start")

	next := consoleRuntimeBoundaryReader(stateDir, cacheDir)
	next.Set("llm.model", "next-model")
	next.Set("llm.api_key", "")
	if err := runtime.ReloadAgentConfigFromReader(next); err != nil {
		t.Fatalf("ReloadAgentConfigFromReader() error = %v", err)
	}

	oldGeneration.mu.Lock()
	refsBeforeResponse := oldGeneration.refs
	retiredBeforeResponse := oldGeneration.retired
	cleanedBeforeResponse := oldGeneration.cleaned
	oldGeneration.mu.Unlock()
	if refsBeforeResponse != 1 || !retiredBeforeResponse || cleanedBeforeResponse {
		t.Fatalf(
			"old generation during request: refs=%d retired=%t cleaned=%t, want refs=1 retired=true cleaned=false",
			refsBeforeResponse,
			retiredBeforeResponse,
			cleanedBeforeResponse,
		)
	}

	releaseHandler()
	waitConsoleGenerationTestSignal(t, requestDone, "old handler completion")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("old handler status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	oldGeneration.mu.Lock()
	refsAfterResponse := oldGeneration.refs
	cleanedAfterResponse := oldGeneration.cleaned
	oldGeneration.mu.Unlock()
	if refsAfterResponse != 0 || !cleanedAfterResponse {
		t.Fatalf("old generation after request: refs=%d cleaned=%t, want refs=0 cleaned=true", refsAfterResponse, cleanedAfterResponse)
	}
}

func newConsoleGenerationTestRuntime(t *testing.T, stateDir, cacheDir string) *consoleLocalRuntime {
	t.Helper()
	previousLogger := slog.Default()
	reader := consoleRuntimeBoundaryReader(stateDir, cacheDir)
	reader.Set("llm.api_key", "")
	runtime, err := newConsoleLocalRuntime(serveConfig{}, reader)
	if err != nil {
		t.Fatalf("newConsoleLocalRuntime() error = %v", err)
	}
	t.Cleanup(func() {
		runtime.Close()
		slog.SetDefault(previousLogger)
	})
	return runtime
}

func waitConsoleGenerationTestSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}
