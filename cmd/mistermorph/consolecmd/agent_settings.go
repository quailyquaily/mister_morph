package consolecmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/integration"
	"github.com/quailyquaily/mistermorph/internal/configbootstrap"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/fsstore"
	"github.com/quailyquaily/mistermorph/internal/llmbench"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const (
	llmSettingsKey        = "llm"
	multimodalSettingsKey = "multimodal"
	toolsSettingsKey      = "tools"
)

var supportedMultimodalSources = []string{"telegram", "slack", "line", "remote_download"}

var agentSettingsEnvRefPattern = regexp.MustCompile(`^\$\{([a-zA-Z_][a-zA-Z0-9_]*)\}$`)

type llmConfigFieldsPayload struct {
	Provider            string `json:"provider"`
	Endpoint            string `json:"endpoint"`
	Model               string `json:"model"`
	APIKey              string `json:"api_key"`
	BedrockAWSKey       string `json:"bedrock_aws_key"`
	BedrockAWSSecret    string `json:"bedrock_aws_secret"`
	BedrockRegion       string `json:"bedrock_region"`
	BedrockModelARN     string `json:"bedrock_model_arn"`
	CloudflareAPIToken  string `json:"cloudflare_api_token"`
	CloudflareAccountID string `json:"cloudflare_account_id"`
	ReasoningEffort     string `json:"reasoning_effort"`
	ToolsEmulationMode  string `json:"tools_emulation_mode"`
}

type llmProfileSettingsPayload struct {
	Name string `json:"name"`
	llmConfigFieldsPayload
}

type llmSettingsPayload struct {
	llmConfigFieldsPayload
	Profiles         []llmProfileSettingsPayload `json:"profiles,omitempty"`
	FallbackProfiles []string                    `json:"fallback_profiles,omitempty"`
}

type llmConfigFieldsUpdatePayload struct {
	Provider            *string `json:"provider,omitempty"`
	Endpoint            *string `json:"endpoint,omitempty"`
	Model               *string `json:"model,omitempty"`
	APIKey              *string `json:"api_key,omitempty"`
	BedrockAWSKey       *string `json:"bedrock_aws_key,omitempty"`
	BedrockAWSSecret    *string `json:"bedrock_aws_secret,omitempty"`
	BedrockRegion       *string `json:"bedrock_region,omitempty"`
	BedrockModelARN     *string `json:"bedrock_model_arn,omitempty"`
	CloudflareAPIToken  *string `json:"cloudflare_api_token,omitempty"`
	CloudflareAccountID *string `json:"cloudflare_account_id,omitempty"`
	ReasoningEffort     *string `json:"reasoning_effort,omitempty"`
	ToolsEmulationMode  *string `json:"tools_emulation_mode,omitempty"`
}

type llmSettingsUpdatePayload struct {
	llmConfigFieldsUpdatePayload
	Profiles         *[]llmProfileSettingsPayload `json:"profiles,omitempty"`
	FallbackProfiles *[]string                    `json:"fallback_profiles,omitempty"`
}

type multimodalSettingsPayload struct {
	ImageSources []string `json:"image_sources"`
}

type multimodalSettingsUpdatePayload struct {
	ImageSources *[]string `json:"image_sources,omitempty"`
}

type toolEnabledPayload struct {
	Enabled bool `json:"enabled"`
}

type toolEnabledUpdatePayload struct {
	Enabled *bool `json:"enabled,omitempty"`
}

type toolsSettingsPayload struct {
	WriteFile    toolEnabledPayload `json:"write_file"`
	Spawn        toolEnabledPayload `json:"spawn"`
	ContactsSend toolEnabledPayload `json:"contacts_send"`
	TodoUpdate   toolEnabledPayload `json:"todo_update"`
	PlanCreate   toolEnabledPayload `json:"plan_create"`
	URLFetch     toolEnabledPayload `json:"url_fetch"`
	WebSearch    toolEnabledPayload `json:"web_search"`
	Bash         toolEnabledPayload `json:"bash"`
	PowerShell   toolEnabledPayload `json:"powershell"`
}

type toolsSettingsUpdatePayload struct {
	WriteFile    *toolEnabledUpdatePayload `json:"write_file,omitempty"`
	Spawn        *toolEnabledUpdatePayload `json:"spawn,omitempty"`
	ContactsSend *toolEnabledUpdatePayload `json:"contacts_send,omitempty"`
	TodoUpdate   *toolEnabledUpdatePayload `json:"todo_update,omitempty"`
	PlanCreate   *toolEnabledUpdatePayload `json:"plan_create,omitempty"`
	URLFetch     *toolEnabledUpdatePayload `json:"url_fetch,omitempty"`
	WebSearch    *toolEnabledUpdatePayload `json:"web_search,omitempty"`
	Bash         *toolEnabledUpdatePayload `json:"bash,omitempty"`
	PowerShell   *toolEnabledUpdatePayload `json:"powershell,omitempty"`
}

type agentSettingsPayload struct {
	LLM        llmSettingsPayload        `json:"llm"`
	Multimodal multimodalSettingsPayload `json:"multimodal"`
	Tools      toolsSettingsPayload      `json:"tools"`
}

type agentSettingsUpdatePayload struct {
	LLM        llmSettingsUpdatePayload         `json:"llm"`
	Multimodal *multimodalSettingsUpdatePayload `json:"multimodal,omitempty"`
	Tools      *toolsSettingsUpdatePayload      `json:"tools,omitempty"`
}

type agentSettingsEnvManagedField struct {
	EnvName  string `json:"env_name"`
	Value    string `json:"value,omitempty"`
	RawValue string `json:"raw_value,omitempty"`
}

type agentSettingsEnvManagedPayload struct {
	LLM         map[string]agentSettingsEnvManagedField            `json:"llm,omitempty"`
	LLMProfiles map[string]map[string]agentSettingsEnvManagedField `json:"llm_profiles,omitempty"`
}

type agentSettingsModelsRequest struct {
	Endpoint string `json:"endpoint"`
	APIKey   string `json:"api_key"`
}

type agentSettingsTestRequest struct {
	LLM           llmSettingsPayload `json:"llm"`
	TargetProfile *string            `json:"target_profile,omitempty"`
}

type agentSettingsBenchmarkResult = llmbench.BenchmarkResult

type agentSettingsTestResult struct {
	Provider   string
	APIBase    string
	Model      string
	Benchmarks []agentSettingsBenchmarkResult
}

type agentSettingsConnectionTestOptions struct {
	InspectPrompt  bool
	InspectRequest bool
}

var runAgentSettingsConnectionTest = defaultAgentSettingsConnectionTest

func (s *server) handleAgentSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAgentSettingsGet(w, r)
	case http.MethodPut:
		s.handleAgentSettingsPut(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *server) handleAgentSettingsGet(w http.ResponseWriter, _ *http.Request) {
	configPath, err := resolveConsoleConfigPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	configExists, configSource, err := inspectAgentSettingsConfigSource(configPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	configValid := true
	settings, err := readAgentSettings(configPath)
	if err != nil {
		if !isInvalidConfigYAMLError(err) {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		settings = defaultAgentSettingsPayload()
		configSource = "defaults"
		configValid = false
	}
	effectiveLLM := s.settingsFromCurrentRuntime()
	doc := configbootstrap.NewEmptyDocument()
	if configValid {
		doc, err = loadYAMLDocument(configPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	settings, envManaged := buildAgentSettingsResponseView(settings, doc, effectiveLLM.Provider)
	writeJSON(w, http.StatusOK, map[string]any{
		"llm":           settings.LLM,
		"env_managed":   envManaged,
		"multimodal":    settings.Multimodal,
		"tools":         settings.Tools,
		"config_path":   configPath,
		"config_exists": configExists,
		"config_valid":  configValid,
		"config_source": configSource,
	})
}

func (s *server) handleAgentSettingsPut(w http.ResponseWriter, r *http.Request) {
	var req agentSettingsUpdatePayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	configPath, err := resolveConsoleConfigPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	serialized, err := writeAgentSettingsUpdate(configPath, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	effectiveLLM := resolveAgentSettingsLLMFromReader(s.currentRuntimeConfigReader(), req.LLM)
	if _, err := validateAgentConfigDocument(serialized, effectiveLLM); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := fsstore.WriteTextAtomic(configPath, string(serialized), fsstore.FileOptions{DirPerm: 0o755, FilePerm: 0o600}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	expanded, err := readExpandedAgentSettingsConfig(configPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	next := readAgentSettingsFromReader(expanded)
	doc, docErr := configbootstrap.LoadDocumentBytes(serialized)
	if docErr != nil {
		writeError(w, http.StatusInternalServerError, docErr.Error())
		return
	}
	next, envManaged := buildAgentSettingsResponseView(next, doc, s.settingsFromCurrentRuntime().Provider)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"llm":           next.LLM,
		"env_managed":   envManaged,
		"multimodal":    next.Multimodal,
		"tools":         next.Tools,
		"config_path":   configPath,
		"config_exists": true,
		"config_valid":  true,
		"config_source": "config",
	})
}

func (s *server) handleAgentSettingsModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req agentSettingsModelsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	current := s.settingsFromCurrentRuntime()
	endpoint := strings.TrimSpace(req.Endpoint)
	if endpoint == "" {
		endpoint = strings.TrimSpace(current.Endpoint)
	}
	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(current.APIKey)
	}
	if apiKey == "" {
		writeError(w, http.StatusBadRequest, "api key is required")
		return
	}
	models, err := fetchOpenAICompatibleModels(r.Context(), endpoint, apiKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": models,
	})
}

func (s *server) handleAgentSettingsTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req agentSettingsTestRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	settings, err := resolveAgentSettingsTestLLMFromReader(s.currentRuntimeConfigReader(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := runAgentSettingsConnectionTest(
		r.Context(),
		settings,
		agentSettingsConnectionTestOptions{
			InspectPrompt:  s != nil && s.cfg.inspectPrompt,
			InspectRequest: s != nil && s.cfg.inspectRequest,
		},
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"provider":   result.Provider,
		"api_base":   result.APIBase,
		"model":      result.Model,
		"benchmarks": result.Benchmarks,
	})
}

func resolveConsoleConfigPath() (string, error) {
	explicit := strings.TrimSpace(viper.GetString("config"))
	if explicit != "" {
		return pathutil.ExpandHomePath(explicit), nil
	}
	for _, candidate := range []string{"config.yaml", "~/.morph/config.yaml"} {
		resolved := pathutil.ExpandHomePath(candidate)
		if _, err := os.Stat(resolved); err == nil {
			return resolved, nil
		}
	}
	return filepath.Clean(pathutil.ExpandHomePath("config.yaml")), nil
}

func readAgentSettings(configPath string) (agentSettingsPayload, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultAgentSettingsPayload(), nil
		}
		return agentSettingsPayload{}, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return defaultAgentSettingsPayload(), nil
	}
	tmp, err := readExpandedAgentSettingsConfig(configPath)
	if err != nil {
		return agentSettingsPayload{}, fmt.Errorf("invalid config yaml: %w", err)
	}
	settings := readAgentSettingsFromReader(tmp)
	doc, err := loadYAMLDocument(configPath)
	if err != nil {
		return agentSettingsPayload{}, err
	}
	return normalizeAgentSettingsConfigView(settings, doc), nil
}

