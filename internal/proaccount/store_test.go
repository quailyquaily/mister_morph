package proaccount

import (
	"path/filepath"
	"testing"
	"time"
)

func TestWriteSessionReadStatus(t *testing.T) {
	stateDir := t.TempDir()
	session := StoredSession{
		AccessToken:        "access-token",
		RefreshToken:       "refresh-token",
		SubscriptionAPIKey: "subscription-key",
		UserInfo: map[string]any{
			"union_id": "union-1",
			"email":    "user@example.test",
		},
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	if err := WriteSession(stateDir, session); err != nil {
		t.Fatalf("WriteSession() error = %v", err)
	}

	status := ReadStatus(stateDir, time.Now().UTC())
	if !status.LoggedIn {
		t.Fatalf("LoggedIn = false, want true")
	}
	if !status.SubscriptionAPIKeyPresent {
		t.Fatalf("SubscriptionAPIKeyPresent = false, want true")
	}
	if UnionID(status.UserInfo) != "union-1" {
		t.Fatalf("UnionID(status.UserInfo) = %q, want union-1", UnionID(status.UserInfo))
	}
	if got := filepath.ToSlash(SessionPath(stateDir)); got != filepath.ToSlash(filepath.Join(stateDir, "auth", "pro-oauth.json")) {
		t.Fatalf("SessionPath() = %q", got)
	}
}
