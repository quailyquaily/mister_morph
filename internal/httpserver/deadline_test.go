package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
}

func (r *deadlineRecorder) SetReadDeadline(deadline time.Time) error {
	r.deadlines = append(r.deadlines, deadline)
	return nil
}

func TestWithBodyReadDeadlineAppliesAndClearsDeadline(t *testing.T) {
	var called bool
	handler := WithBodyReadDeadline(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}), func(*http.Request) time.Duration { return 30 * time.Second })
	recorder := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/tasks", nil))

	if !called {
		t.Fatal("next handler was not called")
	}
	if len(recorder.deadlines) != 2 {
		t.Fatalf("SetReadDeadline calls = %d, want 2", len(recorder.deadlines))
	}
	if recorder.deadlines[0].IsZero() {
		t.Fatal("first read deadline is zero")
	}
	if !recorder.deadlines[1].IsZero() {
		t.Fatalf("cleared read deadline = %v, want zero", recorder.deadlines[1])
	}
}

func TestWithBodyReadDeadlineLeavesRelaxedRouteUntouched(t *testing.T) {
	handler := WithBodyReadDeadline(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), func(*http.Request) time.Duration { return 0 })
	recorder := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/stream", nil))

	if len(recorder.deadlines) != 0 {
		t.Fatalf("SetReadDeadline calls = %d, want 0", len(recorder.deadlines))
	}
}
