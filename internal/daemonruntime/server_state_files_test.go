package daemonruntime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/contacts"
)

func TestRuntimeStateFileSpecsIncludesHeartbeat(t *testing.T) {
	paths := runtimeStatePaths{
		cronPath:         "/tmp/cron.yaml",
		contactsActive:   "/tmp/ACTIVE.md",
		contactsInactive: "/tmp/INACTIVE.md",
		identityPath:     "/tmp/persona/identity.yaml",
		soulPath:         "/tmp/persona/soul.md",
		heartbeatPath:    "/tmp/HEARTBEAT.md",
	}

	items := describeStateFiles(paths, "")
	if len(items) != 6 {
		t.Fatalf("len(items) = %d, want 6", len(items))
	}

	foundHeartbeat := false
	foundCron := false
	for _, item := range items {
		if item["name"] == "HEARTBEAT.md" && item["group"] == "heartbeat" {
			foundHeartbeat = true
		}
		if item["name"] == "cron.yaml" && item["group"] == "cron" {
			foundCron = true
		}
	}
	if !foundHeartbeat {
		t.Fatalf("HEARTBEAT.md should be present in state files: %#v", items)
	}
	if !foundCron {
		t.Fatalf("cron.yaml should be present in state files: %#v", items)
	}
}

func TestResolveStateFileSpec(t *testing.T) {
	paths := runtimeStatePaths{
		cronPath:         "/tmp/cron.yaml",
		contactsActive:   "/tmp/ACTIVE.md",
		contactsInactive: "/tmp/INACTIVE.md",
		identityPath:     "/tmp/persona/identity.yaml",
		soulPath:         "/tmp/persona/soul.md",
		heartbeatPath:    "/tmp/HEARTBEAT.md",
	}

	if spec, ok := resolveStateFileSpec(paths, "", "heartbeat.md"); !ok || spec.Group != "heartbeat" {
		t.Fatalf("resolve heartbeat failed: ok=%v spec=%#v", ok, spec)
	}
	if _, ok := resolveStateFileSpec(paths, "cron", "ACTIVE.md"); ok {
		t.Fatalf("resolve with wrong group should fail")
	}
	if spec, ok := resolveStateFileSpec(paths, "cron", "cron.yaml"); !ok || spec.Name != "cron.yaml" {
		t.Fatalf("resolve cron failed: ok=%v spec=%#v", ok, spec)
	}
	if _, ok := resolveStateFileSpec(paths, "", "scripts.md"); ok {
		t.Fatalf("resolve scripts should fail")
	}
}

func TestStateFilesRoute(t *testing.T) {
	stateDir := t.TempDir()

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:         "serve",
		AuthToken:    "token",
		RuntimePaths: testRuntimePaths(stateDir),
	})

	req := httptest.NewRequest(http.MethodGet, "/state/files", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(payload.Items) != 6 {
		t.Fatalf("len(items) = %d, want 6", len(payload.Items))
	}
}

