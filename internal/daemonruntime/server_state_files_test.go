package daemonruntime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestRuntimeStateFileSpecsIncludesHeartbeat(t *testing.T) {
	paths := runtimeStatePaths{
		todoWIP:          "/tmp/TODO.md",
		todoDone:         "/tmp/TODO.DONE.md",
		contactsActive:   "/tmp/ACTIVE.md",
		contactsInactive: "/tmp/INACTIVE.md",
		identityPath:     "/tmp/IDENTITY.md",
		soulPath:         "/tmp/SOUL.md",
		heartbeatPath:    "/tmp/HEARTBEAT.md",
	}

	items := describeStateFiles(paths, "")
	if len(items) != 7 {
		t.Fatalf("len(items) = %d, want 7", len(items))
	}

	foundHeartbeat := false
	for _, item := range items {
		if item["name"] == "HEARTBEAT.md" && item["group"] == "heartbeat" {
			foundHeartbeat = true
			break
		}
	}
	if !foundHeartbeat {
		t.Fatalf("HEARTBEAT.md should be present in state files: %#v", items)
	}
}

func TestResolveStateFileSpec(t *testing.T) {
	paths := runtimeStatePaths{
		todoWIP:          "/tmp/TODO.md",
		todoDone:         "/tmp/TODO.DONE.md",
		contactsActive:   "/tmp/ACTIVE.md",
		contactsInactive: "/tmp/INACTIVE.md",
		identityPath:     "/tmp/IDENTITY.md",
		soulPath:         "/tmp/SOUL.md",
		heartbeatPath:    "/tmp/HEARTBEAT.md",
	}

	if spec, ok := resolveStateFileSpec(paths, "", "heartbeat.md"); !ok || spec.Group != "heartbeat" {
		t.Fatalf("resolve heartbeat failed: ok=%v spec=%#v", ok, spec)
	}
	if _, ok := resolveStateFileSpec(paths, "todo", "ACTIVE.md"); ok {
		t.Fatalf("resolve with wrong group should fail")
	}
	if spec, ok := resolveStateFileSpec(paths, "todo", "todo.md"); !ok || spec.Name != "TODO.md" {
		t.Fatalf("resolve todo failed: ok=%v spec=%#v", ok, spec)
	}
}

func TestStateFilesRoute(t *testing.T) {
	stateDir := t.TempDir()
	oldStateDir := viper.GetString("file_state_dir")
	oldContactsDir := viper.GetString("contacts.dir_name")
	t.Cleanup(func() {
		viper.Set("file_state_dir", oldStateDir)
		viper.Set("contacts.dir_name", oldContactsDir)
	})
	viper.Set("file_state_dir", stateDir)
	viper.Set("contacts.dir_name", "contacts")

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "serve",
		AuthToken: "token",
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
	if len(payload.Items) != 7 {
		t.Fatalf("len(items) = %d, want 7", len(payload.Items))
	}
}

func TestContactsListRoute(t *testing.T) {
	stateDir := t.TempDir()
	oldStateDir := viper.GetString("file_state_dir")
	oldContactsDir := viper.GetString("contacts.dir_name")
	t.Cleanup(func() {
		viper.Set("file_state_dir", oldStateDir)
		viper.Set("contacts.dir_name", oldContactsDir)
	})
	viper.Set("file_state_dir", stateDir)
	viper.Set("contacts.dir_name", "contacts")

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

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "serve",
		AuthToken: "token",
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
			} `json:"items"`
			Total   int64 `json:"total"`
			Offset  int64 `json:"offset"`
			Limit   int64 `json:"limit"`
			HasMore bool  `json:"has_more"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		if len(payload.Items) != 2 {
			t.Fatalf("len(items) = %d, want 2", len(payload.Items))
		}
		if payload.Total != 2 {
			t.Fatalf("total = %d, want 2", payload.Total)
		}
		if payload.Offset != 0 {
			t.Fatalf("offset = %d, want 0", payload.Offset)
		}
		if payload.Limit != 0 {
			t.Fatalf("limit = %d, want 0", payload.Limit)
		}
		if payload.HasMore {
			t.Fatalf("has_more = true, want false")
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
	})

	t.Run("offset and limit", func(t *testing.T) {
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
			Total   int64 `json:"total"`
			Offset  int64 `json:"offset"`
			Limit   int64 `json:"limit"`
			HasMore bool  `json:"has_more"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		if len(payload.Items) != 1 {
			t.Fatalf("len(items) = %d, want 1", len(payload.Items))
		}
		if got := payload.Items[0].ContactID; got != "tg:@alice" {
			t.Fatalf("contact_id = %q, want tg:@alice", got)
		}
		if payload.Total != 2 {
			t.Fatalf("total = %d, want 2", payload.Total)
		}
		if payload.Offset != 1 {
			t.Fatalf("offset = %d, want 1", payload.Offset)
		}
		if payload.Limit != 1 {
			t.Fatalf("limit = %d, want 1", payload.Limit)
		}
		if payload.HasMore {
			t.Fatalf("has_more = true, want false")
		}
	})

	t.Run("invalid offset", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/contacts/list?offset=-1", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("invalid limit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/contacts/list?limit=oops", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
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
