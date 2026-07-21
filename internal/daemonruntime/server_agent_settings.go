package daemonruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/skills"
)

type runtimeLLMConfigFieldsPayload = agentsettings.LLMConfigFieldsPayload
type runtimeLLMProfileSettingsPayload = agentsettings.LLMProfileSettingsPayload
type runtimeLLMSettingsPayload = agentsettings.LLMSettingsPayload

type runtimeToolEnabledPayload struct {
	Enabled bool `json:"enabled"`
}

type runtimeToolsSettingsPayload struct {
	WriteFile    runtimeToolEnabledPayload `json:"write_file"`
	Spawn        runtimeToolEnabledPayload `json:"spawn"`
	Coder        runtimeToolEnabledPayload `json:"coder"`
	ContactsSend runtimeToolEnabledPayload `json:"contacts_send"`
	TodoUpdate   runtimeToolEnabledPayload `json:"todo_update"`
	PlanCreate   runtimeToolEnabledPayload `json:"plan_create"`
	URLFetch     runtimeToolEnabledPayload `json:"url_fetch"`
	WebSearch    runtimeToolEnabledPayload `json:"web_search"`
	Bash         runtimeToolEnabledPayload `json:"bash"`
	PowerShell   runtimeToolEnabledPayload `json:"powershell"`
}

type runtimeSkillsSettingsPayload struct {
	Enabled   bool                     `json:"enabled"`
	Load      []string                 `json:"load"`
	Loaded    []runtimeSkillStatusItem `json:"loaded"`
	Available []runtimeSkillStatusItem `json:"available"`
}

type runtimeSkillStatusItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type runtimeSkillsConfig struct {
	Roots     []string
	Enabled   bool
	Requested []string
}

type runtimeAgentSettingsPayload struct {
	LLM   runtimeLLMSettingsPayload   `json:"llm"`
	Tools runtimeToolsSettingsPayload `json:"tools"`
}

type runtimeAgentSettingsEnvManagedField = agentsettings.EnvManagedField
type runtimeAgentSettingsEnvManagedPayload = agentsettings.EnvManagedPayload

type runtimeAgentSettingsModelsRequest struct {
	InferenceProvider string `json:"inference_provider"`
	Provider          string `json:"provider"`
	Endpoint          string `json:"endpoint"`
	APIKey            string `json:"api_key"`
}

type runtimeAgentSettingsTestRequest struct {
	LLM           runtimeLLMSettingsPayload `json:"llm"`
	TargetProfile *string                   `json:"target_profile,omitempty"`
}

type runtimeAgentSettingsTestResult = agentsettings.ConnectionTestResult

func registerRuntimeAgentSettingsRoutes(
	mux *http.ServeMux,
	authToken string,
	reader agentsettings.Reader,
) {
	mux.HandleFunc("/settings/agent", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			runtimeAgentSettingsWriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		switch r.Method {
		case http.MethodGet:
			handleRuntimeAgentSettingsGet(w, r, reader)
		case http.MethodPut:
			runtimeAgentSettingsWriteError(w, http.StatusMethodNotAllowed, "runtime settings are read-only")
		default:
			w.Header().Set("Allow", "GET, PUT")
			runtimeAgentSettingsWriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})

	mux.HandleFunc("/settings/agent/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			runtimeAgentSettingsWriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !checkAuth(r, authToken) {
			runtimeAgentSettingsWriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		handleRuntimeAgentSettingsModels(w, r, reader)
	})

	mux.HandleFunc("/settings/agent/test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			runtimeAgentSettingsWriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !checkAuth(r, authToken) {
			runtimeAgentSettingsWriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		handleRuntimeAgentSettingsTest(w, r, reader)
	})
}