func readExpandedAgentSettingsConfig(configPath string) (*viper.Viper, error) {
	tmp := viper.New()
	integration.ApplyViperDefaults(tmp)
	if err := readExpandedConsoleConfig(tmp, configPath); err != nil {
		return nil, err
	}
	return tmp, nil
}

func defaultAgentSettingsPayload() agentSettingsPayload {
	tmp := viper.New()
	integration.ApplyViperDefaults(tmp)
	settings := readAgentSettingsFromReader(tmp)
	settings.LLM.Endpoint = ""
	settings.LLM.Model = ""
	return settings
}

func inspectAgentSettingsConfigSource(configPath string) (bool, string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, "defaults", nil
		}
		return false, "", err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return true, "defaults", nil
	}
	return true, "config", nil
}

func writeAgentSettings(configPath string, values agentSettingsPayload) ([]byte, error) {
	return writeAgentSettingsUpdate(configPath, agentSettingsUpdatePayload{
		LLM: llmSettingsPayloadAsUpdate(values.LLM),
		Multimodal: &multimodalSettingsUpdatePayload{
			ImageSources: stringSlicePointer(values.Multimodal.ImageSources),
		},
		Tools: &toolsSettingsUpdatePayload{
			WriteFile:    toolEnabledUpdatePayloadPointer(values.Tools.WriteFile.Enabled),
			Spawn:        toolEnabledUpdatePayloadPointer(values.Tools.Spawn.Enabled),
			ContactsSend: toolEnabledUpdatePayloadPointer(values.Tools.ContactsSend.Enabled),
			TodoUpdate:   toolEnabledUpdatePayloadPointer(values.Tools.TodoUpdate.Enabled),
			PlanCreate:   toolEnabledUpdatePayloadPointer(values.Tools.PlanCreate.Enabled),
			URLFetch:     toolEnabledUpdatePayloadPointer(values.Tools.URLFetch.Enabled),
			WebSearch:    toolEnabledUpdatePayloadPointer(values.Tools.WebSearch.Enabled),
			Bash:         toolEnabledUpdatePayloadPointer(values.Tools.Bash.Enabled),
			PowerShell:   toolEnabledUpdatePayloadPointer(values.Tools.PowerShell.Enabled),
		},
	})
}

func writeAgentSettingsUpdate(configPath string, values agentSettingsUpdatePayload) ([]byte, error) {
	doc, err := loadYAMLDocument(configPath)
	if err != nil {
		if !isInvalidConfigYAMLError(err) {
			return nil, err
		}
		doc = configbootstrap.NewEmptyDocument()
	}
	current := defaultAgentSettingsPayload()
	if existing, readErr := readAgentSettings(configPath); readErr == nil {
		current = existing
	} else if !isInvalidConfigYAMLError(readErr) && !os.IsNotExist(readErr) {
		return nil, readErr
	}
	if err := applyAgentSettingsUpdateDocument(doc, current, values); err != nil {
		return nil, err
	}
	return configbootstrap.MarshalDocument(doc)
}

func applyAgentSettingsUpdateDocument(doc *yaml.Node, current agentSettingsPayload, values agentSettingsUpdatePayload) error {
	nextLLM := applyLLMSettingsUpdate(current.LLM, values.LLM)
	root, err := configbootstrap.DocumentMapping(doc)
	if err != nil {
		return err
	}

	llmNode := configbootstrap.EnsureMappingValue(root, llmSettingsKey)
	applyLLMConfigFieldsUpdate(llmNode, nextLLM.llmConfigFieldsPayload, values.LLM.llmConfigFieldsUpdatePayload)
	if values.LLM.Profiles != nil {
		profiles, err := normalizeLLMProfileSettings(*values.LLM.Profiles)
		if err != nil {
			return err
		}
		if err := setLLMProfilesNode(llmNode, profiles, nextLLM.Provider); err != nil {
			return err
		}
	}
	if values.LLM.FallbackProfiles != nil {
		setMainLoopFallbackProfilesNode(llmNode, *values.LLM.FallbackProfiles)
	}

	if values.Multimodal != nil && values.Multimodal.ImageSources != nil {
		multimodalNode := configbootstrap.EnsureMappingValue(root, multimodalSettingsKey)
		imageNode := configbootstrap.EnsureMappingValue(multimodalNode, "image")
		setMappingStringList(imageNode, "sources", *values.Multimodal.ImageSources)
	}

	if values.Tools != nil {
		toolsNode := configbootstrap.EnsureMappingValue(root, toolsSettingsKey)
		if enabled := toolEnabledUpdateValue(values.Tools.WriteFile); enabled != nil {
			configbootstrap.SetMappingBoolPath(toolsNode, "write_file", "enabled", *enabled)
		}
		if enabled := toolEnabledUpdateValue(values.Tools.Spawn); enabled != nil {
			configbootstrap.SetMappingBoolPath(toolsNode, "spawn", "enabled", *enabled)
		}
		if enabled := toolEnabledUpdateValue(values.Tools.ContactsSend); enabled != nil {
			configbootstrap.SetMappingBoolPath(toolsNode, "contacts_send", "enabled", *enabled)
		}
		if enabled := toolEnabledUpdateValue(values.Tools.TodoUpdate); enabled != nil {
			configbootstrap.SetMappingBoolPath(toolsNode, "todo_update", "enabled", *enabled)
		}
		if enabled := toolEnabledUpdateValue(values.Tools.PlanCreate); enabled != nil {
			configbootstrap.SetMappingBoolPath(toolsNode, "plan_create", "enabled", *enabled)
		}
		if enabled := toolEnabledUpdateValue(values.Tools.URLFetch); enabled != nil {
			configbootstrap.SetMappingBoolPath(toolsNode, "url_fetch", "enabled", *enabled)
		}
		if enabled := toolEnabledUpdateValue(values.Tools.WebSearch); enabled != nil {
			configbootstrap.SetMappingBoolPath(toolsNode, "web_search", "enabled", *enabled)
		}
		if enabled := toolEnabledUpdateValue(values.Tools.Bash); enabled != nil {
			configbootstrap.SetMappingBoolPath(toolsNode, "bash", "enabled", *enabled)
		}
		if enabled := toolEnabledUpdateValue(values.Tools.PowerShell); enabled != nil {
			configbootstrap.SetMappingBoolPath(toolsNode, "powershell", "enabled", *enabled)
		}
	}
	return nil
}

func validateAgentConfigDocument(data []byte, effectiveLLM llmSettingsPayload) (*viper.Viper, error) {
	tmp := viper.New()
	integration.ApplyViperDefaults(tmp)
	tmp.SetConfigType("yaml")
	if err := tmp.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("invalid config yaml: %w", err)
	}
	values := llmutil.RuntimeValuesFromReader(tmp)
	values.Provider = firstNonEmpty(strings.TrimSpace(effectiveLLM.Provider), values.Provider)
	values.Endpoint = firstNonEmpty(strings.TrimSpace(effectiveLLM.Endpoint), values.Endpoint)
	values.APIKey = firstNonEmpty(strings.TrimSpace(effectiveLLM.APIKey), values.APIKey)
	values.Model = firstNonEmpty(strings.TrimSpace(effectiveLLM.Model), values.Model)
	values.BedrockAWSKey = firstNonEmpty(strings.TrimSpace(effectiveLLM.BedrockAWSKey), values.BedrockAWSKey)
	values.BedrockAWSSecret = firstNonEmpty(strings.TrimSpace(effectiveLLM.BedrockAWSSecret), values.BedrockAWSSecret)
	values.BedrockAWSRegion = firstNonEmpty(strings.TrimSpace(effectiveLLM.BedrockRegion), values.BedrockAWSRegion)
	values.BedrockModelARN = firstNonEmpty(strings.TrimSpace(effectiveLLM.BedrockModelARN), values.BedrockModelARN)
	values.CloudflareAPIToken = firstNonEmpty(strings.TrimSpace(effectiveLLM.CloudflareAPIToken), values.CloudflareAPIToken)
	values.CloudflareAccountID = firstNonEmpty(strings.TrimSpace(effectiveLLM.CloudflareAccountID), values.CloudflareAccountID)
	values.ReasoningEffortRaw = firstNonEmpty(strings.TrimSpace(effectiveLLM.ReasoningEffort), values.ReasoningEffortRaw)
	values.ToolsEmulationMode = firstNonEmpty(strings.TrimSpace(effectiveLLM.ToolsEmulationMode), values.ToolsEmulationMode)
	if err := validateAgentLLMRoute(values, llmutil.RoutePurposeMainLoop); err != nil {
		return nil, err
	}
	for _, profile := range effectiveLLM.Profiles {
		name := strings.TrimSpace(profile.Name)
		if name == "" {
			continue
		}
		profileValues := values
		profileValues.Routes.MainLoop = llmutil.RoutePolicyConfig{Profile: name}
		if err := validateAgentLLMRoute(profileValues, llmutil.RoutePurposeMainLoop); err != nil {
			return nil, err
		}
	}
	return tmp, nil
}

func (s *server) settingsFromCurrentRuntime() llmSettingsPayload {
	return settingsFromRuntimeReader(s.currentRuntimeConfigReader())
}

func settingsFromRuntimeReader(reader *viper.Viper) llmSettingsPayload {
	return llmSettingsPayloadFromRuntimeValues(currentConsoleLLMRuntimeValuesFromReader(reader))
}

func resolveAgentSettingsLLMFromReader(reader *viper.Viper, overrides llmSettingsUpdatePayload) llmSettingsPayload {
	return applyLLMSettingsUpdate(settingsFromRuntimeReader(reader), overrides)
}

func resolveAgentSettingsTestLLMFromReader(reader *viper.Viper, req agentSettingsTestRequest) (llmSettingsPayload, error) {
	targetProfile := agentSettingsTestTargetProfile(req)
	snapshot := resolveAgentSettingsTestSnapshotFromReader(reader, req, targetProfile)
	if targetProfile == "" || strings.EqualFold(targetProfile, llmutil.RouteProfileDefault) {
		return resolveAgentSettingsTestDefaultLLM(snapshot)
	}
	return resolveAgentSettingsTestProfileLLM(snapshot, targetProfile)
}

