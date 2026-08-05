package daemonruntime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/runtimecommands"
)

func (routes *routeRegistration) registerSystemRoutes() {
	mux := routes.mux
	opts := routes.options
	mode := routes.mode
	startedAt := routes.startedAt
	authToken := routes.authToken
	capturedPaths := routes.paths
	statePaths := routes.statePaths
	instanceID := routes.instanceID
	settingsReader := routes.settingsReader
	pricingConfigPath := strings.TrimSpace(settingsReader.GetString("config"))
	if pricingConfigPath == "" {
		pricingConfigPath = strings.TrimSpace(settingsReader.ConfigFileUsed())
	}
	submit := opts.TaskTopic.Submit
	overview := opts.Overview
	poke := opts.Poke
	topicMetadata := opts.TaskTopic.TopicMetadata
	var pokeMu sync.RWMutex
	lastPokeAt := ""
	if overview == nil {
		overview = func(context.Context) (map[string]any, error) {
			return buildDefaultOverviewPayload(mode, startedAt), nil
		}
	}
	resolveAgentName := func() string {
		if opts.AgentNameFunc != nil {
			return strings.TrimSpace(opts.AgentNameFunc())
		}
		return strings.TrimSpace(opts.AgentName)
	}

	if opts.AgentSettingsEnabled {
		registerRuntimeAgentSettingsRoutes(mux, authToken, opts.AgentSettingsOwner, settingsReader)
	}

	if opts.HealthEnabled {
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead:
			default:
				w.Header().Set("Allow", "GET, HEAD")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			payload := map[string]any{
				"ok":             true,
				"time":           time.Now().Format(time.RFC3339Nano),
				"submit_enabled": submit != nil,
			}
			if mode != "" {
				payload["mode"] = mode
			}
			if agentName := resolveAgentName(); agentName != "" {
				payload["agent_name"] = agentName
			}
			if instanceID != "" {
				payload["instance_id"] = instanceID
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if r.Method == http.MethodHead {
				return
			}
			_ = json.NewEncoder(w).Encode(payload)
		})
	}

	mux.HandleFunc("/commands", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": runtimecommands.Suggestions(),
		})
	})

	mux.HandleFunc("/llm/profiles", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		catalog, err := runtimeLLMProfiles(settingsReader)
		if err != nil {
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(catalog)
	})

	mux.HandleFunc("/overview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		payload, err := overview(r.Context())
		if err != nil {
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
			return
		}
		if payload == nil {
			payload = map[string]any{}
		}
		if _, ok := payload["health"]; !ok {
			payload["health"] = "ok"
		}
		if _, ok := payload["mode"]; !ok && mode != "" {
			payload["mode"] = mode
		}
		if _, ok := payload["agent_name"]; !ok {
			if agentName := resolveAgentName(); agentName != "" {
				payload["agent_name"] = agentName
			}
		}
		if _, ok := payload["submit_enabled"]; !ok {
			payload["submit_enabled"] = submit != nil
		}
		if _, ok := payload["instance_id"]; !ok && instanceID != "" {
			payload["instance_id"] = instanceID
		}
		if _, ok := payload["started_at"]; !ok {
			payload["started_at"] = startedAt.Format(time.RFC3339)
		}
		if _, ok := payload["uptime_sec"]; !ok {
			payload["uptime_sec"] = int(time.Since(startedAt).Seconds())
		}
		pokeMu.RLock()
		currentLastPokeAt := lastPokeAt
		pokeMu.RUnlock()
		if strings.TrimSpace(currentLastPokeAt) != "" {
			payload["last_poke_at"] = currentLastPokeAt
		}
		if rawVersion, ok := payload["version"].(string); !ok || strings.TrimSpace(rawVersion) == "" {
			payload["version"] = buildVersion()
		}
		ensureRuntimeMetrics(payload)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	})

	mux.HandleFunc("/topic/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if topicMetadata == nil {
			http.Error(w, "topic metadata is unavailable", http.StatusServiceUnavailable)
			return
		}
		topicID, ok := parseTopicMetadataPath(r.URL.Path)
		if !ok {
			if strings.HasPrefix(r.URL.Path, "/topic/") && strings.HasSuffix(r.URL.Path, "/metadata") {
				http.Error(w, "topic_id is required", http.StatusBadRequest)
				return
			}
			http.NotFound(w, r)
			return
		}
		payload, err := topicMetadata(r.Context(), topicID)
		if err != nil {
			if msg, ok := badRequestMessage(err); ok {
				http.Error(w, msg, http.StatusBadRequest)
				return
			}
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	})

	mux.HandleFunc("/poke", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		input, err := readPokeInput(r)
		if err != nil {
			if errors.Is(err, ErrPokeBodyTooLarge) {
				http.Error(w, strings.TrimSpace(err.Error()), http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusBadRequest)
			return
		}
		if !input.HasBody || strings.TrimSpace(input.BodyText) == "" {
			http.Error(w, "poke body text is required", http.StatusBadRequest)
			return
		}
		if poke == nil {
			http.Error(w, "poke unavailable", http.StatusServiceUnavailable)
			return
		}
		if err := poke(r.Context(), input); err != nil {
			if errors.Is(err, ErrPokeBusy) {
				http.Error(w, "awareness already running", http.StatusConflict)
				return
			}
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
			return
		}
		pokedAt := time.Now().UTC().Format(time.RFC3339Nano)
		pokeMu.Lock()
		lastPokeAt = pokedAt
		pokeMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"mode":     mode,
			"poked_at": pokedAt,
		})
	})

	mux.HandleFunc("/stats/llm/usage", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		store := llmstats.NewProjectionStoreWithOptions(
			capturedPaths.LLMUsageJournalDir,
			capturedPaths.LLMUsageProjectionPath,
			llmstats.ProjectionOptions{
				PricingFile: settingsReader.GetString("llm.pricing_file"),
				ConfigPath:  pricingConfigPath,
			},
		)
		proj, err := store.Refresh()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"generated_at":      time.Now().UTC().Format(time.RFC3339),
			"updated_at":        proj.UpdatedAt,
			"projected_offset":  proj.ProjectedOffset,
			"projected_records": proj.ProjectedRecords,
			"skipped_records":   proj.SkippedRecords,
			"summary":           proj.Summary,
			"api_hosts":         proj.APIHosts,
			"models":            proj.Models,
		})
	})

	mux.HandleFunc("/system/diagnostics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		paths := statePaths
		checks := []map[string]any{
			{"id": "runtime_mode", "ok": strings.TrimSpace(mode) != "", "detail": strings.TrimSpace(mode)},
			diagnoseDirWritable("file_state_dir", paths.stateDir),
			diagnoseDirWritable("file_cache_dir", paths.cacheDir),
			diagnoseFileReadable("contacts_active", paths.contactsActive),
			diagnoseFileReadable("contacts_inactive", paths.contactsInactive),
			diagnoseFileReadable("cron", paths.cronPath),
			diagnoseFileReadable("persona_identity", paths.identityPath),
			diagnoseFileReadable("persona_soul", paths.soulPath),
			diagnoseFileReadable("heartbeat_checklist", paths.heartbeatPath),
			diagnoseFileReadable("audit_jsonl", paths.auditPath),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"started_at": startedAt.Format(time.RFC3339),
			"version":    buildVersion(),
			"checks":     checks,
		})
	})

}