func TestPersonaAvatarRoute(t *testing.T) {
	stateDir := t.TempDir()

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:         "serve",
		AuthToken:    "token",
		RuntimePaths: testRuntimePaths(stateDir),
	})

	body := strings.NewReader("webp-avatar")
	req := httptest.NewRequest(http.MethodPut, "/persona/avatar", body)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "image/webp")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/persona/avatar", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/webp" {
		t.Fatalf("Content-Type = %q, want image/webp", got)
	}
	if got := rec.Body.String(); got != "webp-avatar" {
		t.Fatalf("body = %q, want webp-avatar", got)
	}

	req = httptest.NewRequest(http.MethodDelete, "/persona/avatar", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/persona/avatar", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET after delete status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestContactsListRoute(t *testing.T) {
	stateDir := t.TempDir()

	contactsDir := filepath.Join(stateDir, "contacts")
	if err := os.MkdirAll(contactsDir, 0o700); err != nil {
		t.Fatalf("mkdir contacts: %v", err)
	}

	activeDoc := strings.Join([]string{
		"# Active Contacts",
		"",
		"## Alice",
		"",
		"```yaml",
		"contact_id: \"tg:@alice\"",
		"nickname: \"Alice\"",
		"kind: \"human\"",
		"channel: \"telegram\"",
		"tg_username: \"alice\"",
		"tg_private_chat_id: \"12345\"",
		"last_interaction_at: \"2026-03-12T08:00:00Z\"",
		"topic_preferences:",
		"  - \"go\"",
		"persona_brief: \"core maintainer\"",
		"```",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(contactsDir, "ACTIVE.md"), []byte(activeDoc), 0o600); err != nil {
		t.Fatalf("write ACTIVE.md: %v", err)
	}

	inactiveDoc := strings.Join([]string{
		"# Inactive Contacts",
		"",
		"## Bob",
		"",
		"```yaml",
		"contact_id: \"slack:T001:U002\"",
		"nickname: \"Bob\"",
		"kind: \"human\"",
		"channel: \"slack\"",
		"slack_team_id: \"T001\"",
		"slack_user_id: \"U002\"",
		"last_interaction_at: \"2026-03-13T09:30:00Z\"",
		"persona_brief: \"former reviewer\"",
		"```",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(contactsDir, "INACTIVE.md"), []byte(inactiveDoc), 0o600); err != nil {
		t.Fatalf("write INACTIVE.md: %v", err)
	}
	avatarRaw, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatalf("decode avatar: %v", err)
	}
	contactStore := contacts.NewFileStore(contactsDir)
	if err := contactStore.PutContactAvatar(context.Background(), "tg:@alice", avatarRaw); err != nil {
		t.Fatalf("write contact avatar: %v", err)
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:         "serve",
		AuthToken:    "token",
		RuntimePaths: testRuntimePaths(stateDir),
	})

	t.Run("all", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/contacts/list", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
		}

		var payload struct {
			Items []struct {
				ContactID string `json:"contact_id"`
				Nickname  string `json:"nickname"`
				Status    string `json:"status"`
				AvatarURL string `json:"avatar_url"`
			} `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		if len(payload.Items) != 2 {
			t.Fatalf("len(items) = %d, want 2", len(payload.Items))
		}
		if got := payload.Items[0].ContactID; got != "slack:T001:U002" {
			t.Fatalf("items[0].contact_id = %q, want slack:T001:U002", got)
		}
		if got := payload.Items[1].ContactID; got != "tg:@alice" {
			t.Fatalf("items[1].contact_id = %q, want tg:@alice", got)
		}

		statusByID := map[string]string{}
		for _, item := range payload.Items {
			statusByID[item.ContactID] = item.Status
		}
		if got := statusByID["tg:@alice"]; got != "active" {
			t.Fatalf("status of tg:@alice = %q, want active", got)
		}
		if got := statusByID["slack:T001:U002"]; got != "inactive" {
			t.Fatalf("status of slack:T001:U002 = %q, want inactive", got)
		}
		if got := payload.Items[1].AvatarURL; !strings.HasPrefix(got, "/contacts/avatar?contact_id=tg%3A%40alice&v=") {
			t.Fatalf("alice avatar_url = %q, want versioned contact avatar URL", got)
		}
		if got := payload.Items[0].AvatarURL; got != "" {
			t.Fatalf("bob avatar_url = %q, want empty", got)
		}
	})

	t.Run("load contact avatar", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/contacts/avatar?contact_id=tg%3A%40alice", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "image/png" {
			t.Fatalf("content type = %q, want image/png", got)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
		}
		if string(rec.Body.Bytes()) != string(avatarRaw) {
			t.Fatal("avatar response body mismatch")
		}
	})

	t.Run("contact avatar errors", func(t *testing.T) {
		tests := []struct {
			name   string
			method string
			path   string
			auth   bool
			status int
			allow  string
		}{
			{name: "missing id", method: http.MethodGet, path: "/contacts/avatar", auth: true, status: http.StatusBadRequest},
			{name: "missing contact", method: http.MethodGet, path: "/contacts/avatar?contact_id=tg%3Amissing", auth: true, status: http.StatusNotFound},
			{name: "unauthorized", method: http.MethodGet, path: "/contacts/avatar?contact_id=tg%3A%40alice", status: http.StatusUnauthorized},
			{name: "method", method: http.MethodPost, path: "/contacts/avatar?contact_id=tg%3A%40alice", auth: true, status: http.StatusMethodNotAllowed, allow: "GET"},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				req := httptest.NewRequest(test.method, test.path, nil)
				if test.auth {
					req.Header.Set("Authorization", "Bearer token")
				}
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
				if rec.Code != test.status {
					t.Fatalf("status = %d, want %d (%s)", rec.Code, test.status, rec.Body.String())
				}
				if got := rec.Header().Get("Allow"); got != test.allow {
					t.Fatalf("Allow = %q, want %q", got, test.allow)
				}
			})
		}
	})

	t.Run("pagination query does not truncate contacts", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/contacts/list?offset=1&limit=1", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		var payload struct {
			Items []struct {
				ContactID string `json:"contact_id"`
			} `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		if len(payload.Items) != 2 {
			t.Fatalf("len(items) = %d, want 2", len(payload.Items))
		}
		if got := payload.Items[0].ContactID; got != "slack:T001:U002" {
			t.Fatalf("items[0].contact_id = %q, want slack:T001:U002", got)
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/contacts/list", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
		}
		if got := rec.Header().Get("Allow"); got != "GET" {
			t.Fatalf("allow = %q, want GET", got)
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/contacts/list", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("load contact yaml", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/contacts/item?contact_id=tg%3A%40alice", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		var payload struct {
			YAML string `json:"yaml"`
			Item struct {
				ContactID string `json:"contact_id"`
				Status    string `json:"status"`
			} `json:"item"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		if payload.Item.ContactID != "tg:@alice" || payload.Item.Status != "active" {
			t.Fatalf("unexpected item payload: %#v", payload.Item)
		}
		if !strings.Contains(payload.YAML, "contact_id: \"tg:@alice\"") && !strings.Contains(payload.YAML, "contact_id: tg:@alice") {
			t.Fatalf("yaml missing contact_id: %s", payload.YAML)
		}
	})

	t.Run("edit active contact", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/contacts/item", strings.NewReader(`{
			"contact_id":"tg:@alice",
			"yaml":"contact_id: tg:@alice\nnickname: Alice Updated\nkind: human\nchannel: telegram\ntg_username: alice\ntg_private_chat_id: \"12345\"\npersona_brief: updated maintainer\ntopic_preferences:\n  - go\n  - console\nx_note: keep-me"
		}`))
		req.Header.Set("Authorization", "Bearer token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		activeRaw, err := os.ReadFile(filepath.Join(contactsDir, "ACTIVE.md"))
		if err != nil {
			t.Fatalf("read ACTIVE.md: %v", err)
		}
		if !strings.Contains(string(activeRaw), `nickname: Alice Updated`) {
			t.Fatalf("ACTIVE.md missing updated nickname: %s", string(activeRaw))
		}
		if !strings.Contains(string(activeRaw), `persona_brief: updated maintainer`) {
			t.Fatalf("ACTIVE.md missing updated brief: %s", string(activeRaw))
		}
		if !strings.Contains(string(activeRaw), `- console`) {
			t.Fatalf("ACTIVE.md missing updated topic: %s", string(activeRaw))
		}
		if !strings.Contains(string(activeRaw), `x_note: keep-me`) {
			t.Fatalf("ACTIVE.md should preserve unknown yaml fields: %s", string(activeRaw))
		}
	})

	t.Run("edit inactive contact", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/contacts/item", strings.NewReader(`{
			"contact_id":"slack:T001:U002",
			"yaml":"contact_id: slack:T001:U002\nnickname: Bob Updated\nkind: agent\nchannel: slack\nslack_team_id: T001\nslack_user_id: U002\nslack_dm_channel_id: D002\npersona_brief: restored reviewer"
		}`))
		req.Header.Set("Authorization", "Bearer token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
		}

		inactiveRaw, err := os.ReadFile(filepath.Join(contactsDir, "INACTIVE.md"))
		if err != nil {
			t.Fatalf("read INACTIVE.md: %v", err)
		}
		if !strings.Contains(string(inactiveRaw), `nickname: Bob Updated`) {
			t.Fatalf("INACTIVE.md missing updated nickname: %s", string(inactiveRaw))
		}
		if !strings.Contains(string(inactiveRaw), `kind: agent`) {
			t.Fatalf("INACTIVE.md missing updated kind: %s", string(inactiveRaw))
		}
		if !strings.Contains(string(inactiveRaw), `slack_dm_channel_id: D002`) {
			t.Fatalf("INACTIVE.md missing dm channel: %s", string(inactiveRaw))
		}
	})

	t.Run("reject changing contact id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/contacts/item", strings.NewReader(`{
			"contact_id":"tg:@alice",
			"yaml":"contact_id: tg:@alice-new\nnickname: Alice"
		}`))
		req.Header.Set("Authorization", "Bearer token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusInternalServerError, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "contact_id cannot change") {
			t.Fatalf("unexpected error: %s", rec.Body.String())
		}
	})

	t.Run("delete active contact", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/contacts/item?contact_id=tg%3A%40alice", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		activeRaw, err := os.ReadFile(filepath.Join(contactsDir, "ACTIVE.md"))
		if err != nil {
			t.Fatalf("read ACTIVE.md: %v", err)
		}
		if strings.Contains(string(activeRaw), `contact_id: "tg:@alice"`) || strings.Contains(string(activeRaw), `contact_id: tg:@alice`) {
			t.Fatalf("ACTIVE.md should not contain deleted contact: %s", string(activeRaw))
		}
		if _, found, err := contactStore.ReadContactAvatar(context.Background(), "tg:@alice"); err != nil || found {
			t.Fatalf("avatar after contact delete = (found=%v, err=%v), want false, nil", found, err)
		}
	})

	t.Run("delete inactive contact", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/contacts/item?contact_id=slack%3AT001%3AU002", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		inactiveRaw, err := os.ReadFile(filepath.Join(contactsDir, "INACTIVE.md"))
		if err != nil {
			t.Fatalf("read INACTIVE.md: %v", err)
		}
		if strings.Contains(string(inactiveRaw), `contact_id: "slack:T001:U002"`) || strings.Contains(string(inactiveRaw), `contact_id: slack:T001:U002`) {
			t.Fatalf("INACTIVE.md should not contain deleted contact: %s", string(inactiveRaw))
		}
	})

	t.Run("delete missing contact", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/contacts/item?contact_id=tg%3A%40missing", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusNotFound, rec.Body.String())
		}
	})
}

func TestContactAvatarRouteDoesNotParseContactMarkdown(t *testing.T) {
	stateDir := t.TempDir()
	contactsDir := filepath.Join(stateDir, "contacts")
	store := contacts.NewFileStore(contactsDir)
	ctx := context.Background()
	if err := store.PutContact(ctx, contacts.Contact{
		ContactID:       "tg:@alice",
		Channel:         contacts.ChannelTelegram,
		ContactNickname: "Alice",
	}); err != nil {
		t.Fatalf("PutContact() error = %v", err)
	}
	avatarRaw, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatalf("decode avatar: %v", err)
	}
	if err := store.PutContactAvatar(ctx, "tg:@alice", avatarRaw); err != nil {
		t.Fatalf("PutContactAvatar() error = %v", err)
	}
	activePath := filepath.Join(contactsDir, "ACTIVE.md")
	activeRaw, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatalf("read ACTIVE.md: %v", err)
	}
	activeRaw = append(activeRaw, []byte("\n## Broken\n\n```yaml\ncontact_id: [\n```\n")...)
	if err := os.WriteFile(activePath, activeRaw, 0o600); err != nil {
		t.Fatalf("write malformed ACTIVE.md: %v", err)
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:         "serve",
		AuthToken:    "token",
		RuntimePaths: testRuntimePaths(stateDir),
	})
	req := httptest.NewRequest(http.MethodGet, "/contacts/avatar?contact_id=tg%3A%40alice", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
}