func resolveAgentSettingsTestSnapshotFromReader(reader *viper.Viper, req agentSettingsTestRequest, targetProfile string) llmSettingsPayload {
	if targetProfile != "" && !strings.EqualFold(targetProfile, llmutil.RouteProfileDefault) {
		return resolveAgentSettingsLLMFromReader(reader, llmSettingsPayloadAsProfileTestUpdate(req.LLM))
	}
	return resolveAgentSettingsLLMFromReader(reader, llmSettingsPayloadAsNonEmptyUpdate(req.LLM))
}

func agentSettingsTestTargetProfile(req agentSettingsTestRequest) string {
	if req.TargetProfile == nil {
		return ""
	}
	return strings.TrimSpace(*req.TargetProfile)
}

func resolveAgentSettingsTestDefaultLLM(snapshot llmSettingsPayload) (llmSettingsPayload, error) {
	values, err := runtimeValuesFromAgentSettingsTestSnapshot(snapshot, "")
	if err != nil {
		return llmSettingsPayload{}, err
	}
	return llmSettingsPayloadFromAgentSettingsTestRuntimeValues(values), nil
}

func resolveAgentSettingsTestProfileLLM(snapshot llmSettingsPayload, targetProfile string) (llmSettingsPayload, error) {
	values, err := runtimeValuesFromAgentSettingsTestSnapshot(snapshot, targetProfile)
	if err != nil {
		return llmSettingsPayload{}, err
	}
	values.Routes.MainLoop = llmutil.RoutePolicyConfig{Profile: strings.TrimSpace(targetProfile)}
	route, err := llmutil.ResolveRoute(values, llmutil.RoutePurposeMainLoop)
	if err != nil {
		return llmSettingsPayload{}, err
	}
	return llmSettingsPayloadFromAgentSettingsTestRuntimeValues(route.Values), nil
}

func llmSettingsPayloadFromAgentSettingsTestRuntimeValues(values llmutil.RuntimeValues) llmSettingsPayload {
	payload := llmSettingsPayloadFromRuntimeValues(values)
	payload.Profiles = nil
	payload.FallbackProfiles = nil
	return payload
}

