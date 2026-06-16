package daemonruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	"github.com/quailyquaily/mistermorph/internal/llmbench"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/skills"
	"github.com/spf13/viper"
)

var runtimeAgentSettingsEnvRefPattern = regexp.MustCompile(`^\$\{([a-zA-Z_][a-zA-Z0-9_]*)\}$`)

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

type runtimeAgentSettingsEnvManagedField struct {
	EnvName  string `json:"env_name"`
	Value    string `json:"value,omitempty"`
	RawValue string `json:"raw_value,omitempty"`
}

type runtimeAgentSettingsEnvManagedPayload struct {
	LLM         map[string]runtimeAgentSettingsEnvManagedField            `json:"llm,omitempty"`
	LLMProfiles map[string]map[string]runtimeAgentSettingsEnvManagedField `json:"llm_profiles,omitempty"`
}

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

type runtimeAgentSettingsTestResult struct {
	Provider   string
	APIBase    string
	Model      string
	Benchmarks []llmbench.BenchmarkResult
}

func registerRuntimeAgentSettingsRoutes(
	mux *http.ServeMux,
	authToken string,
	readerFunc func() *viper.Viper,
) {
	mux.HandleFunc("/settings/agent", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			runtimeAgentSettingsWriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		switch r.Method {
		case http.MethodGet:
			handleRuntimeAgentSettingsGet(w, r, readerFunc)
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
		handleRuntimeAgentSettingsModels(w, r, readerFunc)
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
		handleRuntimeAgentSettingsTest(w, r, readerFunc)
	})
}

func handleRuntimeAgentSettingsGet(
	w http.ResponseWriter,
	_ *http.Request,
	readerFunc func() *viper.Viper,
) {
	reader := runtimeAgentSettingsReader(readerFunc)
	settings := runtimeReadAgentSettingsFromReader(reader)
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
	readerFunc func() *viper.Viper,
) {
	var req runtimeAgentSettingsModelsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		runtimeAgentSettingsWriteError(w, http.StatusBadRequest, "invalid json")
		return
	}

	current := runtimeSettingsFromReader(runtimeAgentSettingsReader(readerFunc))
	values := llmutil.RuntimeValuesFromReader(runtimeAgentSettingsReader(readerFunc))
	modelLookup, err := agentsettings.ResolveOpenAICompatibleModelLookup(
		current,
		agentsettings.ModelLookupRequest{
			InferenceProvider: req.InferenceProvider,
			Provider:          req.Provider,
			Endpoint:          req.Endpoint,
			APIKey:            req.APIKey,
			FileStateDir:      values.FileStateDir,
		},
		runtimeResolveAgentSettingsTestFieldValue,
	)
	if err != nil {
		runtimeAgentSettingsWriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	models, err := runtimeFetchOpenAICompatibleModels(r.Context(), modelLookup.Endpoint, modelLookup.APIKey)
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
	readerFunc func() *viper.Viper,
) {
	var req runtimeAgentSettingsTestRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		runtimeAgentSettingsWriteError(w, http.StatusBadRequest, "invalid json")
		return
	}

	reader := runtimeAgentSettingsReader(readerFunc)
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

func runtimeAgentSettingsReader(readerFunc func() *viper.Viper) *viper.Viper {
	if readerFunc == nil {
		return viper.GetViper()
	}
	if reader := readerFunc(); reader != nil {
		return reader
	}
	return viper.GetViper()
}

func runtimeReadAgentSettingsFromReader(r interface {
	GetString(string) string
	GetBool(string) bool
}) runtimeAgentSettingsPayload {
	if r == nil {
		return runtimeAgentSettingsPayload{}
	}
	values := runtimeCurrentLLMRuntimeValuesFromReader(r)
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
	}
}

func runtimeSettingsFromReader(reader *viper.Viper) runtimeLLMSettingsPayload {
	return agentsettings.SettingsPayloadFromRuntimeValues(runtimeCurrentLLMRuntimeValuesFromReader(reader))
}