func handleRuntimeAgentSettingsGet(
	w http.ResponseWriter,
	_ *http.Request,
	reader agentsettings.Reader,
) {
	settings, err := runtimeReadAgentSettingsFromReader(reader)
	if err != nil {
		runtimeAgentSettingsWriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	settings, envManaged := runtimeBuildAgentSettingsResponseView(settings)
	skillsPayload, err := runtimeBuildAgentSkillsSettingsPayload(reader)
	if err != nil {
		runtimeAgentSettingsWriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	configPath := runtimeAgentSettingsConfigPath(reader)
	configExists := false
	if strings.TrimSpace(configPath) != "" {
		if _, err := os.Stat(configPath); err == nil {
			configExists = true
		}
	}
	runtimeAgentSettingsWriteJSON(w, http.StatusOK, map[string]any{
		"llm":           settings.LLM,
		"env_managed":   envManaged,
		"skills":        skillsPayload,
		"tools":         settings.Tools,
		"config_path":   configPath,
		"config_exists": configExists,
		"config_valid":  true,
		"config_source": "runtime",
		"read_only":     true,
	})
}

func handleRuntimeAgentSettingsModels(
	w http.ResponseWriter,
	r *http.Request,
	reader agentsettings.Reader,
) {
	var req runtimeAgentSettingsModelsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		runtimeAgentSettingsWriteError(w, http.StatusBadRequest, "invalid json")
		return
	}

	current, err := runtimeSettingsFromReader(reader)
	if err != nil {
		runtimeAgentSettingsWriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	values, err := llmutil.RuntimeValuesFromReader(reader)
	if err != nil {
		runtimeAgentSettingsWriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	source := configutil.SecretRefSourceFromReader(reader)
	modelLookup, err := agentsettings.ResolveOpenAICompatibleModelLookup(
		current,
		agentsettings.ModelLookupRequest{
			InferenceProvider: req.InferenceProvider,
			Provider:          req.Provider,
			Endpoint:          req.Endpoint,
			APIKey:            req.APIKey,
			FileStateDir:      values.FileStateDir,
		},
		func(value string) (string, error) {
			return agentsettings.ResolveConnectionTestFieldValue(value, source)
		},
	)
	if err != nil {
		runtimeAgentSettingsWriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	models, err := agentsettings.FetchOpenAICompatibleModels(r.Context(), modelLookup.Endpoint, modelLookup.APIKey)
	if err != nil {
		runtimeAgentSettingsWriteError(w, http.StatusBadGateway, err.Error())
		return
	}
	runtimeAgentSettingsWriteJSON(w, http.StatusOK, map[string]any{
		"items": models,
	})
}

func handleRuntimeAgentSettingsTest(
	w http.ResponseWriter,
	r *http.Request,
	reader agentsettings.Reader,
) {
	var req runtimeAgentSettingsTestRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		runtimeAgentSettingsWriteError(w, http.StatusBadRequest, "invalid json")
		return
	}

	settings, err := runtimeResolveAgentSettingsTestLLMFromReader(reader, req)
	if err != nil {
		runtimeAgentSettingsWriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := runtimeDefaultAgentSettingsConnectionTest(r.Context(), reader, settings)
	if err != nil {
		runtimeAgentSettingsWriteError(w, http.StatusBadGateway, err.Error())
		return
	}

	runtimeAgentSettingsWriteJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"provider":   result.Provider,
		"api_base":   result.APIBase,
		"model":      result.Model,
		"benchmarks": result.Benchmarks,
	})
}

func runtimeReadAgentSettingsFromReader(r interface {
	llmutil.ConfigReader
	GetString(string) string
	GetBool(string) bool
}) (runtimeAgentSettingsPayload, error) {
	if r == nil {
		return runtimeAgentSettingsPayload{}, fmt.Errorf("config reader is nil")
	}
	values, err := agentsettings.EffectiveRuntimeValues(r)
	if err != nil {
		return runtimeAgentSettingsPayload{}, err
	}
	return runtimeAgentSettingsPayload{
		LLM: agentsettings.SettingsPayloadFromRuntimeValues(values),
		Tools: runtimeToolsSettingsPayload{
			WriteFile:    runtimeToolEnabledPayload{Enabled: r.GetBool("tools.write_file.enabled")},
			Spawn:        runtimeToolEnabledPayload{Enabled: r.GetBool("tools.spawn.enabled")},
			Coder:        runtimeToolEnabledPayload{Enabled: r.GetBool("tools.coder.enabled")},
			ContactsSend: runtimeToolEnabledPayload{Enabled: r.GetBool("tools.contacts_send.enabled")},
			TodoUpdate:   runtimeToolEnabledPayload{Enabled: r.GetBool("tools.todo_update.enabled")},
			PlanCreate:   runtimeToolEnabledPayload{Enabled: r.GetBool("tools.plan_create.enabled")},
			URLFetch:     runtimeToolEnabledPayload{Enabled: r.GetBool("tools.url_fetch.enabled")},
			WebSearch:    runtimeToolEnabledPayload{Enabled: r.GetBool("tools.web_search.enabled")},
			Bash:         runtimeToolEnabledPayload{Enabled: r.GetBool("tools.bash.enabled")},
			PowerShell:   runtimeToolEnabledPayload{Enabled: r.GetBool("tools.powershell.enabled")},
		},
	}, nil
}

func runtimeSettingsFromReader(reader agentsettings.Reader) (runtimeLLMSettingsPayload, error) {
	values, err := agentsettings.EffectiveRuntimeValues(reader)
	if err != nil {
		return runtimeLLMSettingsPayload{}, err
	}
	return agentsettings.SettingsPayloadFromRuntimeValues(values), nil
}

func runtimeBuildAgentSettingsResponseView(
	settings runtimeAgentSettingsPayload,
) (runtimeAgentSettingsPayload, runtimeAgentSettingsEnvManagedPayload) {
	envManaged := agentsettings.CurrentEnvManaged(settings.LLM.Provider)
	agentsettings.SanitizeManagedLLMFields(&settings.LLM.LLMConfigFieldsPayload, envManaged.LLM, settings.LLM.Provider)
	if len(envManaged.LLM) == 0 {
		envManaged.LLM = nil
	}
	if len(envManaged.LLMProfiles) == 0 {
		envManaged.LLMProfiles = nil
	}
	return settings, envManaged
}

func runtimeBuildAgentSkillsSettingsPayload(reader agentsettings.Reader) (runtimeSkillsSettingsPayload, error) {
	cfg := runtimeSkillsConfigFromReader(reader)
	loaded, available, err := runtimeBuildSkillStatus(cfg)
	if err != nil {
		return runtimeSkillsSettingsPayload{}, err
	}
	return runtimeSkillsSettingsPayload{
		Enabled:   cfg.Enabled,
		Load:      append([]string(nil), cfg.Requested...),
		Loaded:    loaded,
		Available: available,
	}, nil
}

func runtimeSkillsConfigFromReader(reader interface {
	GetString(string) string
	GetStringSlice(string) []string
	GetBool(string) bool
	IsSet(string) bool
}) runtimeSkillsConfig {
	if reader == nil {
		return runtimeSkillsConfig{}
	}
	enabled := true
	if reader.IsSet("skills.enabled") {
		enabled = reader.GetBool("skills.enabled")
	}
	return runtimeSkillsConfig{
		Roots: []string{
			pathutil.ResolveStateChildDir(
				reader.GetString("file_state_dir"),
				runtimeNormalizeSkillsDirName(reader.GetString("skills.dir_name")),
				"skills",
			),
		},
		Enabled:   enabled,
		Requested: append([]string(nil), reader.GetStringSlice("skills.load")...),
	}
}

func runtimeBuildSkillStatus(cfg runtimeSkillsConfig) ([]runtimeSkillStatusItem, []runtimeSkillStatusItem, error) {
	discovered, err := skills.Discover(skills.DiscoverOptions{Roots: cfg.Roots})
	if err != nil {
		return nil, nil, err
	}
	for i, sk := range discovered {
		sk, err := skills.LoadFrontmatter(sk, 64*1024)
		if err != nil {
			return nil, nil, err
		}
		discovered[i] = sk
	}

	loadedIDs := map[string]bool{}
	if cfg.Enabled {
		requested, loadAll := runtimeNormalizeSkillStatusRequests(cfg.Requested)
		if len(requested) == 0 {
			loadAll = true
		}
		if loadAll {
			for _, sk := range discovered {
				loadedIDs[strings.ToLower(strings.TrimSpace(sk.ID))] = true
			}
		} else {
			for _, query := range requested {
				sk, err := skills.Resolve(discovered, query)
				if err != nil {
					continue
				}
				loadedIDs[strings.ToLower(strings.TrimSpace(sk.ID))] = true
			}
		}
	}

	var loaded []runtimeSkillStatusItem
	var available []runtimeSkillStatusItem
	for _, sk := range discovered {
		item := runtimeSkillStatusItem{
			ID:          strings.TrimSpace(sk.ID),
			Name:        runtimeFirstNonEmpty(sk.Name, sk.ID),
			Description: strings.TrimSpace(sk.Description),
		}
		if loadedIDs[strings.ToLower(item.ID)] {
			loaded = append(loaded, item)
			continue
		}
		available = append(available, item)
	}
	return loaded, available, nil
}

func runtimeNormalizeSkillStatusRequests(requested []string) ([]string, bool) {
	seen := map[string]bool{}
	var out []string
	loadAll := false
	for _, raw := range requested {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		if item == "*" {
			loadAll = true
		}
		key := strings.ToLower(item)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out, loadAll
}

func runtimeNormalizeSkillsDirName(raw string) string {
	dirName := strings.TrimSpace(raw)
	if dirName == "" {
		return "skills"
	}
	return dirName
}

func runtimeResolveAgentSettingsTestLLMFromReader(
	reader agentsettings.Reader,
	req runtimeAgentSettingsTestRequest,
) (runtimeLLMSettingsPayload, error) {
	targetProfile := runtimeAgentSettingsTestTargetProfile(req)
	snapshot, err := runtimeResolveAgentSettingsTestSnapshotFromReader(reader, req, targetProfile)
	if err != nil {
		return runtimeLLMSettingsPayload{}, err
	}
	values, err := agentsettings.ResolveConnectionTestValues(
		reader,
		snapshot,
		targetProfile,
		configutil.SecretRefSourceFromReader(reader),
	)
	if err != nil {
		return runtimeLLMSettingsPayload{}, err
	}
	payload := agentsettings.SettingsPayloadFromRuntimeValues(values)
	payload.Profiles = nil
	payload.FallbackProfiles = nil
	return payload, nil
}

func runtimeResolveAgentSettingsTestSnapshotFromReader(
	reader agentsettings.Reader,
	req runtimeAgentSettingsTestRequest,
	targetProfile string,
) (runtimeLLMSettingsPayload, error) {
	current, err := runtimeSettingsFromReader(reader)
	if err != nil {
		return runtimeLLMSettingsPayload{}, err
	}
	includeProfiles := targetProfile != "" && !strings.EqualFold(targetProfile, llmutil.RouteProfileDefault)
	return agentsettings.ApplyLLMSettingsNonEmptyUpdate(current, req.LLM, includeProfiles), nil
}

func runtimeAgentSettingsTestTargetProfile(req runtimeAgentSettingsTestRequest) string {
	if req.TargetProfile == nil {
		return ""
	}
	return strings.TrimSpace(*req.TargetProfile)
}

func runtimeDefaultAgentSettingsConnectionTest(
	ctx context.Context,
	reader agentsettings.Reader,
	settings runtimeLLMSettingsPayload,
) (runtimeAgentSettingsTestResult, error) {
	values, err := agentsettings.ResolveConnectionTestValues(
		reader,
		settings,
		llmutil.RouteProfileDefault,
		configutil.SecretRefSourceFromReader(reader),
	)
	if err != nil {
		return runtimeAgentSettingsTestResult{}, err
	}
	return agentsettings.RunConnectionTest(ctx, values, agentsettings.ConnectionTestOptions{})
}

func runtimeAgentSettingsConfigPath(reader agentsettings.Reader) string {
	if reader == nil {
		return ""
	}
	if path := strings.TrimSpace(reader.ConfigFileUsed()); path != "" {
		return path
	}
	return strings.TrimSpace(reader.GetString("config"))
}

func runtimeFirstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func runtimeAgentSettingsWriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Vary", "Authorization")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func runtimeAgentSettingsWriteError(w http.ResponseWriter, status int, msg string) {
	runtimeAgentSettingsWriteJSON(w, status, map[string]any{"error": strings.TrimSpace(msg)})
}