func runtimeValuesFromAgentSettingsTestSnapshot(
	snapshot llmSettingsPayload,
	targetProfile string,
) (llmutil.RuntimeValues, error) {
	values, err := runtimeValuesFromAgentSettingsTestLLM(snapshot)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	targetProfile = strings.TrimSpace(targetProfile)
	if targetProfile == "" || strings.EqualFold(targetProfile, llmutil.RouteProfileDefault) {
		return values, nil
	}
	profile, ok := findAgentSettingsTestProfile(snapshot.Profiles, targetProfile)
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

func runtimeValuesFromAgentSettingsTestLLM(snapshot llmSettingsPayload) (llmutil.RuntimeValues, error) {
	provider, err := resolveAgentSettingsTestFieldValue(snapshot.Provider)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	endpoint, err := resolveAgentSettingsTestFieldValue(snapshot.Endpoint)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	apiKey, err := resolveAgentSettingsTestFieldValue(snapshot.APIKey)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	model, err := resolveAgentSettingsTestFieldValue(snapshot.Model)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	cloudflareAPIToken, err := resolveAgentSettingsTestFieldValue(snapshot.CloudflareAPIToken)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	cloudflareAccountID, err := resolveAgentSettingsTestFieldValue(snapshot.CloudflareAccountID)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	bedrockAWSKey, err := resolveAgentSettingsTestFieldValue(snapshot.BedrockAWSKey)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	bedrockAWSSecret, err := resolveAgentSettingsTestFieldValue(snapshot.BedrockAWSSecret)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	bedrockRegion, err := resolveAgentSettingsTestFieldValue(snapshot.BedrockRegion)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	bedrockModelARN, err := resolveAgentSettingsTestFieldValue(snapshot.BedrockModelARN)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	reasoningEffort, err := resolveAgentSettingsTestFieldValue(snapshot.ReasoningEffort)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	toolsEmulationMode, err := resolveAgentSettingsTestFieldValue(snapshot.ToolsEmulationMode)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	return llmutil.RuntimeValues{
		Provider:            normalizeAgentSettingsProvider(provider),
		Endpoint:            endpoint,
		APIKey:              apiKey,
		Model:               model,
		RequestTimeoutRaw:   "20s",
		FileStateDir:        strings.TrimSpace(viper.GetString("file_state_dir")),
		ReasoningEffortRaw:  reasoningEffort,
		ToolsEmulationMode:  toolsEmulationMode,
		BedrockAWSKey:       bedrockAWSKey,
		BedrockAWSSecret:    bedrockAWSSecret,
		BedrockAWSRegion:    bedrockRegion,
		BedrockModelARN:     bedrockModelARN,
		CloudflareAPIToken:  cloudflareAPIToken,
		CloudflareAccountID: cloudflareAccountID,
	}, nil
}

func runtimeProfileConfigFromAgentSettingsTestProfile(profile llmProfileSettingsPayload) (llmutil.ProfileConfig, error) {
	provider, err := resolveAgentSettingsTestFieldValue(profile.Provider)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	endpoint, err := resolveAgentSettingsTestFieldValue(profile.Endpoint)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	apiKey, err := resolveAgentSettingsTestFieldValue(profile.APIKey)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	model, err := resolveAgentSettingsTestFieldValue(profile.Model)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	cloudflareAPIToken, err := resolveAgentSettingsTestFieldValue(profile.CloudflareAPIToken)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	cloudflareAccountID, err := resolveAgentSettingsTestFieldValue(profile.CloudflareAccountID)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	bedrockAWSKey, err := resolveAgentSettingsTestFieldValue(profile.BedrockAWSKey)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	bedrockAWSSecret, err := resolveAgentSettingsTestFieldValue(profile.BedrockAWSSecret)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	bedrockRegion, err := resolveAgentSettingsTestFieldValue(profile.BedrockRegion)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	bedrockModelARN, err := resolveAgentSettingsTestFieldValue(profile.BedrockModelARN)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	reasoningEffort, err := resolveAgentSettingsTestFieldValue(profile.ReasoningEffort)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	toolsEmulationMode, err := resolveAgentSettingsTestFieldValue(profile.ToolsEmulationMode)
	if err != nil {
		return llmutil.ProfileConfig{}, err
	}
	return llmutil.ProfileConfig{
		Provider:           normalizeAgentSettingsProviderForOverride(provider),
		Endpoint:           endpoint,
		APIKey:             apiKey,
		Model:              model,
		ToolsEmulationMode: toolsEmulationMode,
		ReasoningEffortRaw: reasoningEffort,
		Bedrock: struct {
			AWSKey          string `mapstructure:"aws_key"`
			AWSSecret       string `mapstructure:"aws_secret"`
			AWSSessionToken string `mapstructure:"aws_session_token"`
			AWSProfile      string `mapstructure:"aws_profile"`
			Region          string `mapstructure:"region"`
			ModelARN        string `mapstructure:"model_arn"`
		}{
			AWSKey:    bedrockAWSKey,
			AWSSecret: bedrockAWSSecret,
			Region:    bedrockRegion,
			ModelARN:  bedrockModelARN,
		},
		Cloudflare: struct {
			AccountID string `mapstructure:"account_id"`
			APIToken  string `mapstructure:"api_token"`
		}{
			AccountID: cloudflareAccountID,
			APIToken:  cloudflareAPIToken,
		},
	}, nil
}

func resolveAgentSettingsTestFieldValue(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	matches := agentSettingsEnvRefPattern.FindStringSubmatch(value)
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

func findAgentSettingsTestProfile(
	profiles []llmProfileSettingsPayload,
	targetProfile string,
) (llmProfileSettingsPayload, bool) {
	targetProfile = strings.TrimSpace(targetProfile)
	for _, profile := range profiles {
		if strings.TrimSpace(profile.Name) == targetProfile {
			return profile, true
		}
	}
	return llmProfileSettingsPayload{}, false
}

func currentConsoleLLMRuntimeValuesFromReader(reader *viper.Viper) llmutil.RuntimeValues {
	if reader == nil {
		reader = viper.GetViper()
	}
	values := llmutil.RuntimeValuesFromReader(reader)

	if _, value, ok := firstManagedEnv("MISTER_MORPH_LLM_PROVIDER"); ok {
		values.Provider = strings.TrimSpace(value)
	}
	if _, value, ok := firstManagedEnv("MISTER_MORPH_LLM_ENDPOINT"); ok {
		values.Endpoint = strings.TrimSpace(value)
	}
	if _, value, ok := firstManagedEnv("MISTER_MORPH_LLM_API_KEY"); ok {
		values.APIKey = strings.TrimSpace(value)
	}
	if _, value, ok := firstManagedEnv("MISTER_MORPH_LLM_MODEL"); ok {
		values.Model = strings.TrimSpace(value)
	}
	if _, value, ok := firstManagedEnv("MISTER_MORPH_LLM_AZURE_DEPLOYMENT"); ok {
		values.AzureDeployment = strings.TrimSpace(value)
	}
	if _, value, ok := firstManagedEnv("MISTER_MORPH_LLM_REASONING_EFFORT"); ok {
		values.ReasoningEffortRaw = strings.TrimSpace(value)
	}
	if _, value, ok := firstManagedEnv("MISTER_MORPH_LLM_TOOLS_EMULATION_MODE"); ok {
		values.ToolsEmulationMode = strings.TrimSpace(value)
	}
	if _, value, ok := firstManagedEnv("MISTER_MORPH_LLM_BEDROCK_AWS_KEY"); ok {
		values.BedrockAWSKey = strings.TrimSpace(value)
	}
	if _, value, ok := firstManagedEnv("MISTER_MORPH_LLM_BEDROCK_AWS_SECRET"); ok {
		values.BedrockAWSSecret = strings.TrimSpace(value)
	}
	if _, value, ok := firstManagedEnv("MISTER_MORPH_LLM_BEDROCK_REGION"); ok {
		values.BedrockAWSRegion = strings.TrimSpace(value)
	}
	if _, value, ok := firstManagedEnv("MISTER_MORPH_LLM_BEDROCK_MODEL_ARN"); ok {
		values.BedrockModelARN = strings.TrimSpace(value)
	}
	if _, value, ok := firstManagedEnv("MISTER_MORPH_LLM_CLOUDFLARE_ACCOUNT_ID"); ok {
		values.CloudflareAccountID = strings.TrimSpace(value)
	}
	if _, value, ok := firstManagedEnv("MISTER_MORPH_LLM_CLOUDFLARE_API_TOKEN"); ok {
		values.CloudflareAPIToken = strings.TrimSpace(value)
	}

	return values
}

func applyLLMSettingsUpdate(current llmSettingsPayload, incoming llmSettingsUpdatePayload) llmSettingsPayload {
	merged := current
	if incoming.Provider != nil {
		merged.Provider = strings.TrimSpace(*incoming.Provider)
	}
	if incoming.Endpoint != nil {
		merged.Endpoint = strings.TrimSpace(*incoming.Endpoint)
	}
	if incoming.Model != nil {
		merged.Model = strings.TrimSpace(*incoming.Model)
	}
	if incoming.APIKey != nil {
		merged.APIKey = strings.TrimSpace(*incoming.APIKey)
	}
	if incoming.BedrockAWSKey != nil {
		merged.BedrockAWSKey = strings.TrimSpace(*incoming.BedrockAWSKey)
	}
	if incoming.BedrockAWSSecret != nil {
		merged.BedrockAWSSecret = strings.TrimSpace(*incoming.BedrockAWSSecret)
	}
	if incoming.BedrockRegion != nil {
		merged.BedrockRegion = strings.TrimSpace(*incoming.BedrockRegion)
	}
	if incoming.BedrockModelARN != nil {
		merged.BedrockModelARN = strings.TrimSpace(*incoming.BedrockModelARN)
	}
	if incoming.CloudflareAPIToken != nil {
		merged.CloudflareAPIToken = strings.TrimSpace(*incoming.CloudflareAPIToken)
	}
	if incoming.CloudflareAccountID != nil {
		merged.CloudflareAccountID = strings.TrimSpace(*incoming.CloudflareAccountID)
	}
	if incoming.ReasoningEffort != nil {
		merged.ReasoningEffort = strings.TrimSpace(*incoming.ReasoningEffort)
	}
	if incoming.ToolsEmulationMode != nil {
		merged.ToolsEmulationMode = strings.TrimSpace(*incoming.ToolsEmulationMode)
	}
	if incoming.Profiles != nil {
		merged.Profiles = append([]llmProfileSettingsPayload(nil), (*incoming.Profiles)...)
	}
	if incoming.FallbackProfiles != nil {
		merged.FallbackProfiles = normalizeNamedProfileSequence(*incoming.FallbackProfiles)
	}
	switch strings.ToLower(strings.TrimSpace(merged.Provider)) {
	case "cloudflare":
		merged.APIKey = ""
		merged.BedrockAWSKey = ""
		merged.BedrockAWSSecret = ""
		merged.BedrockRegion = ""
		merged.BedrockModelARN = ""
	case "bedrock":
		merged.APIKey = ""
		merged.CloudflareAPIToken = ""
		merged.CloudflareAccountID = ""
	case "openai_codex":
		merged.Endpoint = ""
		merged.APIKey = ""
		merged.CloudflareAPIToken = ""
		merged.CloudflareAccountID = ""
		merged.BedrockAWSKey = ""
		merged.BedrockAWSSecret = ""
		merged.BedrockRegion = ""
		merged.BedrockModelARN = ""
	default:
		merged.CloudflareAPIToken = ""
		merged.CloudflareAccountID = ""
		merged.BedrockAWSKey = ""
		merged.BedrockAWSSecret = ""
		merged.BedrockRegion = ""
		merged.BedrockModelARN = ""
	}
	return merged
}

func llmSettingsPayloadAsUpdate(values llmSettingsPayload) llmSettingsUpdatePayload {
	return llmSettingsUpdatePayload{
		llmConfigFieldsUpdatePayload: llmConfigFieldsUpdatePayload{
			Provider:            stringPointer(values.Provider),
			Endpoint:            stringPointer(values.Endpoint),
			Model:               stringPointer(values.Model),
			APIKey:              stringPointer(values.APIKey),
			BedrockAWSKey:       stringPointer(values.BedrockAWSKey),
			BedrockAWSSecret:    stringPointer(values.BedrockAWSSecret),
			BedrockRegion:       stringPointer(values.BedrockRegion),
			BedrockModelARN:     stringPointer(values.BedrockModelARN),
			CloudflareAPIToken:  stringPointer(values.CloudflareAPIToken),
			CloudflareAccountID: stringPointer(values.CloudflareAccountID),
			ReasoningEffort:     stringPointer(values.ReasoningEffort),
			ToolsEmulationMode:  stringPointer(values.ToolsEmulationMode),
		},
		Profiles:         profileSettingsPointer(values.Profiles),
		FallbackProfiles: stringSlicePointer(values.FallbackProfiles),
	}
}

func llmSettingsPayloadAsNonEmptyUpdate(values llmSettingsPayload) llmSettingsUpdatePayload {
	update := llmSettingsUpdatePayload{}
	if value := strings.TrimSpace(values.Provider); value != "" {
		update.Provider = stringPointer(value)
	}
	if value := strings.TrimSpace(values.Endpoint); value != "" {
		update.Endpoint = stringPointer(value)
	}
	if value := strings.TrimSpace(values.Model); value != "" {
		update.Model = stringPointer(value)
	}
	if value := strings.TrimSpace(values.APIKey); value != "" {
		update.APIKey = stringPointer(value)
	}
	if value := strings.TrimSpace(values.BedrockAWSKey); value != "" {
		update.BedrockAWSKey = stringPointer(value)
	}
	if value := strings.TrimSpace(values.BedrockAWSSecret); value != "" {
		update.BedrockAWSSecret = stringPointer(value)
	}
	if value := strings.TrimSpace(values.BedrockRegion); value != "" {
		update.BedrockRegion = stringPointer(value)
	}
	if value := strings.TrimSpace(values.BedrockModelARN); value != "" {
		update.BedrockModelARN = stringPointer(value)
	}
	if value := strings.TrimSpace(values.CloudflareAPIToken); value != "" {
		update.CloudflareAPIToken = stringPointer(value)
	}
	if value := strings.TrimSpace(values.CloudflareAccountID); value != "" {
		update.CloudflareAccountID = stringPointer(value)
	}
	if value := strings.TrimSpace(values.ReasoningEffort); value != "" {
		update.ReasoningEffort = stringPointer(value)
	}
	if value := strings.TrimSpace(values.ToolsEmulationMode); value != "" {
		update.ToolsEmulationMode = stringPointer(value)
	}
	return update
}

func llmSettingsPayloadAsProfileTestUpdate(values llmSettingsPayload) llmSettingsUpdatePayload {
	update := llmSettingsPayloadAsNonEmptyUpdate(values)
	if len(values.Profiles) > 0 {
		update.Profiles = profileSettingsPointer(values.Profiles)
	}
	if len(values.FallbackProfiles) > 0 {
		update.FallbackProfiles = stringSlicePointer(values.FallbackProfiles)
	}
	return update
}

func stringPointer(value string) *string {
	next := value
	return &next
}

func stringSlicePointer(values []string) *[]string {
	next := append([]string(nil), values...)
	return &next
}

func boolPointer(value bool) *bool {
	next := value
	return &next
}

func toolEnabledUpdatePayloadPointer(value bool) *toolEnabledUpdatePayload {
	return &toolEnabledUpdatePayload{Enabled: boolPointer(value)}
}

func toolEnabledUpdateValue(update *toolEnabledUpdatePayload) *bool {
	if update == nil {
		return nil
	}
	return update.Enabled
}

func profileSettingsPointer(values []llmProfileSettingsPayload) *[]llmProfileSettingsPayload {
	next := append([]llmProfileSettingsPayload(nil), values...)
	return &next
}

func validateAgentLLMRoute(values llmutil.RuntimeValues, purpose string) error {
	route, err := llmutil.ResolveRoute(values, purpose)
	if err != nil {
		return err
	}
	_, err = llmutil.BuildRouteClient(route, nil, llmutil.ClientFromConfigWithValues, nil, nil)
	return err
}

func llmSettingsPayloadFromRuntimeValues(values llmutil.RuntimeValues) llmSettingsPayload {
	provider := strings.TrimSpace(values.Provider)
	payload := llmSettingsPayload{
		llmConfigFieldsPayload: llmConfigFieldsPayload{
			Provider:            provider,
			Endpoint:            llmutil.EndpointForProviderWithValues(provider, values),
			Model:               llmutil.ModelForProviderWithValues(provider, values),
			APIKey:              resolvedAgentSettingsAPIKey(provider, strings.TrimSpace(values.APIKey)),
			BedrockAWSKey:       strings.TrimSpace(values.BedrockAWSKey),
			BedrockAWSSecret:    strings.TrimSpace(values.BedrockAWSSecret),
			BedrockRegion:       strings.TrimSpace(values.BedrockAWSRegion),
			BedrockModelARN:     strings.TrimSpace(values.BedrockModelARN),
			CloudflareAPIToken:  resolvedCloudflareToken(provider, strings.TrimSpace(values.APIKey), strings.TrimSpace(values.CloudflareAPIToken)),
			CloudflareAccountID: resolvedCloudflareAccountID(provider, strings.TrimSpace(values.CloudflareAccountID)),
			ReasoningEffort:     strings.TrimSpace(values.ReasoningEffortRaw),
			ToolsEmulationMode:  strings.TrimSpace(values.ToolsEmulationMode),
		},
		Profiles:         llmProfileSettingsPayloadsFromMap(values.Profiles, provider),
		FallbackProfiles: normalizeNamedProfileSequence(values.Routes.MainLoop.FallbackProfiles),
	}
	payload.llmConfigFieldsPayload = sanitizeProviderSpecificLLMFields(payload.llmConfigFieldsPayload, provider)
	return payload
}

func llmProfileSettingsPayloadsFromMap(profiles map[string]llmutil.ProfileConfig, defaultProvider string) []llmProfileSettingsPayload {
	if len(profiles) == 0 {
		return nil
	}
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			names = append(names, trimmed)
		}
	}
	sort.Strings(names)
	out := make([]llmProfileSettingsPayload, 0, len(names))
	for _, name := range names {
		out = append(out, llmProfileSettingsPayloadFromConfig(name, profiles[name], defaultProvider))
	}
	return out
}

