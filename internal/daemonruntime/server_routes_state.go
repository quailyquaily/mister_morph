package daemonruntime

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/contacts"
)

func (routes *routeRegistration) registerStateRoutes() {
	mux := routes.mux
	opts := routes.options
	mode := routes.mode
	authToken := routes.authToken
	capturedPaths := routes.paths
	statePaths := routes.statePaths
	settingsReader := routes.settingsReader
	cronRun := opts.CronRun

	mux.HandleFunc("/state/files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := statePaths
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": describeStateFiles(paths, ""),
		})
	})
	mux.HandleFunc("/state/files/", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := statePaths
		name := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/state/files/"))
		spec, ok := resolveStateFileSpec(paths, "", name)
		if !ok {
			http.Error(w, "invalid file name", http.StatusBadRequest)
			return
		}
		handleTextFileDetail(w, r, spec.Name, spec.Path)
	})

	mux.HandleFunc("/todo/files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := statePaths
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": describeStateFiles(paths, "todo"),
		})
	})
	mux.HandleFunc("/todo/files/", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := statePaths
		name := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/todo/files/"))
		spec, ok := resolveStateFileSpec(paths, "todo", name)
		if !ok {
			http.Error(w, "invalid file name", http.StatusBadRequest)
			return
		}
		handleTextFileDetail(w, r, spec.Name, spec.Path)
	})
	mux.HandleFunc("/todo/tasks", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := statePaths
		handleTodoTasks(w, r, paths.cronPath, paths.contactsDir, mode, settingsReader)
	})
	mux.HandleFunc("/todo/tasks/", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := statePaths
		handleTodoTaskRun(w, r, paths.cronPath, cronRun)
	})

	mux.HandleFunc("/contacts/files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := statePaths
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": describeStateFiles(paths, "contacts"),
		})
	})
	mux.HandleFunc("/contacts/files/", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := statePaths
		name := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/contacts/files/"))
		spec, ok := resolveStateFileSpec(paths, "contacts", name)
		if !ok {
			http.Error(w, "invalid file name", http.StatusBadRequest)
			return
		}
		handleTextFileDetail(w, r, spec.Name, spec.Path)
	})
	mux.HandleFunc("/contacts/chat-profile", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := statePaths
		handleContactsChatProfile(w, r, paths.contactsDir)
	})
	mux.HandleFunc("/contacts/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := statePaths
		service := contacts.NewService(contacts.NewFileStore(paths.contactsDir))
		items, err := listContactsForConsole(r.Context(), service)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": items,
		})
	})
	mux.HandleFunc("/contacts/item", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := statePaths
		store := contacts.NewFileStore(paths.contactsDir)
		svc := contacts.NewService(store)

		switch r.Method {
		case http.MethodGet:
			contactID := strings.TrimSpace(r.URL.Query().Get("contact_id"))
			if contactID == "" {
				http.Error(w, "contact_id is required", http.StatusBadRequest)
				return
			}
			block, ok, err := store.GetContactYAML(r.Context(), contactID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !ok {
				http.Error(w, "contact not found", http.StatusNotFound)
				return
			}
			item, ok, err := getConsoleContactByID(r.Context(), svc, contactID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !ok {
				http.Error(w, "contact not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"item": item,
				"yaml": block.YAML,
			})
			return
		case http.MethodPut:
		case http.MethodDelete:
			contactID := strings.TrimSpace(r.URL.Query().Get("contact_id"))
			if contactID == "" {
				http.Error(w, "contact_id is required", http.StatusBadRequest)
				return
			}
			block, deleted, err := store.DeleteContactYAML(r.Context(), contactID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !deleted {
				http.Error(w, "contact not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"deleted":    true,
				"contact_id": block.ContactID,
				"status":     string(block.Status),
			})
			return
		default:
			w.Header().Set("Allow", "GET, PUT, DELETE")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			ContactID string `json:"contact_id"`
			YAML      string `json:"yaml"`
		}
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&payload); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		payload.ContactID = strings.TrimSpace(payload.ContactID)
		if payload.ContactID == "" {
			http.Error(w, "contact_id is required", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(payload.YAML) == "" {
			http.Error(w, "yaml is required", http.StatusBadRequest)
			return
		}
		if _, ok, err := getConsoleContactByID(r.Context(), svc, payload.ContactID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		} else if !ok {
			http.Error(w, "contact not found", http.StatusNotFound)
			return
		}
		block, err := store.PutContactYAML(r.Context(), payload.ContactID, payload.YAML)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		updated, ok, err := getConsoleContactByID(r.Context(), svc, payload.ContactID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "contact not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"item": updated,
			"yaml": block.YAML,
		})
	})

	mux.HandleFunc("/persona/files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := statePaths
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": describeStateFiles(paths, "persona"),
		})
	})
	mux.HandleFunc("/persona/files/", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := statePaths
		name := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/persona/files/"))
		spec, ok := resolveStateFileSpec(paths, "persona", name)
		if !ok {
			http.Error(w, "invalid file name", http.StatusBadRequest)
			return
		}
		handleTextFileDetail(w, r, spec.Name, spec.Path)
	})
	mux.HandleFunc("/persona/avatar", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := statePaths
		handlePersonaAvatar(w, r, paths.avatarPath)
	})

	mux.HandleFunc("/audit/files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := statePaths
		items, err := listAuditFiles(paths.auditPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"default_file": filepath.Base(strings.TrimSpace(paths.auditPath)),
			"items":        items,
		})
	})

	mux.HandleFunc("/audit/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := statePaths
		filePath, err := resolveAuditFilePath(paths.auditPath, strings.TrimSpace(r.URL.Query().Get("file")))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		limit, err := parseInt64QueryParamInRange(r.URL.Query().Get("limit"), auditDefaultLineLimit, auditMinLineLimit, auditMaxLineLimit)
		if err != nil {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		chunk, err := readAuditLogChunk(filePath, strings.TrimSpace(r.URL.Query().Get("cursor")), limit)
		if err != nil {
			if badRequest, ok := badRequestMessage(err); ok {
				http.Error(w, badRequest, http.StatusBadRequest)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		chunk.File = filepath.Base(filePath)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(chunk)
	})

	mux.HandleFunc("/logs/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		limit, err := parseInt64QueryParamInRange(r.URL.Query().Get("limit"), logDefaultLineLimit, logMinLineLimit, logMaxLineLimit)
		if err != nil {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		chunk, err := readLatestLogChunk(capturedPaths.LogDir, strings.TrimSpace(r.URL.Query().Get("cursor")), limit)
		if err != nil {
			if badRequest, ok := badRequestMessage(err); ok {
				http.Error(w, badRequest, http.StatusBadRequest)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(chunk)
	})

	mux.HandleFunc("/observations", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		limit, err := parseInt64QueryParamInRange(r.URL.Query().Get("limit"), observationDefaultLimit, observationMinLimit, observationMaxLimit)
		if err != nil {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		view, err := readObservationView(capturedPaths.JournalDir, capturedPaths.LogDir, r.URL.Query().Get("task_id"), r.URL.Query().Get("topic_id"), int(limit))
		if err != nil {
			if badRequest, ok := badRequestMessage(err); ok {
				http.Error(w, badRequest, http.StatusBadRequest)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(view)
	})

}
