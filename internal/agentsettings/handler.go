package agentsettings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/secref"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
)

type LLMConfigFieldsUpdate struct {
	InferenceProvider   *string `json:"inference_provider,omitempty"`
	Provider            *string `json:"provider,omitempty"`
	Endpoint            *string `json:"endpoint,omitempty"`
	Model               *string `json:"model,omitempty"`
	ContextWindowTokens *string `json:"context_window_tokens,omitempty"`
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

type LLMProfileUpdate struct {
	OriginalName string `json:"original_name,omitempty"`
	LLMProfileSettingsPayload
	providedSecretFields map[string]bool
}

func (u *LLMProfileUpdate) UnmarshalJSON(data []byte) error {
	type profileUpdateAlias LLMProfileUpdate
	var decoded profileUpdateAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*u = LLMProfileUpdate(decoded)
	u.providedSecretFields = map[string]bool{}
	for _, name := range llmSecretFieldNames {
		_, u.providedSecretFields[name] = fields[name]
	}
	return nil
}

func (u *LLMProfileUpdate) secretFieldProvided(name, value string) bool {
	return u != nil && (u.providedSecretFields[name] || strings.TrimSpace(value) != "")
}

type LLMSettingsUpdate struct {
	LLMConfigFieldsUpdate
	Profiles         *[]LLMProfileSettingsPayload `json:"profiles,omitempty"`
	FallbackProfiles *[]string                    `json:"fallback_profiles,omitempty"`
	Profile          *LLMProfileUpdate            `json:"profile,omitempty"`
	DeleteProfile    *string                      `json:"delete_profile,omitempty"`
}

type ToolEnabledPayload struct {
	Enabled bool `json:"enabled"`
}

type ToolEnabledUpdate struct {
	Enabled *bool `json:"enabled,omitempty"`
}

type ToolsSettingsPayload struct {
	WriteFile    ToolEnabledPayload `json:"write_file"`
	Spawn        ToolEnabledPayload `json:"spawn"`
	Coder        ToolEnabledPayload `json:"coder"`
	ContactsSend ToolEnabledPayload `json:"contacts_send"`
	TodoUpdate   ToolEnabledPayload `json:"todo_update"`
	PlanCreate   ToolEnabledPayload `json:"plan_create"`
	URLFetch     ToolEnabledPayload `json:"url_fetch"`
	WebSearch    ToolEnabledPayload `json:"web_search"`
	Bash         ToolEnabledPayload `json:"bash"`
	PowerShell   ToolEnabledPayload `json:"powershell"`
}

type ToolsSettingsUpdate struct {
	WriteFile    *ToolEnabledUpdate `json:"write_file,omitempty"`
	Spawn        *ToolEnabledUpdate `json:"spawn,omitempty"`
	Coder        *ToolEnabledUpdate `json:"coder,omitempty"`
	ContactsSend *ToolEnabledUpdate `json:"contacts_send,omitempty"`
	TodoUpdate   *ToolEnabledUpdate `json:"todo_update,omitempty"`
	PlanCreate   *ToolEnabledUpdate `json:"plan_create,omitempty"`
	URLFetch     *ToolEnabledUpdate `json:"url_fetch,omitempty"`
	WebSearch    *ToolEnabledUpdate `json:"web_search,omitempty"`
	Bash         *ToolEnabledUpdate `json:"bash,omitempty"`
	PowerShell   *ToolEnabledUpdate `json:"powershell,omitempty"`
}

type SkillsSettingsPayload struct {
	Enabled   bool                         `json:"enabled"`
	Load      []string                     `json:"load"`
	Loaded    []skillsutil.SkillStatusItem `json:"loaded"`
	Available []skillsutil.SkillStatusItem `json:"available"`
}

type SkillsSettingsUpdate struct {
	Enabled *bool     `json:"enabled,omitempty"`
	Load    *[]string `json:"load,omitempty"`
}

type AgentSettingsUpdate struct {
	LLM    LLMSettingsUpdate     `json:"llm"`
	Skills *SkillsSettingsUpdate `json:"skills,omitempty"`
	Tools  *ToolsSettingsUpdate  `json:"tools,omitempty"`
}

type AgentSettingsView struct {
	OK             bool                  `json:"ok,omitempty"`
	LLM            LLMSettingsPayload    `json:"llm"`
	EnvManaged     EnvManagedPayload     `json:"env_managed,omitempty"`
	SecretFields   SecretFieldsPayload   `json:"secret_fields,omitempty"`
	Skills         SkillsSettingsPayload `json:"skills"`
	Tools          ToolsSettingsPayload  `json:"tools"`
	ConfigPath     string                `json:"config_path"`
	ConfigExists   bool                  `json:"config_exists"`
	ConfigValid    bool                  `json:"config_valid"`
	ConfigSource   string                `json:"config_source"`
	ReadOnly       bool                  `json:"read_only"`
	ReadOnlyReason string                `json:"read_only_reason,omitempty"`
}

type Owner interface {
	View(context.Context) (AgentSettingsView, error)
	Update(context.Context, AgentSettingsUpdate) (AgentSettingsView, error)
	CurrentReader() Reader
}

type RuntimeConfigSource interface {
	CurrentReader() Reader
	ConfigPath() string
	LoadCandidate() (Reader, error)
	ReplaceReader(Reader)
}

type HandlerOptions struct {
	Owner                 Owner
	FetchModels           func(context.Context, string, string) ([]string, error)
	ConnectionTest        func(context.Context, LLMSettingsPayload, Reader, ConnectionTestOptions) (ConnectionTestResult, error)
	ConnectionTestOptions ConnectionTestOptions
}

type Handler struct {
	owner                 Owner
	fetchModels           func(context.Context, string, string) ([]string, error)
	connectionTest        func(context.Context, LLMSettingsPayload, Reader, ConnectionTestOptions) (ConnectionTestResult, error)
	connectionTestOptions ConnectionTestOptions
}

func NewHandler(opts HandlerOptions) *Handler {
	fetchModels := opts.FetchModels
	if fetchModels == nil {
		fetchModels = FetchOpenAICompatibleModels
	}
	connectionTest := opts.ConnectionTest
	if connectionTest == nil {
		connectionTest = defaultHandlerConnectionTest
	}
	return &Handler{
		owner:                 opts.Owner,
		fetchModels:           fetchModels,
		connectionTest:        connectionTest,
		connectionTestOptions: opts.ConnectionTestOptions,
	}
}

func (h *Handler) Settings(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.owner == nil {
		writeSettingsError(w, http.StatusServiceUnavailable, "agent settings are unavailable")
		return
	}
	switch r.Method {
	case http.MethodGet:
		view, err := h.owner.View(r.Context())
		if err != nil {
			writeSettingsError(w, settingsErrorStatus(err), err.Error())
			return
		}
		writeSettingsJSON(w, http.StatusOK, view)
	case http.MethodPut:
		current, err := h.owner.View(r.Context())
		if err != nil {
			writeSettingsError(w, settingsErrorStatus(err), err.Error())
			return
		}
		if current.ReadOnly {
			reason := strings.TrimSpace(current.ReadOnlyReason)
			if reason == "" {
				reason = "agent settings are read-only"
			}
			writeSettingsJSON(w, http.StatusMethodNotAllowed, map[string]any{
				"ok":               false,
				"error":            reason,
				"read_only":        true,
				"read_only_reason": reason,
			})
			return
		}
		var update AgentSettingsUpdate
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&update); err != nil {
			writeSettingsError(w, http.StatusBadRequest, "invalid json")
			return
		}
		view, err := h.owner.Update(r.Context(), update)
		if err != nil {
			writeSettingsError(w, settingsErrorStatus(err), err.Error())
			return
		}
		view.OK = true
		writeSettingsJSON(w, http.StatusOK, view)
	default:
		w.Header().Set("Allow", "GET, PUT")
		writeSettingsError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func settingsErrorStatus(err error) int {
	if errors.Is(err, secref.ErrOSStoreUnavailable) || errors.Is(err, secref.ErrOSSecretNotFound) {
		return http.StatusServiceUnavailable
	}
	var statusErr interface{ HTTPStatus() int }
	if errors.As(err, &statusErr) {
		return statusErr.HTTPStatus()
	}
	return http.StatusInternalServerError
}

type ModelsRequest struct {
	InferenceProvider string `json:"inference_provider"`
	Provider          string `json:"provider"`
	Endpoint          string `json:"endpoint"`
	APIKey            string `json:"api_key"`
}

func (h *Handler) Models(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeSettingsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	reader, ok := h.currentReader(w)
	if !ok {
		return
	}
	var req ModelsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeSettingsError(w, http.StatusBadRequest, "invalid json")
		return
	}
	current, err := settingsFromRuntimeReader(reader)
	if err != nil {
		writeSettingsError(w, http.StatusInternalServerError, err.Error())
		return
	}
	values, err := EffectiveRuntimeValues(reader)
	if err != nil {
		writeSettingsError(w, http.StatusInternalServerError, err.Error())
		return
	}
	lookup, err := ResolveOpenAICompatibleModelLookup(
		current,
		ModelLookupRequest{
			InferenceProvider: req.InferenceProvider,
			Provider:          req.Provider,
			Endpoint:          req.Endpoint,
			APIKey:            req.APIKey,
			FileStateDir:      values.FileStateDir,
		},
		func(value string) (string, error) {
			return ResolveConnectionTestFieldValue(value, configutil.SecretRefSourceFromReader(reader))
		},
	)
	if err != nil {
		writeSettingsError(w, http.StatusBadRequest, err.Error())
		return
	}
	models, err := h.fetchModels(r.Context(), lookup.Endpoint, lookup.APIKey)
	if err != nil {
		writeSettingsError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeSettingsJSON(w, http.StatusOK, map[string]any{"items": models})
}

type ConnectionTestRequest struct {
	LLM           LLMSettingsPayload `json:"llm"`
	TargetProfile *string            `json:"target_profile,omitempty"`
}

func (h *Handler) Test(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeSettingsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	reader, ok := h.currentReader(w)
	if !ok {
		return
	}
	var req ConnectionTestRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeSettingsError(w, http.StatusBadRequest, "invalid json")
		return
	}
	settings, err := resolveAgentSettingsTestLLMFromReader(reader, agentSettingsTestRequest{
		LLM:           req.LLM,
		TargetProfile: req.TargetProfile,
	})
	if err != nil {
		writeSettingsError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.connectionTest(r.Context(), settings, reader, h.connectionTestOptions)
	if err != nil {
		writeSettingsError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeSettingsJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"provider":   result.Provider,
		"api_base":   result.APIBase,
		"model":      result.Model,
		"benchmarks": result.Benchmarks,
	})
}

func (h *Handler) currentReader(w http.ResponseWriter) (Reader, bool) {
	if h == nil || h.owner == nil {
		writeSettingsError(w, http.StatusServiceUnavailable, "agent settings are unavailable")
		return nil, false
	}
	reader := h.owner.CurrentReader()
	if reader == nil {
		writeSettingsError(w, http.StatusServiceUnavailable, "agent settings reader is unavailable")
		return nil, false
	}
	return reader, true
}

func defaultHandlerConnectionTest(ctx context.Context, settings LLMSettingsPayload, reader Reader, opts ConnectionTestOptions) (ConnectionTestResult, error) {
	values, err := ResolveConnectionTestValues(
		reader,
		settings,
		llmutil.RouteProfileDefault,
		configutil.SecretRefSourceFromReader(reader),
	)
	if err != nil {
		return ConnectionTestResult{}, err
	}
	return RunConnectionTest(ctx, values, opts)
}

func writeSettingsJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeSettingsError(w http.ResponseWriter, status int, message string) {
	writeSettingsJSON(w, status, map[string]any{
		"ok":    false,
		"error": strings.TrimSpace(message),
	})
}