func llmProfileSettingsPayloadFromConfig(name string, cfg llmutil.ProfileConfig, defaultProvider string) llmProfileSettingsPayload {
	effectiveProvider := firstNonEmpty(strings.TrimSpace(cfg.Provider), defaultProvider)
	payload := llmProfileSettingsPayload{
		Name: strings.TrimSpace(name),
		llmConfigFieldsPayload: llmConfigFieldsPayload{
			Provider:            strings.TrimSpace(cfg.Provider),
			Endpoint:            strings.TrimSpace(cfg.Endpoint),
			Model:               strings.TrimSpace(cfg.Model),
			APIKey:              resolvedAgentSettingsAPIKey(effectiveProvider, strings.TrimSpace(cfg.APIKey)),
			BedrockAWSKey:       strings.TrimSpace(cfg.Bedrock.AWSKey),
			BedrockAWSSecret:    strings.TrimSpace(cfg.Bedrock.AWSSecret),
			BedrockRegion:       strings.TrimSpace(cfg.Bedrock.Region),
			BedrockModelARN:     strings.TrimSpace(cfg.Bedrock.ModelARN),
			CloudflareAPIToken:  resolvedCloudflareToken(effectiveProvider, strings.TrimSpace(cfg.APIKey), strings.TrimSpace(cfg.Cloudflare.APIToken)),
			CloudflareAccountID: resolvedCloudflareAccountID(effectiveProvider, strings.TrimSpace(cfg.Cloudflare.AccountID)),
			ReasoningEffort:     strings.TrimSpace(cfg.ReasoningEffortRaw),
			ToolsEmulationMode:  strings.TrimSpace(cfg.ToolsEmulationMode),
		},
	}
	payload.llmConfigFieldsPayload = sanitizeProviderSpecificLLMFields(payload.llmConfigFieldsPayload, effectiveProvider)
	return payload
}

func sanitizeProviderSpecificLLMFields(fields llmConfigFieldsPayload, effectiveProvider string) llmConfigFieldsPayload {
	switch strings.ToLower(strings.TrimSpace(effectiveProvider)) {
	case "cloudflare":
		fields.APIKey = ""
		fields.BedrockAWSKey = ""
		fields.BedrockAWSSecret = ""
		fields.BedrockRegion = ""
		fields.BedrockModelARN = ""
	case "bedrock":
		fields.APIKey = ""
		fields.CloudflareAPIToken = ""
		fields.CloudflareAccountID = ""
	case "openai_codex":
		fields.Endpoint = ""
		fields.APIKey = ""
		fields.BedrockAWSKey = ""
		fields.BedrockAWSSecret = ""
		fields.BedrockRegion = ""
		fields.BedrockModelARN = ""
		fields.CloudflareAPIToken = ""
		fields.CloudflareAccountID = ""
	default:
		fields.BedrockAWSKey = ""
		fields.BedrockAWSSecret = ""
		fields.BedrockRegion = ""
		fields.BedrockModelARN = ""
		fields.CloudflareAPIToken = ""
		fields.CloudflareAccountID = ""
	}
	return fields
}

func resolvedAgentSettingsAPIKey(provider, apiKey string) string {
	if strings.EqualFold(strings.TrimSpace(provider), "openai_codex") {
		return ""
	}
	return strings.TrimSpace(apiKey)
}

func resolvedCloudflareToken(provider, apiKey, apiToken string) string {
	if strings.EqualFold(strings.TrimSpace(provider), "cloudflare") {
		return firstNonEmpty(apiToken, apiKey)
	}
	if strings.EqualFold(strings.TrimSpace(provider), "openai_codex") {
		return ""
	}
	return strings.TrimSpace(apiToken)
}

func resolvedCloudflareAccountID(provider, accountID string) string {
	if strings.EqualFold(strings.TrimSpace(provider), "openai_codex") {
		return ""
	}
	return strings.TrimSpace(accountID)
}

func normalizeLLMProfileSettings(profiles []llmProfileSettingsPayload) ([]llmProfileSettingsPayload, error) {
	if len(profiles) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(profiles))
	out := make([]llmProfileSettingsPayload, 0, len(profiles))
	for _, profile := range profiles {
		name := strings.TrimSpace(profile.Name)
		if name == "" {
			return nil, fmt.Errorf("profile name is required")
		}
		if strings.EqualFold(name, llmutil.RouteProfileDefault) {
			return nil, fmt.Errorf("profile name %q is reserved", name)
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate profile %q", name)
		}
		seen[key] = struct{}{}
		normalized := llmProfileSettingsPayload{
			Name: name,
			llmConfigFieldsPayload: llmConfigFieldsPayload{
				Provider:            strings.TrimSpace(profile.Provider),
				Endpoint:            strings.TrimSpace(profile.Endpoint),
				Model:               strings.TrimSpace(profile.Model),
				APIKey:              strings.TrimSpace(profile.APIKey),
				BedrockAWSKey:       strings.TrimSpace(profile.BedrockAWSKey),
				BedrockAWSSecret:    strings.TrimSpace(profile.BedrockAWSSecret),
				BedrockRegion:       strings.TrimSpace(profile.BedrockRegion),
				BedrockModelARN:     strings.TrimSpace(profile.BedrockModelARN),
				CloudflareAPIToken:  strings.TrimSpace(profile.CloudflareAPIToken),
				CloudflareAccountID: strings.TrimSpace(profile.CloudflareAccountID),
				ReasoningEffort:     strings.TrimSpace(profile.ReasoningEffort),
				ToolsEmulationMode:  strings.TrimSpace(profile.ToolsEmulationMode),
			},
		}
		switch {
		case strings.EqualFold(normalized.Provider, "cloudflare"):
			normalized.CloudflareAPIToken = firstNonEmpty(normalized.CloudflareAPIToken, normalized.APIKey)
			normalized.APIKey = ""
			normalized.BedrockAWSKey = ""
			normalized.BedrockAWSSecret = ""
			normalized.BedrockRegion = ""
			normalized.BedrockModelARN = ""
		case strings.EqualFold(normalized.Provider, "bedrock"):
			normalized.APIKey = ""
			normalized.CloudflareAPIToken = ""
			normalized.CloudflareAccountID = ""
		case strings.EqualFold(normalized.Provider, "openai_codex"):
			normalized.Endpoint = ""
			normalized.APIKey = ""
			normalized.CloudflareAPIToken = ""
			normalized.CloudflareAccountID = ""
			normalized.BedrockAWSKey = ""
			normalized.BedrockAWSSecret = ""
			normalized.BedrockRegion = ""
			normalized.BedrockModelARN = ""
		case normalized.Provider != "":
			normalized.CloudflareAPIToken = ""
			normalized.CloudflareAccountID = ""
			normalized.BedrockAWSKey = ""
			normalized.BedrockAWSSecret = ""
			normalized.BedrockRegion = ""
			normalized.BedrockModelARN = ""
		}
		out = append(out, normalized)
	}
	return out, nil
}

func normalizeNamedProfileSequence(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func llmProfileSettingsAsUpdate(profile llmProfileSettingsPayload) llmConfigFieldsUpdatePayload {
	return llmConfigFieldsUpdatePayload{
		Provider:            stringPointer(profile.Provider),
		Endpoint:            stringPointer(profile.Endpoint),
		Model:               stringPointer(profile.Model),
		APIKey:              stringPointer(profile.APIKey),
		BedrockAWSKey:       stringPointer(profile.BedrockAWSKey),
		BedrockAWSSecret:    stringPointer(profile.BedrockAWSSecret),
		BedrockRegion:       stringPointer(profile.BedrockRegion),
		BedrockModelARN:     stringPointer(profile.BedrockModelARN),
		CloudflareAPIToken:  stringPointer(profile.CloudflareAPIToken),
		CloudflareAccountID: stringPointer(profile.CloudflareAccountID),
		ReasoningEffort:     stringPointer(profile.ReasoningEffort),
		ToolsEmulationMode:  stringPointer(profile.ToolsEmulationMode),
	}
}

func applyLLMConfigFieldsUpdate(node *yaml.Node, effective llmConfigFieldsPayload, update llmConfigFieldsUpdatePayload) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	if update.Provider != nil {
		configbootstrap.SetOrDeleteMappingScalar(node, "provider", *update.Provider)
	}
	if update.Endpoint != nil {
		configbootstrap.SetOrDeleteMappingScalar(node, "endpoint", *update.Endpoint)
	}
	if update.Model != nil {
		configbootstrap.SetOrDeleteMappingScalar(node, "model", *update.Model)
	}
	if update.ReasoningEffort != nil {
		configbootstrap.SetOrDeleteMappingScalar(node, "reasoning_effort", *update.ReasoningEffort)
	}
	if update.ToolsEmulationMode != nil {
		configbootstrap.SetOrDeleteMappingScalar(node, "tools_emulation_mode", *update.ToolsEmulationMode)
	}
	switch strings.ToLower(strings.TrimSpace(effective.Provider)) {
	case "openai_codex":
		configbootstrap.SetOrDeleteMappingScalar(node, "endpoint", "")
		configbootstrap.SetOrDeleteMappingScalar(node, "api_key", "")
		configbootstrap.DeleteMappingKey(node, "cloudflare")
		configbootstrap.DeleteMappingKey(node, "bedrock")
		return
	case "cloudflare":
		configbootstrap.SetOrDeleteMappingScalar(node, "api_key", "")
		configbootstrap.DeleteMappingKey(node, "bedrock")
		cloudflareNode := configbootstrap.FindMappingValue(node, "cloudflare")
		if cloudflareNode != nil && cloudflareNode.Kind != yaml.MappingNode {
			cloudflareNode = configbootstrap.EnsureMappingValue(node, "cloudflare")
		}
		if update.CloudflareAccountID != nil || update.CloudflareAPIToken != nil {
			if cloudflareNode == nil {
				cloudflareNode = configbootstrap.EnsureMappingValue(node, "cloudflare")
			}
			if update.CloudflareAccountID != nil {
				configbootstrap.SetOrDeleteMappingScalar(cloudflareNode, "account_id", *update.CloudflareAccountID)
			}
			if update.CloudflareAPIToken != nil {
				configbootstrap.SetOrDeleteMappingScalar(cloudflareNode, "api_token", *update.CloudflareAPIToken)
			}
		}
		if cloudflareNode != nil && len(cloudflareNode.Content) == 0 {
			configbootstrap.DeleteMappingKey(node, "cloudflare")
		}
		return
	case "bedrock":
		configbootstrap.SetOrDeleteMappingScalar(node, "api_key", "")
		configbootstrap.DeleteMappingKey(node, "cloudflare")
		bedrockNode := configbootstrap.FindMappingValue(node, "bedrock")
		if bedrockNode != nil && bedrockNode.Kind != yaml.MappingNode {
			bedrockNode = configbootstrap.EnsureMappingValue(node, "bedrock")
		}
		if update.BedrockAWSKey != nil || update.BedrockAWSSecret != nil || update.BedrockRegion != nil || update.BedrockModelARN != nil {
			if bedrockNode == nil {
				bedrockNode = configbootstrap.EnsureMappingValue(node, "bedrock")
			}
			if update.BedrockAWSKey != nil {
				configbootstrap.SetOrDeleteMappingScalar(bedrockNode, "aws_key", *update.BedrockAWSKey)
			}
			if update.BedrockAWSSecret != nil {
				configbootstrap.SetOrDeleteMappingScalar(bedrockNode, "aws_secret", *update.BedrockAWSSecret)
			}
			if update.BedrockRegion != nil {
				configbootstrap.SetOrDeleteMappingScalar(bedrockNode, "region", *update.BedrockRegion)
			}
			if update.BedrockModelARN != nil {
				configbootstrap.SetOrDeleteMappingScalar(bedrockNode, "model_arn", *update.BedrockModelARN)
			}
		}
		if bedrockNode != nil && len(bedrockNode.Content) == 0 {
			configbootstrap.DeleteMappingKey(node, "bedrock")
		}
		return
	}
	if update.APIKey != nil {
		configbootstrap.SetOrDeleteMappingScalar(node, "api_key", *update.APIKey)
	}
	configbootstrap.DeleteMappingKey(node, "cloudflare")
	configbootstrap.DeleteMappingKey(node, "bedrock")
}