func runtimeCurrentLLMRuntimeValuesFromReader(reader interface {
	GetString(string) string
}) llmutil.RuntimeValues {
	if reader == nil {
		reader = viper.GetViper()
	}
	values := llmutil.RuntimeValuesFromReader(reader)

	if _, value, ok := runtimeFirstManagedEnv("MISTER_MORPH_LLM_INFERENCE_PROVIDER"); ok {
		values.InferenceProvider = strings.TrimSpace(value)
	}
	if _, value, ok := runtimeFirstManagedEnv("MISTER_MORPH_LLM_PROVIDER"); ok {
		values.Provider = strings.TrimSpace(value)
	}
	if _, value, ok := runtimeFirstManagedEnv("MISTER_MORPH_LLM_ENDPOINT"); ok {
		values.Endpoint = strings.TrimSpace(value)
	}
	if _, value, ok := runtimeFirstManagedEnv("MISTER_MORPH_LLM_API_KEY"); ok {
		values.APIKey = strings.TrimSpace(value)
	}
	if _, value, ok := runtimeFirstManagedEnv("MISTER_MORPH_LLM_MODEL"); ok {
		values.Model = strings.TrimSpace(value)
	}
	if _, value, ok := runtimeFirstManagedEnv("MISTER_MORPH_LLM_CONTEXT_WINDOW_TOKENS"); ok {
		values.ContextWindowRaw = strings.TrimSpace(value)
	}
	if _, value, ok := runtimeFirstManagedEnv("MISTER_MORPH_LLM_AZURE_DEPLOYMENT"); ok {
		values.AzureDeployment = strings.TrimSpace(value)
	}
	if _, value, ok := runtimeFirstManagedEnv("MISTER_MORPH_LLM_REASONING_EFFORT"); ok {
		values.ReasoningEffortRaw = strings.TrimSpace(value)
	}
	if _, value, ok := runtimeFirstManagedEnv("MISTER_MORPH_LLM_TOOLS_EMULATION_MODE"); ok {
		values.ToolsEmulationMode = strings.TrimSpace(value)
	}
	if _, value, ok := runtimeFirstManagedEnv("MISTER_MORPH_LLM_BEDROCK_AWS_KEY"); ok {
		values.BedrockAWSKey = strings.TrimSpace(value)
	}
	if _, value, ok := runtimeFirstManagedEnv("MISTER_MORPH_LLM_BEDROCK_AWS_SECRET"); ok {
		values.BedrockAWSSecret = strings.TrimSpace(value)
	}
	if _, value, ok := runtimeFirstManagedEnv("MISTER_MORPH_LLM_BEDROCK_REGION"); ok {
		values.BedrockAWSRegion = strings.TrimSpace(value)
	}
	if _, value, ok := runtimeFirstManagedEnv("MISTER_MORPH_LLM_BEDROCK_MODEL_ARN"); ok {
		values.BedrockModelARN = strings.TrimSpace(value)
	}
	if _, value, ok := runtimeFirstManagedEnv("MISTER_MORPH_LLM_CLOUDFLARE_ACCOUNT_ID"); ok {
		values.CloudflareAccountID = strings.TrimSpace(value)
	}
	if _, value, ok := runtimeFirstManagedEnv("MISTER_MORPH_LLM_CLOUDFLARE_API_TOKEN"); ok {
		values.CloudflareAPIToken = strings.TrimSpace(value)
	}

	return values
}

func runtimeBuildAgentSettingsResponseView(
	settings runtimeAgentSettingsPayload,
) (runtimeAgentSettingsPayload, runtimeAgentSettingsEnvManagedPayload) {
	envManaged := runtimeCurrentAgentSettingsEnvManaged(settings.LLM.Provider)
	runtimeSanitizeAgentSettingsManagedLLMFields(&settings.LLM.LLMConfigFieldsPayload, envManaged.LLM, settings.LLM.Provider)
	if len(envManaged.LLM) == 0 {
		envManaged.LLM = nil
	}
	if len(envManaged.LLMProfiles) == 0 {
		envManaged.LLMProfiles = nil
	}
	return settings, envManaged
}

func runtimeCurrentAgentSettingsEnvManaged(provider string) runtimeAgentSettingsEnvManagedPayload {
	return runtimeAgentSettingsEnvManagedPayload{
		LLM: runtimeCurrentAgentSettingsLLMEnvManaged(provider),
	}
}

