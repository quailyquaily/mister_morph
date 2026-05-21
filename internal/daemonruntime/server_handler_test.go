package daemonruntime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewHandlerRootAndUnknownPath(t *testing.T) {
	handler := NewHandler(RoutesOptions{})

	rootReq := httptest.NewRequest(http.MethodGet, "/", nil)
	rootRec := httptest.NewRecorder()
	handler.ServeHTTP(rootRec, rootReq)
	if rootRec.Code != http.StatusOK {
		t.Fatalf("root status = %d, want %d (%s)", rootRec.Code, http.StatusOK, rootRec.Body.String())
	}
	if strings.TrimSpace(rootRec.Body.String()) != "ok" {
		t.Fatalf("root body = %q, want ok", rootRec.Body.String())
	}

	missingReq := httptest.NewRequest(http.MethodGet, "/settings/agent", nil)
	missingRec := httptest.NewRecorder()
	handler.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want %d (%s)", missingRec.Code, http.StatusNotFound, missingRec.Body.String())
	}
}