func setLLMProfilesNode(llmNode *yaml.Node, profiles []llmProfileSettingsPayload, defaultProvider string) error {
	if llmNode == nil || llmNode.Kind != yaml.MappingNode {
		return nil
	}
	if len(profiles) == 0 {
		configbootstrap.DeleteMappingKey(llmNode, "profiles")
		return nil
	}
	existingProfiles := configbootstrap.FindMappingValue(llmNode, "profiles")
	existingNodes := make(map[string]*yaml.Node, len(profiles))
	if existingProfiles != nil && existingProfiles.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(existingProfiles.Content); i += 2 {
			name := strings.TrimSpace(existingProfiles.Content[i].Value)
			if name == "" {
				continue
			}
			existingNodes[name] = existingProfiles.Content[i+1]
		}
	}
	profilesNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, profile := range profiles {
		name := strings.TrimSpace(profile.Name)
		if name == "" {
			return fmt.Errorf("profile name is required")
		}
		profileNode := existingNodes[name]
		if profileNode == nil || profileNode.Kind != yaml.MappingNode {
			profileNode = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		}
		effective := profile.llmConfigFieldsPayload
		effective.Provider = firstNonEmpty(profile.Provider, defaultProvider)
		applyLLMConfigFieldsUpdate(profileNode, effective, llmProfileSettingsAsUpdate(profile))
		profilesNode.Content = append(profilesNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name},
			profileNode,
		)
	}
	for i := 0; i+1 < len(llmNode.Content); i += 2 {
		if !strings.EqualFold(strings.TrimSpace(llmNode.Content[i].Value), "profiles") {
			continue
		}
		llmNode.Content[i+1] = profilesNode
		return nil
	}
	llmNode.Content = append(llmNode.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "profiles"},
		profilesNode,
	)
	return nil
}

func setMappingOrderedStringList(node *yaml.Node, key string, values []string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	values = normalizeNamedProfileSequence(values)
	if len(values) == 0 {
		configbootstrap.DeleteMappingKey(node, key)
		return
	}
	list := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, value := range values {
		list.Content = append(list.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if !strings.EqualFold(strings.TrimSpace(node.Content[i].Value), key) {
			continue
		}
		node.Content[i+1] = list
		return
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		list,
	)
}

func setMainLoopFallbackProfilesNode(llmNode *yaml.Node, values []string) {
	if llmNode == nil || llmNode.Kind != yaml.MappingNode {
		return
	}
	values = normalizeNamedProfileSequence(values)
	configbootstrap.DeleteMappingKey(llmNode, "fallback_profiles")

	routesNode := configbootstrap.FindMappingValue(llmNode, "routes")
	if len(values) == 0 {
		pruneMainLoopFallbackProfilesNode(llmNode, routesNode)
		return
	}
	if routesNode == nil || routesNode.Kind != yaml.MappingNode {
		routesNode = configbootstrap.EnsureMappingValue(llmNode, "routes")
	}
	mainLoopNode := ensureRoutePolicyMappingValue(routesNode, llmutil.RoutePurposeMainLoop)
	if mainLoopNode == nil {
		return
	}
	setMappingOrderedStringList(mainLoopNode, "fallback_profiles", values)
}

func pruneMainLoopFallbackProfilesNode(llmNode *yaml.Node, routesNode *yaml.Node) {
	if llmNode == nil || llmNode.Kind != yaml.MappingNode {
		return
	}
	if routesNode == nil || routesNode.Kind != yaml.MappingNode {
		return
	}
	mainLoopNode := configbootstrap.FindMappingValue(routesNode, llmutil.RoutePurposeMainLoop)
	if mainLoopNode == nil || mainLoopNode.Kind != yaml.MappingNode {
		return
	}
	configbootstrap.DeleteMappingKey(mainLoopNode, "fallback_profiles")
	if len(mainLoopNode.Content) == 0 {
		configbootstrap.DeleteMappingKey(routesNode, llmutil.RoutePurposeMainLoop)
	}
	if len(routesNode.Content) == 0 {
		configbootstrap.DeleteMappingKey(llmNode, "routes")
	}
}

func ensureRoutePolicyMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	if value := configbootstrap.FindMappingValue(node, key); value != nil {
		if value.Kind == yaml.MappingNode {
			return value
		}
		profile := strings.TrimSpace(value.Value)
		*value = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		if profile != "" {
			value.Content = append(value.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "profile"},
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: profile},
			)
		}
		return value
	}
	child := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		child,
	)
	return child
}

func mergeLLMSettingsMap(base map[string]any, values llmSettingsPayload) map[string]any {
	out := cloneStringAnyMap(base)
	mergeLLMConfigFieldsMap(out, values.llmConfigFieldsPayload, values.Provider)

	if len(values.Profiles) == 0 {
		delete(out, "profiles")
	} else {
		existingProfiles := mapValueAsStringAnyMap(out["profiles"])
		profiles := make(map[string]any, len(values.Profiles))
		for _, profile := range values.Profiles {
			name := strings.TrimSpace(profile.Name)
			if name == "" {
				continue
			}
			profileMap := cloneStringAnyMap(mapValueAsStringAnyMap(existingProfiles[name]))
			mergeLLMConfigFieldsMap(profileMap, profile.llmConfigFieldsPayload, firstNonEmpty(profile.Provider, values.Provider))
			profiles[name] = profileMap
		}
		out["profiles"] = profiles
	}

	mergeMainLoopFallbackProfilesMap(out, values.FallbackProfiles)
	return out
}

func mergeMainLoopFallbackProfilesMap(out map[string]any, values []string) {
	if out == nil {
		return
	}
	values = normalizeNamedProfileSequence(values)
	delete(out, "fallback_profiles")

	routes := cloneStringAnyMap(mapValueAsStringAnyMap(out["routes"]))
	if len(values) == 0 {
		policy, ok := routePolicyMapValue(routes[llmutil.RoutePurposeMainLoop])
		if ok {
			delete(policy, "fallback_profiles")
			if len(policy) == 0 {
				delete(routes, llmutil.RoutePurposeMainLoop)
			} else {
				routes[llmutil.RoutePurposeMainLoop] = policy
			}
		}
		if len(routes) == 0 {
			delete(out, "routes")
		} else {
			out["routes"] = routes
		}
		return
	}

	policy, _ := routePolicyMapValue(routes[llmutil.RoutePurposeMainLoop])
	if len(policy) == 0 {
		policy = map[string]any{}
	}
	policy["fallback_profiles"] = values
	routes[llmutil.RoutePurposeMainLoop] = policy
	out["routes"] = routes
}

func routePolicyMapValue(raw any) (map[string]any, bool) {
	switch value := raw.(type) {
	case nil:
		return nil, false
	case string:
		profile := strings.TrimSpace(value)
		if profile == "" {
			return map[string]any{}, true
		}
		return map[string]any{"profile": profile}, true
	case map[string]any:
		return cloneStringAnyMap(value), true
	case map[any]any:
		return cloneStringAnyMap(stringAnyMapFromAnyMap(value)), true
	default:
		return nil, false
	}
}

func stringAnyMapFromAnyMap(raw map[any]any) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		name, ok := key.(string)
		if !ok {
			continue
		}
		out[name] = value
	}
	return out
}

func mergeLLMConfigFieldsMap(dst map[string]any, fields llmConfigFieldsPayload, effectiveProvider string) {
	if dst == nil {
		return
	}
	setOrDeleteStringMapValue(dst, "provider", fields.Provider)
	setOrDeleteStringMapValue(dst, "endpoint", fields.Endpoint)
	setOrDeleteStringMapValue(dst, "model", fields.Model)
	setOrDeleteStringMapValue(dst, "reasoning_effort", fields.ReasoningEffort)
	setOrDeleteStringMapValue(dst, "tools_emulation_mode", fields.ToolsEmulationMode)
	switch strings.ToLower(strings.TrimSpace(effectiveProvider)) {
	case "openai_codex":
		delete(dst, "endpoint")
		delete(dst, "api_key")
		delete(dst, "cloudflare")
		delete(dst, "bedrock")
		return
	case "cloudflare":
		delete(dst, "api_key")
		delete(dst, "bedrock")
		cloudflare := cloneStringAnyMap(mapValueAsStringAnyMap(dst["cloudflare"]))
		setOrDeleteStringMapValue(cloudflare, "account_id", fields.CloudflareAccountID)
		setOrDeleteStringMapValue(cloudflare, "api_token", firstNonEmpty(fields.CloudflareAPIToken, fields.APIKey))
		if len(cloudflare) == 0 {
			delete(dst, "cloudflare")
		} else {
			dst["cloudflare"] = cloudflare
		}
		return
	case "bedrock":
		delete(dst, "api_key")
		delete(dst, "cloudflare")
		bedrock := cloneStringAnyMap(mapValueAsStringAnyMap(dst["bedrock"]))
		setOrDeleteStringMapValue(bedrock, "aws_key", fields.BedrockAWSKey)
		setOrDeleteStringMapValue(bedrock, "aws_secret", fields.BedrockAWSSecret)
		setOrDeleteStringMapValue(bedrock, "region", fields.BedrockRegion)
		setOrDeleteStringMapValue(bedrock, "model_arn", fields.BedrockModelARN)
		if len(bedrock) == 0 {
			delete(dst, "bedrock")
		} else {
			dst["bedrock"] = bedrock
		}
		return
	}
	delete(dst, "cloudflare")
	delete(dst, "bedrock")
	setOrDeleteStringMapValue(dst, "api_key", fields.APIKey)
}

func cloneStringAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func mapValueAsStringAnyMap(value any) map[string]any {
	out, ok := value.(map[string]any)
	if !ok || len(out) == 0 {
		return nil
	}
	return out
}

func setOrDeleteStringMapValue(dst map[string]any, key, value string) {
	if dst == nil {
		return
	}
	if value = strings.TrimSpace(value); value == "" {
		delete(dst, key)
		return
	}
	dst[key] = value
}