func runtimeCurrentAgentSettingsLLMEnvManaged(provider string) map[string]runtimeAgentSettingsEnvManagedField {
	fields := map[string]runtimeAgentSettingsEnvManagedField{}
	normalizedProvider := strings.TrimSpace(strings.ToLower(provider))
	isCodexProvider := normalizedProvider == "openai_codex"

	if field, ok := runtimeCurrentAgentSettingsManagedEnvField(false, "MISTER_MORPH_LLM_INFERENCE_PROVIDER"); ok {
		fields["inference_provider"] = field
	}
	if field, ok := runtimeCurrentAgentSettingsManagedEnvField(false, "MISTER_MORPH_LLM_PROVIDER"); ok {
		fields["provider"] = field
	}
	if !isCodexProvider {
		if field, ok := runtimeCurrentAgentSettingsManagedEnvField(false, "MISTER_MORPH_LLM_ENDPOINT"); ok {
			fields["endpoint"] = field
		}
	}
	if field, ok := runtimeCurrentAgentSettingsModelEnvField(provider); ok {
		fields["model"] = field
	}
	if field, ok := runtimeCurrentAgentSettingsManagedEnvField(false, "MISTER_MORPH_LLM_CONTEXT_WINDOW_TOKENS"); ok {
		fields["context_window_tokens"] = field
	}
	switch normalizedProvider {
	case "cloudflare":
		if field, ok := runtimeCurrentAgentSettingsManagedEnvField(
			true,
			"MISTER_MORPH_LLM_CLOUDFLARE_API_TOKEN",
			"MISTER_MORPH_LLM_API_KEY",
		); ok {
			fields["cloudflare_api_token"] = field
		}
	case "bedrock":
	default:
		if !isCodexProvider {
			if field, ok := runtimeCurrentAgentSettingsManagedEnvField(true, "MISTER_MORPH_LLM_API_KEY"); ok {
				fields["api_key"] = field
			}
			if field, ok := runtimeCurrentAgentSettingsManagedEnvField(true, "MISTER_MORPH_LLM_CLOUDFLARE_API_TOKEN"); ok {
				fields["cloudflare_api_token"] = field
			}
		}
	}
	if !isCodexProvider {
		if field, ok := runtimeCurrentAgentSettingsManagedEnvField(false, "MISTER_MORPH_LLM_CLOUDFLARE_ACCOUNT_ID"); ok {
			fields["cloudflare_account_id"] = field
		}
	}
	if field, ok := runtimeCurrentAgentSettingsManagedEnvField(true, "MISTER_MORPH_LLM_BEDROCK_AWS_KEY"); ok {
		fields["bedrock_aws_key"] = field
	}
	if field, ok := runtimeCurrentAgentSettingsManagedEnvField(true, "MISTER_MORPH_LLM_BEDROCK_AWS_SECRET"); ok {
		fields["bedrock_aws_secret"] = field
	}
	if field, ok := runtimeCurrentAgentSettingsManagedEnvField(false, "MISTER_MORPH_LLM_BEDROCK_REGION"); ok {
		fields["bedrock_region"] = field
	}
	if field, ok := runtimeCurrentAgentSettingsManagedEnvField(false, "MISTER_MORPH_LLM_BEDROCK_MODEL_ARN"); ok {
		fields["bedrock_model_arn"] = field
	}
	if field, ok := runtimeCurrentAgentSettingsManagedEnvField(false, "MISTER_MORPH_LLM_REASONING_EFFORT"); ok {
		fields["reasoning_effort"] = field
	}
	if field, ok := runtimeCurrentAgentSettingsManagedEnvField(false, "MISTER_MORPH_LLM_TOOLS_EMULATION_MODE"); ok {
		fields["tools_emulation_mode"] = field
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func runtimeCurrentAgentSettingsModelEnvField(provider string) (runtimeAgentSettingsEnvManagedField, bool) {
	if strings.EqualFold(strings.TrimSpace(provider), "azure") {
		if field, ok := runtimeCurrentAgentSettingsManagedEnvField(false, "MISTER_MORPH_LLM_AZURE_DEPLOYMENT"); ok {
			return field, true
		}
	}
	return runtimeCurrentAgentSettingsManagedEnvField(false, "MISTER_MORPH_LLM_MODEL")
}

func runtimeCurrentAgentSettingsManagedEnvField(
	secret bool,
	names ...string,
) (runtimeAgentSettingsEnvManagedField, bool) {
	name, value, ok := runtimeFirstManagedEnv(names...)
	if !ok {
		return runtimeAgentSettingsEnvManagedField{}, false
	}
	field := runtimeAgentSettingsEnvManagedField{
		EnvName:  name,
		RawValue: "${" + name + "}",
	}
	if !secret {
		field.Value = strings.TrimSpace(value)
	}
	return field, true
}

func runtimeSanitizeAgentSettingsManagedLLMFields(
	fields *runtimeLLMConfigFieldsPayload,
	envManaged map[string]runtimeAgentSettingsEnvManagedField,
	effectiveProvider string,
) {
	if fields == nil {
		return
	}
	if field, ok := envManaged["provider"]; ok && strings.TrimSpace(field.Value) != "" {
		fields.Provider = strings.TrimSpace(field.Value)
	}
	if field, ok := envManaged["endpoint"]; ok && strings.TrimSpace(field.Value) != "" {
		fields.Endpoint = strings.TrimSpace(field.Value)
	}
	if field, ok := envManaged["model"]; ok && strings.TrimSpace(field.Value) != "" {
		fields.Model = strings.TrimSpace(field.Value)
	}
	if field, ok := envManaged["context_window_tokens"]; ok && strings.TrimSpace(field.Value) != "" {
		fields.ContextWindowTokens = strings.TrimSpace(field.Value)
	}
	if field, ok := envManaged["cloudflare_account_id"]; ok && strings.TrimSpace(field.Value) != "" {
		fields.CloudflareAccountID = strings.TrimSpace(field.Value)
	}
	if field, ok := envManaged["bedrock_region"]; ok && strings.TrimSpace(field.Value) != "" {
		fields.BedrockRegion = strings.TrimSpace(field.Value)
	}
	if field, ok := envManaged["bedrock_model_arn"]; ok && strings.TrimSpace(field.Value) != "" {
		fields.BedrockModelARN = strings.TrimSpace(field.Value)
	}
	if field, ok := envManaged["reasoning_effort"]; ok && strings.TrimSpace(field.Value) != "" {
		fields.ReasoningEffort = strings.TrimSpace(field.Value)
	}
	if field, ok := envManaged["tools_emulation_mode"]; ok && strings.TrimSpace(field.Value) != "" {
		fields.ToolsEmulationMode = strings.TrimSpace(field.Value)
	}
	if _, ok := envManaged["api_key"]; ok {
		fields.APIKey = ""
	}
	if _, ok := envManaged["bedrock_aws_key"]; ok {
		fields.BedrockAWSKey = ""
	}
	if _, ok := envManaged["bedrock_aws_secret"]; ok {
		fields.BedrockAWSSecret = ""
	}
	if _, ok := envManaged["cloudflare_api_token"]; ok {
		fields.CloudflareAPIToken = ""
		if strings.EqualFold(strings.TrimSpace(effectiveProvider), "cloudflare") {
			fields.APIKey = ""
		}
	}
}

func runtimeBuildAgentSkillsSettingsPayload(reader *viper.Viper) (runtimeSkillsSettingsPayload, error) {
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
		reader = viper.GetViper()
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
	reader *viper.Viper,
	req runtimeAgentSettingsTestRequest,
) (runtimeLLMSettingsPayload, error) {
	targetProfile := runtimeAgentSettingsTestTargetProfile(req)
	snapshot := runtimeResolveAgentSettingsTestSnapshotFromReader(reader, req, targetProfile)
	if targetProfile == "" || strings.EqualFold(targetProfile, llmutil.RouteProfileDefault) {
		return runtimeResolveAgentSettingsTestDefaultLLM(reader, snapshot)
	}
	return runtimeResolveAgentSettingsTestProfileLLM(reader, snapshot, targetProfile)
}

func runtimeResolveAgentSettingsTestSnapshotFromReader(
	reader *viper.Viper,
	req runtimeAgentSettingsTestRequest,
	targetProfile string,
) runtimeLLMSettingsPayload {
	current := runtimeSettingsFromReader(reader)
	includeProfiles := targetProfile != "" && !strings.EqualFold(targetProfile, llmutil.RouteProfileDefault)
	return agentsettings.ApplyLLMSettingsNonEmptyUpdate(current, req.LLM, includeProfiles)
}

func runtimeAgentSettingsTestTargetProfile(req runtimeAgentSettingsTestRequest) string {
	if req.TargetProfile == nil {
		return ""
	}
	return strings.TrimSpace(*req.TargetProfile)
}

func runtimeResolveAgentSettingsTestDefaultLLM(
	reader *viper.Viper,
	snapshot runtimeLLMSettingsPayload,
) (runtimeLLMSettingsPayload, error) {
	values, err := runtimeValuesFromAgentSettingsTestSnapshot(reader, snapshot, "")
	if err != nil {
		return runtimeLLMSettingsPayload{}, err
	}
	return runtimeLLMSettingsPayloadFromAgentSettingsTestRuntimeValues(values), nil
}

func runtimeResolveAgentSettingsTestProfileLLM(
	reader *viper.Viper,
	snapshot runtimeLLMSettingsPayload,
	targetProfile string,
) (runtimeLLMSettingsPayload, error) {
	values, err := runtimeValuesFromAgentSettingsTestSnapshot(reader, snapshot, targetProfile)
	if err != nil {
		return runtimeLLMSettingsPayload{}, err
	}
	values.Routes.MainLoop = llmutil.RoutePolicyConfig{Profile: strings.TrimSpace(targetProfile)}
	route, err := llmutil.ResolveRoute(values, llmutil.RoutePurposeMainLoop)
	if err != nil {
		return runtimeLLMSettingsPayload{}, err
	}
	return runtimeLLMSettingsPayloadFromAgentSettingsTestRuntimeValues(route.Values), nil
}

func runtimeLLMSettingsPayloadFromAgentSettingsTestRuntimeValues(
	values llmutil.RuntimeValues,
) runtimeLLMSettingsPayload {
	payload := agentsettings.SettingsPayloadFromRuntimeValues(values)
	payload.Profiles = nil
	payload.FallbackProfiles = nil
	return payload
}

func runtimeValuesFromAgentSettingsTestSnapshot(
	reader *viper.Viper,
	snapshot runtimeLLMSettingsPayload,
	targetProfile string,
) (llmutil.RuntimeValues, error) {
	values, err := runtimeValuesFromAgentSettingsTestLLM(reader, snapshot)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	targetProfile = strings.TrimSpace(targetProfile)
	if targetProfile == "" || strings.EqualFold(targetProfile, llmutil.RouteProfileDefault) {
		return values, nil
	}
	profile, ok := runtimeFindAgentSettingsTestProfile(snapshot.Profiles, targetProfile)
	if !ok {
		return llmutil.RuntimeValues{}, fmt.Errorf("missing profile %q", targetProfile)
	}
	cfg, err := runtimeProfileConfigFromAgentSettingsTestProfile(profile)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	values.Profiles = map[string]llmutil.ProfileConfig{
		targetProfile: cfg,
	}
	return values, nil
}

func runtimeValuesFromAgentSettingsTestLLM(
	reader *viper.Viper,
	snapshot runtimeLLMSettingsPayload,
) (llmutil.RuntimeValues, error) {
	base := llmutil.RuntimeValuesFromReader(reader)
	inferenceProvider, err := runtimeResolveAgentSettingsTestFieldValue(snapshot.InferenceProvider)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	provider, err := runtimeResolveAgentSettingsTestFieldValue(snapshot.Provider)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	endpoint, err := runtimeResolveAgentSettingsTestFieldValue(snapshot.Endpoint)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	apiKey, err := runtimeResolveAgentSettingsTestFieldValue(snapshot.APIKey)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	model, err := runtimeResolveAgentSettingsTestFieldValue(snapshot.Model)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	contextWindowTokens, err := runtimeResolveAgentSettingsTestFieldValue(snapshot.ContextWindowTokens)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	cloudflareAPIToken, err := runtimeResolveAgentSettingsTestFieldValue(snapshot.CloudflareAPIToken)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	cloudflareAccountID, err := runtimeResolveAgentSettingsTestFieldValue(snapshot.CloudflareAccountID)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	bedrockAWSKey, err := runtimeResolveAgentSettingsTestFieldValue(snapshot.BedrockAWSKey)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	bedrockAWSSecret, err := runtimeResolveAgentSettingsTestFieldValue(snapshot.BedrockAWSSecret)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	bedrockRegion, err := runtimeResolveAgentSettingsTestFieldValue(snapshot.BedrockRegion)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	bedrockModelARN, err := runtimeResolveAgentSettingsTestFieldValue(snapshot.BedrockModelARN)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	reasoningEffort, err := runtimeResolveAgentSettingsTestFieldValue(snapshot.ReasoningEffort)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	toolsEmulationMode, err := runtimeResolveAgentSettingsTestFieldValue(snapshot.ToolsEmulationMode)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	return llmutil.RuntimeValues{
		InferenceProvider:   strings.TrimSpace(inferenceProvider),
		Provider:            agentsettings.NormalizeAgentSettingsProvider(provider),
		Endpoint:            endpoint,
		APIKey:              apiKey,
		Model:               model,
		ContextWindowRaw:    contextWindowTokens,
		Headers:             base.Headers,
		CacheTTL:            base.CacheTTL,
		CacheKeyPrefix:      base.CacheKeyPrefix,
		RequestTimeoutRaw:   base.RequestTimeoutRaw,
		ToolsEmulationMode:  toolsEmulationMode,
		TemperatureRaw:      base.TemperatureRaw,
		ReasoningEffortRaw:  reasoningEffort,
		ReasoningBudgetRaw:  base.ReasoningBudgetRaw,
		PricingFile:         base.PricingFile,
		ConfigPath:          base.ConfigPath,
		FileStateDir:        base.FileStateDir,
		BedrockAWSKey:       bedrockAWSKey,
		BedrockAWSSecret:    bedrockAWSSecret,
		BedrockAWSRegion:    bedrockRegion,
		BedrockModelARN:     bedrockModelARN,
		CloudflareAPIToken:  cloudflareAPIToken,
		CloudflareAccountID: cloudflareAccountID,
	}, nil
}

func runtimeProfileConfigFromAgentSettingsTestProfile(
	profile runtimeLLMProfileSettingsPayload,
) (llmutil.ProfileConfig, error) {
	inferenceProvider, err := runtimeResolveAgentSettingsTestFieldValue(profile.InferenceProvider)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	provider, err := runtimeResolveAgentSettingsTestFieldValue(profile.Provider)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	endpoint, err := runtimeResolveAgentSettingsTestFieldValue(profile.Endpoint)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	apiKey, err := runtimeResolveAgentSettingsTestFieldValue(profile.APIKey)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	model, err := runtimeResolveAgentSettingsTestFieldValue(profile.Model)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	contextWindowTokens, err := runtimeResolveAgentSettingsTestFieldValue(profile.ContextWindowTokens)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	cloudflareAPIToken, err := runtimeResolveAgentSettingsTestFieldValue(profile.CloudflareAPIToken)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	cloudflareAccountID, err := runtimeResolveAgentSettingsTestFieldValue(profile.CloudflareAccountID)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	bedrockAWSKey, err := runtimeResolveAgentSettingsTestFieldValue(profile.BedrockAWSKey)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	bedrockAWSSecret, err := runtimeResolveAgentSettingsTestFieldValue(profile.BedrockAWSSecret)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	bedrockRegion, err := runtimeResolveAgentSettingsTestFieldValue(profile.BedrockRegion)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	bedrockModelARN, err := runtimeResolveAgentSettingsTestFieldValue(profile.BedrockModelARN)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	reasoningEffort, err := runtimeResolveAgentSettingsTestFieldValue(profile.ReasoningEffort)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	toolsEmulationMode, err := runtimeResolveAgentSettingsTestFieldValue(profile.ToolsEmulationMode)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	return llmutil.ProfileConfig{
		InferenceProvider:  strings.TrimSpace(inferenceProvider),
		Provider:           agentsettings.NormalizeAgentSettingsProviderForOverride(provider),
		Endpoint:           endpoint,
		APIKey:             apiKey,
		Model:              model,
		ContextWindowRaw:   contextWindowTokens,
		ToolsEmulationMode: toolsEmulationMode,
		ReasoningEffortRaw: reasoningEffort,
		Bedrock: struct {
			AWSKey          string `mapstructure:"aws_key" yaml:"aws_key"`
			AWSSecret       string `mapstructure:"aws_secret" yaml:"aws_secret"`
			AWSSessionToken string `mapstructure:"aws_session_token" yaml:"aws_session_token"`
			AWSProfile      string `mapstructure:"aws_profile" yaml:"aws_profile"`
			Region          string `mapstructure:"region" yaml:"region"`
			ModelARN        string `mapstructure:"model_arn" yaml:"model_arn"`
		}{
			AWSKey:    bedrockAWSKey,
			AWSSecret: bedrockAWSSecret,
			Region:    bedrockRegion,
			ModelARN:  bedrockModelARN,
		},
		Cloudflare: struct {
			AccountID string `mapstructure:"account_id" yaml:"account_id"`
			APIToken  string `mapstructure:"api_token" yaml:"api_token"`
		}{
			AccountID: cloudflareAccountID,
			APIToken:  cloudflareAPIToken,
		},
	}, nil
}

func runtimeResolveAgentSettingsTestFieldValue(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	matches := runtimeAgentSettingsEnvRefPattern.FindStringSubmatch(value)
	if len(matches) != 2 {
		return value, nil
	}
	envName := strings.TrimSpace(matches[1])
	if envName == "" {
		return "", fmt.Errorf("invalid env placeholder %q", value)
	}
	resolved, ok := os.LookupEnv(envName)
	if !ok {
		return "", fmt.Errorf("missing env %q", envName)
	}
	return strings.TrimSpace(resolved), nil
}

func runtimeFindAgentSettingsTestProfile(
	profiles []runtimeLLMProfileSettingsPayload,
	targetProfile string,
) (runtimeLLMProfileSettingsPayload, bool) {
	targetProfile = strings.TrimSpace(targetProfile)
	for _, profile := range profiles {
		if strings.TrimSpace(profile.Name) == targetProfile {
			return profile, true
		}
	}
	return runtimeLLMProfileSettingsPayload{}, false
}

func runtimeDefaultAgentSettingsConnectionTest(
	ctx context.Context,
	reader *viper.Viper,
	settings runtimeLLMSettingsPayload,
) (runtimeAgentSettingsTestResult, error) {
	values, err := runtimeValuesFromAgentSettingsTestLLM(reader, settings)
	if err != nil {
		return runtimeAgentSettingsTestResult{}, err
	}
	route, err := llmutil.ResolveRoute(values, llmutil.RoutePurposeMainLoop)
	if err != nil {
		return runtimeAgentSettingsTestResult{}, err
	}
	client, err := llmutil.ClientFromConfigWithValues(route.ClientConfig, route.Values)
	if err != nil {
		return runtimeAgentSettingsTestResult{}, err
	}
	return runtimeAgentSettingsTestResult{
		Provider: route.ClientConfig.Provider,
		APIBase:  strings.TrimSpace(route.ClientConfig.Endpoint),
		Model:    route.ClientConfig.Model,
		Benchmarks: llmbench.Run(ctx, client, llmbench.ProfileMetadata{
			Provider: route.ClientConfig.Provider,
			APIBase:  strings.TrimSpace(route.ClientConfig.Endpoint),
			Model:    route.ClientConfig.Model,
		}).Benchmarks,
	}, nil
}

func runtimeFetchOpenAICompatibleModels(ctx context.Context, endpoint string, apiKey string) ([]string, error) {
	modelsURL, err := runtimeNormalizeOpenAICompatibleModelsURL(endpoint)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	req.Header.Set("Accept", "application/json")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("model lookup failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("model lookup failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("model lookup failed: %s", msg)
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("invalid models response")
	}

	seen := make(map[string]struct{}, len(payload.Data))
	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, id)
	}
	sort.Strings(models)
	return models, nil
}

func runtimeNormalizeOpenAICompatibleModelsURL(endpoint string) (string, error) {
	base := strings.TrimSpace(endpoint)
	if base == "" {
		base = "https://api.openai.com"
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("invalid api base")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("invalid api base")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return "", fmt.Errorf("invalid api base")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	switch {
	case strings.HasSuffix(parsed.Path, "/models"):
	case strings.HasSuffix(parsed.Path, "/v1"):
		parsed.Path += "/models"
	default:
		parsed.Path += "/v1/models"
	}
	return parsed.String(), nil
}

func runtimeFirstManagedEnv(names ...string) (string, string, bool) {
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		value, ok := os.LookupEnv(name)
		if ok {
			return name, value, true
		}
	}
	return "", "", false
}

func runtimeAgentSettingsConfigPath(reader *viper.Viper) string {
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
