package daemonruntime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/domainjournal"
)

func TestObservationsRouteReturnsJournalEventsAndRelatedLogs(t *testing.T) {
	stateDir := t.TempDir()
	paths := testRuntimePaths(stateDir)

	journal, err := domainjournal.New(domainjournal.JournalOptions{
		Dir:           paths.JournalDir,
		SyncEachWrite: true,
	})
	if err != nil {
		t.Fatalf("domainjournal.New() error = %v", err)
	}
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	appendObservationEventFixture(t, journal, domainjournal.Event{
		ID:            "evt_task_1",
		Time:          now.Format(time.RFC3339Nano),
		Domain:        "task",
		Type:          "task_update",
		SchemaVersion: 1,
		Trace: domainjournal.Trace{
			TraceID: "trace_1",
			Target:  "console",
			TopicID: "topic_1",
			TaskID:  "task_1",
		},
		Payload: json.RawMessage(`{"task":{"id":"task_1","task":"inspect issue"},"token":"secret-value"}`),
	})
	appendObservationEventFixture(t, journal, domainjournal.Event{
		ID:            "evt_task_2",
		Time:          now.Add(time.Second).Format(time.RFC3339Nano),
		Domain:        "task",
		Type:          "task_update",
		SchemaVersion: 1,
		Trace: domainjournal.Trace{
			TraceID: "trace_2",
			TopicID: "topic_2",
			TaskID:  "task_2",
		},
		Payload: json.RawMessage(`{"task":{"id":"task_2","task":"other issue"}}`),
	})
	if err := journal.Close(); err != nil {
		t.Fatalf("journal.Close() error = %v", err)
	}

	logDir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(logDir) error = %v", err)
	}
	writeLogFixture(t, logDir, "mistermorph-2026-06-15.jsonl", []string{
		`{"time":"2026-06-15T12:00:00Z","trace_id":"trace_1","msg":"matched log"}`,
		`{"time":"2026-06-15T12:00:01Z","trace_id":"trace_2","msg":"unrelated log"}`,
	})

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{AuthToken: "token", RuntimePaths: paths})

	req := httptest.NewRequest(http.MethodGet, "/observations?task_id=task_1&limit=10", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), stateDir) {
		t.Fatalf("response exposed absolute path: %s", rec.Body.String())
	}

	var view observationView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(view.Items) != 1 {
		t.Fatalf("items len = %d, want 1: %+v", len(view.Items), view.Items)
	}
	if got := view.Items[0]; got.ID != "evt_task_1" || got.Type != "task_update" || got.Trace.TraceID != "trace_1" {
		t.Fatalf("item = %+v, want task event with trace_1", got)
	}
	if !strings.Contains(string(view.Items[0].Payload), `"token":"[redacted]"`) {
		t.Fatalf("payload was not redacted: %s", string(view.Items[0].Payload))
	}
	if len(view.Logs) != 1 {
		t.Fatalf("logs len = %d, want 1: %+v", len(view.Logs), view.Logs)
	}
	if !strings.Contains(view.Logs[0].Line, "matched log") || strings.Contains(view.Logs[0].Line, "unrelated log") {
		t.Fatalf("logs = %+v, want only related log line", view.Logs)
	}
}

func TestObservationsRouteUsesIndexInsteadOfFullJournalScan(t *testing.T) {
	stateDir := t.TempDir()
	paths := testRuntimePaths(stateDir)

	journalDir := paths.JournalDir
	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(journalDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(journalDir, "events.000000000000000001.jsonl"), []byte("{bad json}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(journal) error = %v", err)
	}
	journal, err := domainjournal.New(domainjournal.JournalOptions{
		Dir:           journalDir,
		SyncEachWrite: true,
	})
	if err != nil {
		t.Fatalf("domainjournal.New() error = %v", err)
	}
	appendObservationEventFixture(t, journal, domainjournal.Event{
		ID:            "evt_task_indexed",
		Time:          "2026-06-15T12:00:00Z",
		Domain:        "task",
		Type:          "task_update",
		SchemaVersion: 1,
		Trace: domainjournal.Trace{
			TraceID: "trace_indexed",
			Target:  "console",
			TopicID: "topic_indexed",
			TaskID:  "task_indexed",
		},
		Payload: json.RawMessage(`{"task":{"id":"task_indexed","task":"indexed event"}}`),
	})
	if err := journal.Close(); err != nil {
		t.Fatalf("journal.Close() error = %v", err)
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{AuthToken: "token", RuntimePaths: paths})

	req := httptest.NewRequest(http.MethodGet, "/observations?task_id=task_indexed&limit=10", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var view observationView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(view.Items) != 1 || view.Items[0].ID != "evt_task_indexed" {
		t.Fatalf("items = %+v, want indexed task event", view.Items)
	}
}

func TestObservationsRouteRequiresTaskOrTopicID(t *testing.T) {
	stateDir := t.TempDir()

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{AuthToken: "token", RuntimePaths: testRuntimePaths(stateDir)})

	req := httptest.NewRequest(http.MethodGet, "/observations", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func appendObservationEventFixture(t *testing.T, journal *domainjournal.Journal, event domainjournal.Event) {
	t.Helper()
	if _, err := journal.Append(event); err != nil {
		t.Fatalf("journal.Append(%s) error = %v", event.ID, err)
	}
}