func defaultAgentSettingsConnectionTest(ctx context.Context, settings llmSettingsPayload, opts agentSettingsConnectionTestOptions) (agentSettingsTestResult, error) {
	values := llmutil.RuntimeValues{
		Provider:            normalizeAgentSettingsProvider(settings.Provider),
		Endpoint:            strings.TrimSpace(settings.Endpoint),
		APIKey:              strings.TrimSpace(settings.APIKey),
		Model:               strings.TrimSpace(settings.Model),
		RequestTimeoutRaw:   "20s",
		FileStateDir:        strings.TrimSpace(viper.GetString("file_state_dir")),
		ReasoningEffortRaw:  strings.TrimSpace(settings.ReasoningEffort),
		ToolsEmulationMode:  strings.TrimSpace(settings.ToolsEmulationMode),
		BedrockAWSKey:       strings.TrimSpace(settings.BedrockAWSKey),
		BedrockAWSSecret:    strings.TrimSpace(settings.BedrockAWSSecret),
		BedrockAWSRegion:    strings.TrimSpace(settings.BedrockRegion),
		BedrockModelARN:     strings.TrimSpace(settings.BedrockModelARN),
		CloudflareAPIToken:  strings.TrimSpace(settings.CloudflareAPIToken),
		CloudflareAccountID: strings.TrimSpace(settings.CloudflareAccountID),
	}

	route, err := llmutil.ResolveRoute(values, llmutil.RoutePurposeMainLoop)
	if err != nil {
		return agentSettingsTestResult{}, err
	}
	client, err := llmutil.ClientFromConfigWithValues(route.ClientConfig, route.Values)
	if err != nil {
		return agentSettingsTestResult{}, err
	}
	inspectors, err := newConsoleInspectors(opts.InspectPrompt, opts.InspectRequest, "console_settings_test", "settings_test", "20060102_150405.000000000")
	if err != nil {
		return agentSettingsTestResult{}, err
	}
	defer func() {
		if inspectors != nil {
			_ = inspectors.Close()
		}
	}()
	client = inspectors.Wrap(client, route)

	return agentSettingsTestResult{
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

func runAgentSettingsTextBenchmark(ctx context.Context, client llm.Client, model string) agentSettingsBenchmarkResult {
	return llmbench.RunTextBenchmark(ctx, client, model)
}

func runAgentSettingsJSONBenchmark(ctx context.Context, client llm.Client, model string) agentSettingsBenchmarkResult {
	return llmbench.RunJSONBenchmark(ctx, client, model)
}

func runAgentSettingsToolCallingBenchmark(ctx context.Context, client llm.Client, model string) agentSettingsBenchmarkResult {
	return llmbench.RunToolCallingBenchmark(ctx, client, model)
}

func benchmarkRawResponse(result llm.Result) string {
	return llmbench.RawResponse(result)
}

func benchmarkRawResponseFromError(err error) string {
	return llmbench.RawResponseFromError(err)
}

func summarizeBenchmarkDetail(value string) string {
	return llmbench.SummarizeBenchmarkDetail(value)
}

func normalizeAgentSettingsProvider(provider string) string {
	value := strings.ToLower(strings.TrimSpace(provider))
	switch value {
	case "", "openai_compatible":
		return "openai"
	default:
		return value
	}
}

func normalizeAgentSettingsProviderForOverride(provider string) string {
	value := strings.ToLower(strings.TrimSpace(provider))
	switch value {
	case "":
		return ""
	case "openai_compatible":
		return "openai"
	default:
		return value
	}
}

func fetchOpenAICompatibleModels(ctx context.Context, endpoint string, apiKey string) ([]string, error) {
	modelsURL, err := normalizeOpenAICompatibleModelsURL(endpoint)
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

func normalizeOpenAICompatibleModelsURL(endpoint string) (string, error) {
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

func loadYAMLDocument(configPath string) (*yaml.Node, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return configbootstrap.NewEmptyDocument(), nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return configbootstrap.NewEmptyDocument(), nil
	}
	return configbootstrap.LoadDocumentBytes(data)
}

func readExpandedConsoleConfig(v *viper.Viper, configPath string) error {
	if v == nil {
		return fmt.Errorf("config reader is nil")
	}
	return configutil.ReadExpandedConfig(v, configPath, nil)
}

func isInvalidConfigYAMLError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "invalid config yaml")
}

func setMappingStringList(node *yaml.Node, key string, values []string) {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(strings.ToLower(raw))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	configbootstrap.SetMappingStringList(node, key, normalized)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeAgentSettingsConfigView(settings agentSettingsPayload, doc *yaml.Node) agentSettingsPayload {
	if !agentSettingsYAMLHasLLMKey(doc, "endpoint") {
		settings.LLM.Endpoint = ""
	}
	if !agentSettingsYAMLHasLLMKey(doc, "model") {
		settings.LLM.Model = ""
	}
	settings.LLM.Profiles = sortAgentSettingsProfilesByYAMLOrder(settings.LLM.Profiles, doc)
	return settings
}

func buildAgentSettingsResponseView(
	settings agentSettingsPayload,
	doc *yaml.Node,
	runtimeProvider string,
) (agentSettingsPayload, agentSettingsEnvManagedPayload) {
	settings = normalizeAgentSettingsConfigView(settings, doc)
	envManaged := currentAgentSettingsEnvManaged(runtimeProvider)
	llmNode := agentSettingsYAMLLLMNode(doc)
	defaultProvider := strings.TrimSpace(settings.LLM.Provider)
	if field, ok := envManaged.LLM["provider"]; ok && strings.TrimSpace(field.Value) != "" {
		defaultProvider = strings.TrimSpace(field.Value)
	}
	envManaged.LLM = applyAgentSettingsYAMLEnvManaged(
		&settings.LLM.llmConfigFieldsPayload,
		envManaged.LLM,
		llmNode,
		defaultProvider,
	)
	settings.LLM.Profiles, envManaged.LLMProfiles = buildAgentSettingsProfileResponseView(
		settings.LLM.Profiles,
		llmNode,
		defaultProvider,
	)
	if len(envManaged.LLM) == 0 {
		envManaged.LLM = nil
	}
	if len(envManaged.LLMProfiles) == 0 {
		envManaged.LLMProfiles = nil
	}
	return settings, envManaged
}

func buildAgentSettingsProfileResponseView(
	profiles []llmProfileSettingsPayload,
	llmNode *yaml.Node,
	defaultProvider string,
) ([]llmProfileSettingsPayload, map[string]map[string]agentSettingsEnvManagedField) {
	if len(profiles) == 0 {
		return profiles, nil
	}
	profilesNode := configbootstrap.FindMappingValue(llmNode, "profiles")
	out := append([]llmProfileSettingsPayload(nil), profiles...)
	envManaged := map[string]map[string]agentSettingsEnvManagedField{}
	for i := range out {
		name := strings.TrimSpace(out[i].Name)
		if name == "" {
			continue
		}
		profileNode := configbootstrap.FindMappingValue(profilesNode, name)
		profileProvider := firstNonEmpty(strings.TrimSpace(out[i].Provider), defaultProvider)
		fields := applyAgentSettingsYAMLEnvManaged(
			&out[i].llmConfigFieldsPayload,
			nil,
			profileNode,
			profileProvider,
		)
		if len(fields) == 0 {
			continue
		}
		envManaged[name] = fields
	}
	if len(envManaged) == 0 {
		return out, nil
	}
	return out, envManaged
}

func applyAgentSettingsYAMLEnvManaged(
	fields *llmConfigFieldsPayload,
	envManaged map[string]agentSettingsEnvManagedField,
	node *yaml.Node,
	defaultProvider string,
) map[string]agentSettingsEnvManagedField {
	if fields == nil {
		return envManaged
	}
	if _, ok := envManaged["provider"]; !ok {
		if field, ok := agentSettingsYAMLManagedField(node, defaultProvider, "provider"); ok {
			if envManaged == nil {
				envManaged = map[string]agentSettingsEnvManagedField{}
			}
			envManaged["provider"] = field
		}
	}
	effectiveProvider := firstNonEmpty(strings.TrimSpace(fields.Provider), defaultProvider)
	if field, ok := envManaged["provider"]; ok && strings.TrimSpace(field.Value) != "" {
		effectiveProvider = strings.TrimSpace(field.Value)
	}
	for _, fieldName := range []string{
		"endpoint",
		"model",
		"api_key",
		"bedrock_aws_key",
		"bedrock_aws_secret",
		"bedrock_region",
		"bedrock_model_arn",
		"cloudflare_api_token",
		"cloudflare_account_id",
		"reasoning_effort",
		"tools_emulation_mode",
	} {
		if _, ok := envManaged[fieldName]; ok {
			continue
		}
		field, ok := agentSettingsYAMLManagedField(node, effectiveProvider, fieldName)
		if !ok {
			continue
		}
		if envManaged == nil {
			envManaged = map[string]agentSettingsEnvManagedField{}
		}
		envManaged[fieldName] = field
	}
	sanitizeAgentSettingsManagedLLMFields(fields, envManaged, effectiveProvider)
	if len(envManaged) == 0 {
		return nil
	}
	return envManaged
}

func sanitizeAgentSettingsManagedLLMFields(
	fields *llmConfigFieldsPayload,
	envManaged map[string]agentSettingsEnvManagedField,
	effectiveProvider string,
) {
	if fields == nil {
		return
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

func agentSettingsYAMLManagedField(
	node *yaml.Node,
	provider string,
	field string,
) (agentSettingsEnvManagedField, bool) {
	fieldPathSets := [][]string{}
	switch strings.TrimSpace(field) {
	case "provider":
		fieldPathSets = [][]string{{"provider"}}
	case "endpoint":
		fieldPathSets = [][]string{{"endpoint"}}
	case "model":
		fieldPathSets = [][]string{{"model"}}
		if strings.EqualFold(strings.TrimSpace(provider), "azure") {
			fieldPathSets = append([][]string{{"azure", "deployment"}}, fieldPathSets...)
		}
	case "api_key":
		normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
		if normalizedProvider != "cloudflare" && normalizedProvider != "bedrock" && normalizedProvider != "openai_codex" {
			fieldPathSets = [][]string{{"api_key"}}
		}
	case "bedrock_aws_key":
		fieldPathSets = [][]string{{"bedrock", "aws_key"}}
	case "bedrock_aws_secret":
		fieldPathSets = [][]string{{"bedrock", "aws_secret"}}
	case "bedrock_region":
		fieldPathSets = [][]string{{"bedrock", "region"}}
	case "bedrock_model_arn":
		fieldPathSets = [][]string{{"bedrock", "model_arn"}}
	case "cloudflare_api_token":
		if !strings.EqualFold(strings.TrimSpace(provider), "openai_codex") {
			fieldPathSets = [][]string{{"cloudflare", "api_token"}}
		}
		if strings.EqualFold(strings.TrimSpace(provider), "cloudflare") {
			fieldPathSets = append(fieldPathSets, []string{"api_key"})
		}
	case "cloudflare_account_id":
		fieldPathSets = [][]string{{"cloudflare", "account_id"}}
	case "reasoning_effort":
		fieldPathSets = [][]string{{"reasoning_effort"}}
	case "tools_emulation_mode":
		fieldPathSets = [][]string{{"tools_emulation_mode"}}
	}
	for _, path := range fieldPathSets {
		current := node
		for _, key := range path {
			current = configbootstrap.FindMappingValue(current, key)
			if current == nil {
				break
			}
		}
		entry, ok := agentSettingsYAMLPlaceholderField(current, field)
		if ok {
			return entry, true
		}
	}
	return agentSettingsEnvManagedField{}, false
}

func agentSettingsYAMLPlaceholderField(
	node *yaml.Node,
	field string,
) (agentSettingsEnvManagedField, bool) {
	if node == nil || node.Kind != yaml.ScalarNode {
		return agentSettingsEnvManagedField{}, false
	}
	value := strings.TrimSpace(node.Value)
	matches := agentSettingsEnvRefPattern.FindStringSubmatch(value)
	if len(matches) != 2 {
		return agentSettingsEnvManagedField{}, false
	}
	envName := strings.TrimSpace(matches[1])
	if envName == "" {
		return agentSettingsEnvManagedField{}, false
	}
	out := agentSettingsEnvManagedField{EnvName: envName}
	switch strings.TrimSpace(field) {
	case "api_key", "bedrock_aws_key", "bedrock_aws_secret", "cloudflare_api_token":
	default:
		if resolved, ok := os.LookupEnv(envName); ok {
			out.Value = strings.TrimSpace(resolved)
		}
	}
	out.RawValue = value
	return out, true
}

func agentSettingsYAMLLLMNode(doc *yaml.Node) *yaml.Node {
	root, err := configbootstrap.DocumentMapping(doc)
	if err != nil {
		return nil
	}
	return configbootstrap.FindMappingValue(root, llmSettingsKey)
}

func sortAgentSettingsProfilesByYAMLOrder(profiles []llmProfileSettingsPayload, doc *yaml.Node) []llmProfileSettingsPayload {
	if len(profiles) <= 1 {
		return profiles
	}
	order := agentSettingsYAMLProfileOrder(doc)
	if len(order) == 0 {
		return profiles
	}
	indexByName := make(map[string]int, len(order))
	for idx, name := range order {
		indexByName[name] = idx
	}
	out := append([]llmProfileSettingsPayload(nil), profiles...)
	sort.SliceStable(out, func(i, j int) bool {
		left := strings.TrimSpace(out[i].Name)
		right := strings.TrimSpace(out[j].Name)
		leftIndex, leftOK := indexByName[left]
		rightIndex, rightOK := indexByName[right]
		switch {
		case leftOK && rightOK:
			return leftIndex < rightIndex
		case leftOK:
			return true
		case rightOK:
			return false
		default:
			return left < right
		}
	})
	return out
}

func agentSettingsYAMLProfileOrder(doc *yaml.Node) []string {
	root, err := configbootstrap.DocumentMapping(doc)
	if err != nil {
		return nil
	}
	llmNode := configbootstrap.FindMappingValue(root, llmSettingsKey)
	if llmNode == nil || llmNode.Kind != yaml.MappingNode {
		return nil
	}
	profilesNode := configbootstrap.FindMappingValue(llmNode, "profiles")
	if profilesNode == nil || profilesNode.Kind != yaml.MappingNode {
		return nil
	}
	order := make([]string, 0, len(profilesNode.Content)/2)
	for i := 0; i+1 < len(profilesNode.Content); i += 2 {
		if name := strings.TrimSpace(profilesNode.Content[i].Value); name != "" {
			order = append(order, name)
		}
	}
	return order
}

func agentSettingsYAMLHasLLMKey(doc *yaml.Node, key string) bool {
	root, err := configbootstrap.DocumentMapping(doc)
	if err != nil {
		return false
	}
	llmNode := configbootstrap.FindMappingValue(root, llmSettingsKey)
	if llmNode == nil || llmNode.Kind != yaml.MappingNode {
		return false
	}
	return configbootstrap.FindMappingValue(llmNode, key) != nil
}

func readAgentSettingsFromReader(r interface {
	GetString(string) string
	GetStringSlice(string) []string
	GetBool(string) bool
}) agentSettingsPayload {
	if r == nil {
		return agentSettingsPayload{}
	}
	values := llmutil.RuntimeValuesFromReader(r)
	return agentSettingsPayload{
		LLM: llmSettingsPayloadFromRuntimeValues(values),
		Multimodal: multimodalSettingsPayload{
			ImageSources: sanitizeMultimodalSources(r.GetStringSlice("multimodal.image.sources")),
		},
		Tools: toolsSettingsPayload{
			WriteFile:    toolEnabledPayload{Enabled: r.GetBool("tools.write_file.enabled")},
			Spawn:        toolEnabledPayload{Enabled: r.GetBool("tools.spawn.enabled")},
			ContactsSend: toolEnabledPayload{Enabled: r.GetBool("tools.contacts_send.enabled")},
			TodoUpdate:   toolEnabledPayload{Enabled: r.GetBool("tools.todo_update.enabled")},
			PlanCreate:   toolEnabledPayload{Enabled: r.GetBool("tools.plan_create.enabled")},
			URLFetch:     toolEnabledPayload{Enabled: r.GetBool("tools.url_fetch.enabled")},
			WebSearch:    toolEnabledPayload{Enabled: r.GetBool("tools.web_search.enabled")},
			Bash:         toolEnabledPayload{Enabled: r.GetBool("tools.bash.enabled")},
			PowerShell:   toolEnabledPayload{Enabled: r.GetBool("tools.powershell.enabled")},
		},
	}
}

func currentAgentSettingsEnvManaged(provider string) agentSettingsEnvManagedPayload {
	return agentSettingsEnvManagedPayload{
		LLM: currentAgentSettingsLLMEnvManaged(provider),
	}
}

func currentAgentSettingsLLMEnvManaged(provider string) map[string]agentSettingsEnvManagedField {
	fields := map[string]agentSettingsEnvManagedField{}
	normalizedProvider := strings.TrimSpace(strings.ToLower(provider))
	isCodexProvider := normalizedProvider == "openai_codex"

	if field, ok := currentAgentSettingsManagedEnvField(false, "MISTER_MORPH_LLM_PROVIDER"); ok {
		fields["provider"] = field
	}
	if !isCodexProvider {
		if field, ok := currentAgentSettingsManagedEnvField(false, "MISTER_MORPH_LLM_ENDPOINT"); ok {
			fields["endpoint"] = field
		}
	}
	if field, ok := currentAgentSettingsModelEnvField(provider); ok {
		fields["model"] = field
	}
	switch normalizedProvider {
	case "cloudflare":
		if field, ok := currentAgentSettingsManagedEnvField(
			true,
			"MISTER_MORPH_LLM_CLOUDFLARE_API_TOKEN",
			"MISTER_MORPH_LLM_API_KEY",
		); ok {
			fields["cloudflare_api_token"] = field
		}
	case "bedrock":
	default:
		if !isCodexProvider {
			if field, ok := currentAgentSettingsManagedEnvField(true, "MISTER_MORPH_LLM_API_KEY"); ok {
				fields["api_key"] = field
			}
			if field, ok := currentAgentSettingsManagedEnvField(true, "MISTER_MORPH_LLM_CLOUDFLARE_API_TOKEN"); ok {
				fields["cloudflare_api_token"] = field
			}
		}
	}
	if !isCodexProvider {
		if field, ok := currentAgentSettingsManagedEnvField(false, "MISTER_MORPH_LLM_CLOUDFLARE_ACCOUNT_ID"); ok {
			fields["cloudflare_account_id"] = field
		}
	}
	if field, ok := currentAgentSettingsManagedEnvField(true, "MISTER_MORPH_LLM_BEDROCK_AWS_KEY"); ok {
		fields["bedrock_aws_key"] = field
	}
	if field, ok := currentAgentSettingsManagedEnvField(true, "MISTER_MORPH_LLM_BEDROCK_AWS_SECRET"); ok {
		fields["bedrock_aws_secret"] = field
	}
	if field, ok := currentAgentSettingsManagedEnvField(false, "MISTER_MORPH_LLM_BEDROCK_REGION"); ok {
		fields["bedrock_region"] = field
	}
	if field, ok := currentAgentSettingsManagedEnvField(false, "MISTER_MORPH_LLM_BEDROCK_MODEL_ARN"); ok {
		fields["bedrock_model_arn"] = field
	}
	if field, ok := currentAgentSettingsManagedEnvField(false, "MISTER_MORPH_LLM_REASONING_EFFORT"); ok {
		fields["reasoning_effort"] = field
	}
	if field, ok := currentAgentSettingsManagedEnvField(false, "MISTER_MORPH_LLM_TOOLS_EMULATION_MODE"); ok {
		fields["tools_emulation_mode"] = field
	}

	if len(fields) == 0 {
		return nil
	}
	return fields
}

func currentAgentSettingsModelEnvField(provider string) (agentSettingsEnvManagedField, bool) {
	if strings.EqualFold(strings.TrimSpace(provider), "azure") {
		return currentAgentSettingsManagedEnvField(
			false,
			"MISTER_MORPH_LLM_AZURE_DEPLOYMENT",
			"MISTER_MORPH_LLM_MODEL",
		)
	}
	return currentAgentSettingsManagedEnvField(false, "MISTER_MORPH_LLM_MODEL")
}

func currentAgentSettingsManagedEnvField(sensitive bool, names ...string) (agentSettingsEnvManagedField, bool) {
	name, value, ok := firstManagedEnv(names...)
	if !ok {
		return agentSettingsEnvManagedField{}, false
	}
	field := agentSettingsEnvManagedField{EnvName: name}
	if !sensitive {
		field.Value = strings.TrimSpace(value)
	}
	return field, true
}

func firstManagedEnvName(names ...string) (string, bool) {
	name, _, ok := firstManagedEnv(names...)
	return name, ok
}

func firstManagedEnv(names ...string) (string, string, bool) {
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if value, ok := os.LookupEnv(name); ok {
			return name, value, true
		}
	}
	return "", "", false
}

func sanitizeMultimodalSources(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(supportedMultimodalSources))
	for _, value := range supportedMultimodalSources {
		allowed[value] = struct{}{}
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(strings.ToLower(raw))
		if value == "" {
			continue
		}
		if _, ok := allowed[value]; !ok {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
