package agentsettings

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/quailyquaily/mistermorph/internal/configbootstrap"
	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/fsstore"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/secref"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/skills"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const (
	llmSettingsKey    = "llm"
	skillsSettingsKey = "skills"
	toolsSettingsKey  = "tools"
)

type FileSettings struct {
	LLM   LLMSettingsPayload
	Tools ToolsSettingsPayload
}

type agentSettingsTestRequest struct {
	LLM           LLMSettingsPayload
	TargetProfile *string
}

type FileOwnerOptions struct {
	ConfigPath string
	Reader     Reader
}

type FileOwner struct {
	mu         sync.RWMutex
	configPath string
	base       *ReaderSnapshot
	reader     *ReaderSnapshot
}

type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.Message)
}

func (e *StatusError) HTTPStatus() int {
	if e == nil || e.Status == 0 {
		return http.StatusInternalServerError
	}
	return e.Status
}

func NewFileOwner(opts FileOwnerOptions) *FileOwner {
	reader := NewReaderSnapshot(opts.Reader)
	return &FileOwner{
		configPath: resolveFileOwnerConfigPath(opts.ConfigPath, opts.Reader),
		base:       reader,
		reader:     reader,
	}
}

func resolveFileOwnerConfigPath(explicit string, reader Reader) string {
	configPath := strings.TrimSpace(explicit)
	if configPath == "" && reader != nil {
		configPath = strings.TrimSpace(reader.ConfigFileUsed())
		if configPath == "" {
			configPath = strings.TrimSpace(reader.GetString("config"))
		}
	}
	if configPath == "" {
		configPath = pathutil.DefaultConfigPath()
	}
	return pathutil.ExpandHomePath(configPath)
}

func (o *FileOwner) CurrentReader() Reader {
	if o == nil {
		return nil
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.reader
}

func (o *FileOwner) ConfigPath() string {
	if o == nil {
		return ""
	}
	return o.configPath
}

func (o *FileOwner) LoadCandidate() (Reader, error) {
	if o == nil {
		return nil, fmt.Errorf("agent settings owner is nil")
	}
	fileReader := viper.New()
	configdefaults.Apply(fileReader)
	fileReader.SetEnvPrefix("MISTER_MORPH")
	fileReader.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	fileReader.AutomaticEnv()
	configPath := strings.TrimSpace(o.configPath)
	if configPath != "" {
		fileReader.SetConfigFile(configPath)
		if err := configutil.ReadExpandedConfig(fileReader, configPath, nil); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		fileReader.Set("config", configPath)
	}

	reader := viper.New()
	configdefaults.Apply(reader)
	if o.base != nil {
		if err := reader.MergeConfigMap(o.base.AllSettings()); err != nil {
			return nil, err
		}
	}
	fileSettings := fileReader.AllSettings()
	for _, key := range []string{llmSettingsKey, skillsSettingsKey, toolsSettingsKey} {
		if value, ok := fileSettings[key]; ok {
			reader.Set(key, value)
		}
	}
	if configPath != "" {
		reader.Set("config", configPath)
	}
	return NewReaderSnapshot(reader), nil
}

func (o *FileOwner) ReplaceReader(reader Reader) {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.reader = NewReaderSnapshot(reader)
	o.mu.Unlock()
}

func (o *FileOwner) View(_ context.Context) (AgentSettingsView, error) {
	if o == nil {
		return AgentSettingsView{}, fmt.Errorf("agent settings owner is nil")
	}
	configExists, configSource, err := inspectAgentSettingsConfigSource(o.configPath)
	if err != nil {
		return AgentSettingsView{}, err
	}
	configValid := true
	settings, err := ReadFileSettings(o.configPath)
	if err != nil {
		if !isInvalidConfigYAMLError(err) {
			return AgentSettingsView{}, err
		}
		settings, err = defaultAgentSettingsPayload()
		if err != nil {
			return AgentSettingsView{}, err
		}
		configSource = "defaults"
		configValid = false
	}
	doc := configbootstrap.NewEmptyDocument()
	if configValid {
		doc, err = loadYAMLDocument(o.configPath)
		if err != nil {
			return AgentSettingsView{}, err
		}
	}
	runtimeLLM, err := settingsFromRuntimeReader(o.CurrentReader())
	if err != nil {
		return AgentSettingsView{}, err
	}
	settings, envManaged := buildAgentSettingsResponseView(settings, doc, runtimeLLM)
	skills, err := buildAgentSkillsSettingsResponse(o.configPath, configValid)
	if err != nil {
		return AgentSettingsView{}, err
	}
	return AgentSettingsView{
		LLM:          settings.LLM,
		EnvManaged:   envManaged,
		Skills:       skills,
		Tools:        settings.Tools,
		ConfigPath:   o.configPath,
		ConfigExists: configExists,
		ConfigValid:  configValid,
		ConfigSource: configSource,
		ReadOnly:     false,
	}, nil
}

func (o *FileOwner) Update(_ context.Context, update AgentSettingsUpdate) (AgentSettingsView, error) {
	if o == nil {
		return AgentSettingsView{}, fmt.Errorf("agent settings owner is nil")
	}
	if err := protectManagedFields(o.configPath, o.CurrentReader(), &update); err != nil {
		return AgentSettingsView{}, err
	}
	serialized, err := MarshalFileSettingsUpdate(o.configPath, update)
	if err != nil {
		return AgentSettingsView{}, &StatusError{Status: http.StatusBadRequest, Message: err.Error()}
	}
	effectiveLLM, err := resolveAgentSettingsLLMFromReader(o.CurrentReader(), update.LLM)
	if err != nil {
		return AgentSettingsView{}, err
	}
	if _, err := validateAgentConfigDocument(serialized, effectiveLLM); err != nil {
		return AgentSettingsView{}, &StatusError{Status: http.StatusBadRequest, Message: err.Error()}
	}
	if err := os.MkdirAll(filepath.Dir(o.configPath), 0o755); err != nil {
		return AgentSettingsView{}, err
	}
	if err := fsstore.WriteTextAtomic(o.configPath, string(serialized), fsstore.FileOptions{DirPerm: 0o755, FilePerm: 0o600}); err != nil {
		return AgentSettingsView{}, err
	}
	return o.persistedView(serialized)
}

func (o *FileOwner) persistedView(serialized []byte) (AgentSettingsView, error) {
	expanded, err := readExpandedAgentSettingsConfig(o.configPath)
	if err != nil {
		return AgentSettingsView{}, err
	}
	next, err := readAgentSettingsFromReader(expanded)
	if err != nil {
		return AgentSettingsView{}, err
	}
	doc, err := configbootstrap.LoadDocumentBytes(serialized)
	if err != nil {
		return AgentSettingsView{}, err
	}
	current, err := settingsFromRuntimeReader(o.CurrentReader())
	if err != nil {
		return AgentSettingsView{}, err
	}
	next, envManaged := buildAgentSettingsResponseView(next, doc, current)
	skills, err := buildAgentSkillsSettingsPayload(expanded)
	if err != nil {
		return AgentSettingsView{}, err
	}
	return AgentSettingsView{
		LLM:          next.LLM,
		EnvManaged:   envManaged,
		Skills:       skills,
		Tools:        next.Tools,
		ConfigPath:   o.configPath,
		ConfigExists: true,
		ConfigValid:  true,
		ConfigSource: "config",
		ReadOnly:     false,
	}, nil
}

func protectManagedFields(configPath string, reader Reader, update *AgentSettingsUpdate) error {
	if update == nil {
		return nil
	}
	doc, err := loadYAMLDocument(configPath)
	if err != nil {
		if isInvalidConfigYAMLError(err) {
			return nil
		}
		return err
	}
	current, err := ReadFileSettings(configPath)
	if err != nil {
		if !isInvalidConfigYAMLError(err) {
			return err
		}
		current, err = defaultAgentSettingsPayload()
		if err != nil {
			return err
		}
	}
	managed := CurrentEnvManaged(current.LLM.Provider).LLM
	fields := current.LLM.LLMConfigFieldsPayload
	llmNode := agentSettingsYAMLLLMNode(doc)
	managed = applyAgentSettingsYAMLEnvManaged(&fields, managed, llmNode, current.LLM.Provider)
	if err := protectManagedLLMConfigUpdate(&update.LLM.LLMConfigFieldsUpdate, managed, "llm"); err != nil {
		return err
	}
	profiles, managedProfiles := buildAgentSettingsProfileResponseView(current.LLM.Profiles, llmNode, current.LLM.Provider)
	if len(managedProfiles) == 0 {
		return nil
	}
	profileByName := make(map[string]LLMProfileSettingsPayload, len(profiles))
	for _, profile := range profiles {
		profileByName[strings.ToLower(strings.TrimSpace(profile.Name))] = profile
	}
	if update.LLM.Profile != nil {
		name := firstNonEmpty(update.LLM.Profile.OriginalName, update.LLM.Profile.Name)
		managedFields := managedProfileFields(managedProfiles, name)
		if len(managedFields) > 0 {
			profileUpdate := llmProfileSettingsAsUpdate(update.LLM.Profile.LLMProfileSettingsPayload)
			if err := protectManagedLLMConfigUpdate(&profileUpdate, managedFields, "llm.profiles."+strings.TrimSpace(name)); err != nil {
				return err
			}
		}
	}
	if update.LLM.DeleteProfile != nil {
		name := strings.TrimSpace(*update.LLM.DeleteProfile)
		if managedFields := managedProfileFields(managedProfiles, name); len(managedFields) > 0 {
			return managedFieldDeletionError("llm.profiles."+name, managedFields)
		}
	}
	if update.LLM.Profiles != nil {
		incoming := make(map[string]LLMProfileSettingsPayload, len(*update.LLM.Profiles))
		for _, profile := range *update.LLM.Profiles {
			incoming[strings.ToLower(strings.TrimSpace(profile.Name))] = profile
		}
		for name, managedFields := range managedProfiles {
			currentProfile, exists := profileByName[strings.ToLower(strings.TrimSpace(name))]
			if !exists {
				continue
			}
			next, exists := incoming[strings.ToLower(strings.TrimSpace(currentProfile.Name))]
			if !exists {
				return managedFieldDeletionError("llm.profiles."+currentProfile.Name, managedFields)
			}
			profileUpdate := llmProfileSettingsAsUpdate(next)
			if err := protectManagedLLMConfigUpdate(&profileUpdate, managedFields, "llm.profiles."+currentProfile.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

func managedProfileFields(profiles map[string]map[string]EnvManagedField, name string) map[string]EnvManagedField {
	name = strings.TrimSpace(name)
	for profileName, fields := range profiles {
		if strings.EqualFold(strings.TrimSpace(profileName), name) {
			return fields
		}
	}
	return nil
}

func managedFieldDeletionError(prefix string, managed map[string]EnvManagedField) error {
	fields := make([]string, 0, len(managed))
	for field := range managed {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	if len(fields) == 0 {
		return nil
	}
	field := managed[fields[0]]
	return managedFieldConflictError(prefix, fields[0], field)
}

func protectManagedLLMConfigUpdate(update *LLMConfigFieldsUpdate, managed map[string]EnvManagedField, prefix string) error {
	if update == nil || len(managed) == 0 {
		return nil
	}
	fields := []struct {
		name  string
		value **string
	}{
		{name: "inference_provider", value: &update.InferenceProvider},
		{name: "provider", value: &update.Provider},
		{name: "endpoint", value: &update.Endpoint},
		{name: "model", value: &update.Model},
		{name: "context_window_tokens", value: &update.ContextWindowTokens},
		{name: "api_key", value: &update.APIKey},
		{name: "bedrock_aws_key", value: &update.BedrockAWSKey},
		{name: "bedrock_aws_secret", value: &update.BedrockAWSSecret},
		{name: "bedrock_region", value: &update.BedrockRegion},
		{name: "bedrock_model_arn", value: &update.BedrockModelARN},
		{name: "cloudflare_api_token", value: &update.CloudflareAPIToken},
		{name: "cloudflare_account_id", value: &update.CloudflareAccountID},
		{name: "reasoning_effort", value: &update.ReasoningEffort},
		{name: "tools_emulation_mode", value: &update.ToolsEmulationMode},
	}
	for _, item := range fields {
		if item.value == nil || *item.value == nil {
			continue
		}
		field, ok := managed[item.name]
		if !ok {
			continue
		}
		if strings.TrimSpace(**item.value) == strings.TrimSpace(field.RawValue) {
			*item.value = nil
			continue
		}
		return managedFieldConflictError(prefix, item.name, field)
	}
	return nil
}

func managedFieldConflictError(prefix, name string, field EnvManagedField) error {
	source := strings.TrimSpace(field.Source)
	if source == "" && strings.TrimSpace(field.EnvName) != "" {
		source = "environment"
	}
	if source == "" {
		source = "external source"
	}
	return &StatusError{
		Status:  http.StatusConflict,
		Message: fmt.Sprintf("%s.%s is managed by %s", strings.TrimSpace(prefix), strings.TrimSpace(name), source),
	}
}

func ReadFileSettings(configPath string) (FileSettings, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultAgentSettingsPayload()
		}
		return FileSettings{}, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return defaultAgentSettingsPayload()
	}
	tmp, err := readExpandedAgentSettingsConfig(configPath)
	if err != nil {
		return FileSettings{}, fmt.Errorf("invalid config yaml: %w", err)
	}
	settings, err := readAgentSettingsFromReader(tmp)
	if err != nil {
		return FileSettings{}, err
	}
	doc, err := loadYAMLDocument(configPath)
	if err != nil {
		return FileSettings{}, err
	}
	return normalizeAgentSettingsConfigView(settings, doc), nil
}

func readExpandedAgentSettingsConfig(configPath string) (*viper.Viper, error) {
	tmp := viper.New()
	configdefaults.Apply(tmp)
	if err := readExpandedConfig(tmp, configPath); err != nil {
		return nil, err
	}
	return tmp, nil
}

func defaultAgentSettingsPayload() (FileSettings, error) {
	tmp := viper.New()
	configdefaults.Apply(tmp)
	settings, err := readAgentSettingsFromReader(tmp)
	if err != nil {
		return FileSettings{}, err
	}
	settings.LLM.Endpoint = ""
	settings.LLM.Model = ""
	return settings, nil
}

func buildAgentSkillsSettingsResponse(configPath string, configValid bool) (SkillsSettingsPayload, error) {
	reader := viper.New()
	configdefaults.Apply(reader)
	if configValid {
		if _, err := os.Stat(configPath); err != nil {
			if os.IsNotExist(err) {
				return buildAgentSkillsSettingsPayload(reader)
			}
			return SkillsSettingsPayload{}, err
		}
		expanded, err := readExpandedAgentSettingsConfig(configPath)
		if err != nil {
			return SkillsSettingsPayload{}, err
		}
		reader = expanded
	}
	return buildAgentSkillsSettingsPayload(reader)
}

func buildAgentSkillsSettingsPayload(reader *viper.Viper) (SkillsSettingsPayload, error) {
	return buildAgentSkillsSettingsPayloadFromReader(reader)
}

func buildAgentSkillsSettingsPayloadFromReader(reader Reader) (SkillsSettingsPayload, error) {
	cfg := skillsutil.SkillsConfigFromReader(reader)
	status, err := skillsutil.BuildSkillStatus(cfg, nil)
	if err != nil {
		return SkillsSettingsPayload{}, err
	}
	return SkillsSettingsPayload{
		Enabled:   cfg.Enabled,
		Load:      append([]string(nil), cfg.Requested...),
		Loaded:    status.Loaded,
		Available: status.Available,
	}, nil
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

func MarshalFileSettings(configPath string, values FileSettings) ([]byte, error) {
	return MarshalFileSettingsUpdate(configPath, AgentSettingsUpdate{
		LLM: LLMSettingsPayloadAsUpdate(values.LLM),
		Tools: &ToolsSettingsUpdate{
			WriteFile:    ToolEnabledUpdatePointer(values.Tools.WriteFile.Enabled),
			Spawn:        ToolEnabledUpdatePointer(values.Tools.Spawn.Enabled),
			Coder:        ToolEnabledUpdatePointer(values.Tools.Coder.Enabled),
			ContactsSend: ToolEnabledUpdatePointer(values.Tools.ContactsSend.Enabled),
			TodoUpdate:   ToolEnabledUpdatePointer(values.Tools.TodoUpdate.Enabled),
			PlanCreate:   ToolEnabledUpdatePointer(values.Tools.PlanCreate.Enabled),
			URLFetch:     ToolEnabledUpdatePointer(values.Tools.URLFetch.Enabled),
			WebSearch:    ToolEnabledUpdatePointer(values.Tools.WebSearch.Enabled),
			Bash:         ToolEnabledUpdatePointer(values.Tools.Bash.Enabled),
			PowerShell:   ToolEnabledUpdatePointer(values.Tools.PowerShell.Enabled),
		},
	})
}

func MarshalFileSettingsUpdate(configPath string, values AgentSettingsUpdate) ([]byte, error) {
	doc, err := loadYAMLDocument(configPath)
	if err != nil {
		if !isInvalidConfigYAMLError(err) {
			return nil, err
		}
		doc = configbootstrap.NewEmptyDocument()
	}
	current, err := defaultAgentSettingsPayload()
	if err != nil {
		return nil, err
	}
	if existing, readErr := ReadFileSettings(configPath); readErr == nil {
		current = existing
	} else if !isInvalidConfigYAMLError(readErr) && !os.IsNotExist(readErr) {
		return nil, readErr
	}
	if err := expandSingleLLMProfileUpdate(current.LLM, &values.LLM); err != nil {
		return nil, err
	}
	if err := applyAgentSettingsUpdateDocument(doc, current, values); err != nil {
		return nil, err
	}
	return configbootstrap.MarshalDocument(doc)
}

func applyAgentSettingsUpdateDocument(doc *yaml.Node, current FileSettings, values AgentSettingsUpdate) error {
	nextLLM := applyLLMSettingsUpdate(current.LLM, values.LLM)
	root, err := configbootstrap.DocumentMapping(doc)
	if err != nil {
		return err
	}

	llmNode := configbootstrap.EnsureMappingValue(root, llmSettingsKey)
	applyLLMConfigFieldsUpdate(llmNode, nextLLM.LLMConfigFieldsPayload, values.LLM.LLMConfigFieldsUpdate)
	if values.LLM.Profiles != nil {
		if values.LLM.Profile != nil {
			renameLLMProfileNode(llmNode, values.LLM.Profile.OriginalName, values.LLM.Profile.Name)
		}
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

	configbootstrap.DeleteMappingKey(root, "multimodal")

	if values.Skills != nil {
		skillsNode := configbootstrap.EnsureMappingValue(root, skillsSettingsKey)
		if values.Skills.Enabled != nil {
			configbootstrap.SetMappingBoolValue(skillsNode, "enabled", *values.Skills.Enabled)
		}
		if values.Skills.Load != nil {
			load, err := normalizeSkillLoadSettings(*values.Skills.Load)
			if err != nil {
				return err
			}
			configbootstrap.SetMappingStringList(skillsNode, "load", load)
		}
	}

	if values.Tools != nil {
		toolsNode := configbootstrap.EnsureMappingValue(root, toolsSettingsKey)
		if enabled := toolEnabledUpdateValue(values.Tools.WriteFile); enabled != nil {
			configbootstrap.SetMappingBoolPath(toolsNode, "write_file", "enabled", *enabled)
		}
		if enabled := toolEnabledUpdateValue(values.Tools.Spawn); enabled != nil {
			configbootstrap.SetMappingBoolPath(toolsNode, "spawn", "enabled", *enabled)
		}
		if enabled := toolEnabledUpdateValue(values.Tools.Coder); enabled != nil {
			configbootstrap.SetMappingBoolPath(toolsNode, "coder", "enabled", *enabled)
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

func validateAgentConfigDocument(data []byte, effectiveLLM LLMSettingsPayload) (*viper.Viper, error) {
	tmp := viper.New()
	configdefaults.Apply(tmp)
	tmp.SetConfigType("yaml")
	if err := tmp.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("invalid config yaml: %w", err)
	}
	values, err := llmutil.RuntimeValuesFromReader(tmp)
	if err != nil {
		return nil, err
	}
	values.InferenceProvider = strings.TrimSpace(effectiveLLM.InferenceProvider)
	values.Provider = firstNonEmpty(strings.TrimSpace(effectiveLLM.Provider), values.Provider)
	values.Endpoint = firstNonEmpty(strings.TrimSpace(effectiveLLM.Endpoint), values.Endpoint)
	values.APIKey = firstNonEmpty(strings.TrimSpace(effectiveLLM.APIKey), values.APIKey)
	values.Model = firstNonEmpty(strings.TrimSpace(effectiveLLM.Model), values.Model)
	values.ContextWindowRaw = firstNonEmpty(strings.TrimSpace(effectiveLLM.ContextWindowTokens), values.ContextWindowRaw)
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
	for name := range values.Profiles {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		profileValues := values
		profileValues.Routes.MainLoop = llmutil.RoutePolicyConfig{Profile: name}
		if err := validateAgentLLMRoute(profileValues, llmutil.RoutePurposeMainLoop); err != nil {
			return nil, err
		}
	}
	if err := validateAgentSkillsLoad(tmp); err != nil {
		return nil, err
	}
	return tmp, nil
}

func settingsFromRuntimeReader(reader Reader) (LLMSettingsPayload, error) {
	values, err := EffectiveRuntimeValues(reader)
	if err != nil {
		return LLMSettingsPayload{}, err
	}
	payload := SettingsPayloadFromRuntimeValues(values)
	selection := llmselect.ProcessStore().Get()
	if selection.Mode == llmselect.ModeManual {
		payload.CurrentProfile = selection.ManualProfile
	}
	return payload, nil
}

func resolveAgentSettingsLLMFromReader(reader Reader, overrides LLMSettingsUpdate) (LLMSettingsPayload, error) {
	values, err := settingsFromRuntimeReader(reader)
	if err != nil {
		return LLMSettingsPayload{}, err
	}
	return applyLLMSettingsUpdate(values, overrides), nil
}

func resolveAgentSettingsTestLLMFromReader(reader Reader, req agentSettingsTestRequest) (LLMSettingsPayload, error) {
	targetProfile := agentSettingsTestTargetProfile(req)
	snapshot, err := resolveAgentSettingsTestSnapshotFromReader(reader, req, targetProfile)
	if err != nil {
		return LLMSettingsPayload{}, err
	}
	values, err := ResolveConnectionTestValues(
		reader,
		snapshot,
		targetProfile,
		configutil.SecretRefSourceFromReader(reader),
	)
	if err != nil {
		return LLMSettingsPayload{}, err
	}
	payload := SettingsPayloadFromRuntimeValues(values)
	payload.Profiles = nil
	payload.FallbackProfiles = nil
	return payload, nil
}

func resolveAgentSettingsTestSnapshotFromReader(reader Reader, req agentSettingsTestRequest, targetProfile string) (LLMSettingsPayload, error) {
	if targetProfile != "" && !strings.EqualFold(targetProfile, llmutil.RouteProfileDefault) {
		return resolveAgentSettingsLLMFromReader(reader, LLMSettingsPayloadAsProfileTestUpdate(req.LLM))
	}
	return resolveAgentSettingsLLMFromReader(reader, LLMSettingsPayloadAsNonEmptyUpdate(req.LLM))
}

func agentSettingsTestTargetProfile(req agentSettingsTestRequest) string {
	if req.TargetProfile == nil {
		return ""
	}
	return strings.TrimSpace(*req.TargetProfile)
}

func applyLLMSettingsUpdate(current LLMSettingsPayload, incoming LLMSettingsUpdate) LLMSettingsPayload {
	merged := current
	if incoming.InferenceProvider != nil {
		merged.InferenceProvider = strings.TrimSpace(*incoming.InferenceProvider)
	} else if incoming.Provider != nil || incoming.Endpoint != nil {
		merged.InferenceProvider = ""
	}
	if incoming.Provider != nil {
		merged.Provider = strings.TrimSpace(*incoming.Provider)
	}
	if incoming.Endpoint != nil {
		merged.Endpoint = strings.TrimSpace(*incoming.Endpoint)
	}
	if incoming.Model != nil {
		merged.Model = strings.TrimSpace(*incoming.Model)
	}
	if incoming.ContextWindowTokens != nil {
		merged.ContextWindowTokens = strings.TrimSpace(*incoming.ContextWindowTokens)
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
		merged.Profiles = append([]LLMProfileSettingsPayload(nil), (*incoming.Profiles)...)
	}
	if incoming.FallbackProfiles != nil {
		merged.FallbackProfiles = NormalizeNamedProfileSequence(*incoming.FallbackProfiles)
	}
	merged.LLMConfigFieldsPayload = ResolveInferenceProviderSettingsFields(merged.LLMConfigFieldsPayload)
	merged.LLMConfigFieldsPayload = SanitizeProviderSpecificLLMFields(
		merged.LLMConfigFieldsPayload,
		merged.Provider,
	)
	return merged
}

func LLMSettingsPayloadAsUpdate(values LLMSettingsPayload) LLMSettingsUpdate {
	return LLMSettingsUpdate{
		LLMConfigFieldsUpdate: LLMConfigFieldsUpdate{
			InferenceProvider:   stringPointer(values.InferenceProvider),
			Provider:            stringPointer(values.Provider),
			Endpoint:            stringPointer(values.Endpoint),
			Model:               stringPointer(values.Model),
			ContextWindowTokens: stringPointer(values.ContextWindowTokens),
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

func LLMSettingsPayloadAsNonEmptyUpdate(values LLMSettingsPayload) LLMSettingsUpdate {
	update := LLMSettingsUpdate{}
	if value := strings.TrimSpace(values.InferenceProvider); value != "" {
		update.InferenceProvider = stringPointer(value)
	}
	if value := strings.TrimSpace(values.Provider); value != "" {
		update.Provider = stringPointer(value)
	}
	if value := strings.TrimSpace(values.Endpoint); value != "" {
		update.Endpoint = stringPointer(value)
	}
	if value := strings.TrimSpace(values.Model); value != "" {
		update.Model = stringPointer(value)
	}
	if value := strings.TrimSpace(values.ContextWindowTokens); value != "" {
		update.ContextWindowTokens = stringPointer(value)
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

func LLMSettingsPayloadAsProfileTestUpdate(values LLMSettingsPayload) LLMSettingsUpdate {
	update := LLMSettingsPayloadAsNonEmptyUpdate(values)
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

func ToolEnabledUpdatePointer(value bool) *ToolEnabledUpdate {
	return &ToolEnabledUpdate{Enabled: boolPointer(value)}
}

func toolEnabledUpdateValue(update *ToolEnabledUpdate) *bool {
	if update == nil {
		return nil
	}
	return update.Enabled
}

func profileSettingsPointer(values []LLMProfileSettingsPayload) *[]LLMProfileSettingsPayload {
	next := append([]LLMProfileSettingsPayload(nil), values...)
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

func normalizeLLMProfileSettings(profiles []LLMProfileSettingsPayload) ([]LLMProfileSettingsPayload, error) {
	if len(profiles) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(profiles))
	out := make([]LLMProfileSettingsPayload, 0, len(profiles))
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
		normalized := LLMProfileSettingsPayload{
			Name: name,
			LLMConfigFieldsPayload: LLMConfigFieldsPayload{
				InferenceProvider:   strings.TrimSpace(profile.InferenceProvider),
				Provider:            strings.TrimSpace(profile.Provider),
				Endpoint:            strings.TrimSpace(profile.Endpoint),
				Model:               strings.TrimSpace(profile.Model),
				ContextWindowTokens: strings.TrimSpace(profile.ContextWindowTokens),
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
		normalized.LLMConfigFieldsPayload = ResolveInferenceProviderSettingsFields(normalized.LLMConfigFieldsPayload)
		if strings.EqualFold(normalized.Provider, "cloudflare") {
			normalized.CloudflareAPIToken = firstNonEmpty(normalized.CloudflareAPIToken, normalized.APIKey)
		}
		if normalized.Provider != "" {
			normalized.LLMConfigFieldsPayload = SanitizeProviderSpecificLLMFields(
				normalized.LLMConfigFieldsPayload,
				normalized.Provider,
			)
		}
		out = append(out, normalized)
	}
	return out, nil
}

func expandSingleLLMProfileUpdate(current LLMSettingsPayload, update *LLMSettingsUpdate) error {
	if update == nil || (update.Profile == nil && update.DeleteProfile == nil) {
		return nil
	}
	if update.Profiles != nil {
		return fmt.Errorf("profiles cannot be combined with a single profile update")
	}
	if update.Profile != nil && update.DeleteProfile != nil {
		return fmt.Errorf("profile and delete_profile cannot be combined")
	}

	profiles := append([]LLMProfileSettingsPayload(nil), current.Profiles...)
	fallbacks := append([]string(nil), current.FallbackProfiles...)
	if update.Profile != nil {
		normalized, err := normalizeLLMProfileSettings([]LLMProfileSettingsPayload{
			update.Profile.LLMProfileSettingsPayload,
		})
		if err != nil {
			return err
		}
		next := normalized[0]
		originalName := strings.TrimSpace(update.Profile.OriginalName)
		targetIndex := -1
		if originalName != "" {
			for i := range profiles {
				if strings.EqualFold(strings.TrimSpace(profiles[i].Name), originalName) {
					targetIndex = i
					break
				}
			}
			if targetIndex < 0 {
				return fmt.Errorf("profile %q not found", originalName)
			}
		}
		for i := range profiles {
			if i == targetIndex {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(profiles[i].Name), next.Name) {
				return fmt.Errorf("duplicate profile %q", next.Name)
			}
		}
		if targetIndex < 0 {
			profiles = append(profiles, next)
		} else {
			profiles[targetIndex] = next
			if originalName != next.Name {
				for i := range fallbacks {
					if strings.EqualFold(strings.TrimSpace(fallbacks[i]), originalName) {
						fallbacks[i] = next.Name
					}
				}
				update.FallbackProfiles = stringSlicePointer(fallbacks)
			}
		}
		update.Profiles = profileSettingsPointer(profiles)
		return nil
	}

	name := strings.TrimSpace(*update.DeleteProfile)
	if name == "" {
		return fmt.Errorf("profile name is required")
	}
	targetIndex := -1
	for i := range profiles {
		if strings.EqualFold(strings.TrimSpace(profiles[i].Name), name) {
			targetIndex = i
			break
		}
	}
	if targetIndex < 0 {
		return fmt.Errorf("profile %q not found", name)
	}
	profiles = append(profiles[:targetIndex], profiles[targetIndex+1:]...)
	filteredFallbacks := fallbacks[:0]
	for _, fallback := range fallbacks {
		if !strings.EqualFold(strings.TrimSpace(fallback), name) {
			filteredFallbacks = append(filteredFallbacks, fallback)
		}
	}
	update.Profiles = profileSettingsPointer(profiles)
	update.FallbackProfiles = stringSlicePointer(filteredFallbacks)
	return nil
}

func llmProfileSettingsAsUpdate(profile LLMProfileSettingsPayload) LLMConfigFieldsUpdate {
	return LLMConfigFieldsUpdate{
		InferenceProvider:   stringPointer(profile.InferenceProvider),
		Provider:            stringPointer(profile.Provider),
		Endpoint:            stringPointer(profile.Endpoint),
		Model:               stringPointer(profile.Model),
		ContextWindowTokens: stringPointer(profile.ContextWindowTokens),
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

func renameLLMProfileNode(llmNode *yaml.Node, originalName, nextName string) {
	originalName = strings.TrimSpace(originalName)
	nextName = strings.TrimSpace(nextName)
	if llmNode == nil || originalName == "" || nextName == "" || originalName == nextName {
		return
	}
	profilesNode := configbootstrap.FindMappingValue(llmNode, "profiles")
	if profilesNode == nil || profilesNode.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(profilesNode.Content); i += 2 {
		if strings.EqualFold(strings.TrimSpace(profilesNode.Content[i].Value), originalName) {
			profilesNode.Content[i].Value = nextName
			return
		}
	}
}

func applyLLMConfigFieldsUpdate(node *yaml.Node, effective LLMConfigFieldsPayload, update LLMConfigFieldsUpdate) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	if update.InferenceProvider != nil {
		configbootstrap.SetOrDeleteMappingScalar(node, "inference_provider", *update.InferenceProvider)
		if update.Provider == nil {
			configbootstrap.SetOrDeleteMappingScalar(node, "provider", "")
		}
		if update.Endpoint == nil {
			if info, ok := llmutil.InferenceProviderInfoByValue(*update.InferenceProvider); ok && !info.SupportsCustomAPIBase {
				configbootstrap.SetOrDeleteMappingScalar(node, "endpoint", "")
			}
		}
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
	if update.ContextWindowTokens != nil {
		configbootstrap.SetOrDeleteMappingScalar(node, "context_window_tokens", *update.ContextWindowTokens)
	}
	if update.ReasoningEffort != nil {
		configbootstrap.SetOrDeleteMappingScalar(node, "reasoning_effort", *update.ReasoningEffort)
	}
	if update.ToolsEmulationMode != nil {
		configbootstrap.SetOrDeleteMappingScalar(node, "tools_emulation_mode", *update.ToolsEmulationMode)
	}
	if strings.EqualFold(strings.TrimSpace(effective.InferenceProvider), llmutil.InferenceProviderMisterMorphPro) {
		configbootstrap.SetOrDeleteMappingScalar(node, "provider", "")
		configbootstrap.SetOrDeleteMappingScalar(node, "endpoint", "")
		configbootstrap.SetOrDeleteMappingScalar(node, "api_key", "")
		configbootstrap.DeleteMappingKey(node, "azure")
		configbootstrap.DeleteMappingKey(node, "cloudflare")
		configbootstrap.DeleteMappingKey(node, "bedrock")
		return
	}
	switch strings.ToLower(strings.TrimSpace(effective.Provider)) {
	case "openai_codex":
		if update.APIKey != nil {
			configbootstrap.SetOrDeleteMappingScalar(node, "api_key", *update.APIKey)
		}
		configbootstrap.DeleteMappingKey(node, "cloudflare")
		configbootstrap.DeleteMappingKey(node, "bedrock")
		return
	case "xai_oauth":
		configbootstrap.SetOrDeleteMappingScalar(node, "endpoint", "")
		configbootstrap.SetOrDeleteMappingScalar(node, "api_key", "")
		configbootstrap.DeleteMappingKey(node, "cloudflare")
		configbootstrap.DeleteMappingKey(node, "bedrock")
		configbootstrap.DeleteMappingKey(node, "aws")
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

func setLLMProfilesNode(llmNode *yaml.Node, profiles []LLMProfileSettingsPayload, defaultProvider string) error {
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
		effective := profile.LLMConfigFieldsPayload
		effective = ResolveInferenceProviderSettingsFields(effective)
		effective.Provider = firstNonEmpty(effective.Provider, defaultProvider)
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
	values = NormalizeNamedProfileSequence(values)
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
	values = NormalizeNamedProfileSequence(values)
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

func mergeLLMSettingsMap(base map[string]any, values LLMSettingsPayload) map[string]any {
	out := cloneStringAnyMap(base)
	mergeLLMConfigFieldsMap(out, values.LLMConfigFieldsPayload, values.Provider)

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
			mergeLLMConfigFieldsMap(profileMap, profile.LLMConfigFieldsPayload, firstNonEmpty(profile.Provider, values.Provider))
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
	values = NormalizeNamedProfileSequence(values)
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

func mergeLLMConfigFieldsMap(dst map[string]any, fields LLMConfigFieldsPayload, effectiveProvider string) {
	if dst == nil {
		return
	}
	fields = ResolveInferenceProviderSettingsFields(fields)
	setOrDeleteStringMapValue(dst, "provider", fields.Provider)
	setOrDeleteStringMapValue(dst, "inference_provider", fields.InferenceProvider)
	setOrDeleteStringMapValue(dst, "endpoint", fields.Endpoint)
	setOrDeleteStringMapValue(dst, "model", fields.Model)
	setOrDeleteStringMapValue(dst, "context_window_tokens", fields.ContextWindowTokens)
	setOrDeleteStringMapValue(dst, "reasoning_effort", fields.ReasoningEffort)
	setOrDeleteStringMapValue(dst, "tools_emulation_mode", fields.ToolsEmulationMode)
	switch strings.ToLower(strings.TrimSpace(effectiveProvider)) {
	case "openai_codex":
		setOrDeleteStringMapValue(dst, "api_key", fields.APIKey)
		delete(dst, "cloudflare")
		delete(dst, "bedrock")
		return
	case "xai_oauth":
		delete(dst, "endpoint")
		delete(dst, "api_key")
		delete(dst, "cloudflare")
		delete(dst, "bedrock")
		delete(dst, "aws")
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

func readExpandedConfig(v *viper.Viper, configPath string) error {
	if v == nil {
		return fmt.Errorf("config reader is nil")
	}
	return configutil.ReadExpandedConfig(v, configPath, nil)
}

func isInvalidConfigYAMLError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "invalid config yaml")
}

func validateAgentSkillsLoad(reader *viper.Viper) error {
	cfg := skillsutil.SkillsConfigFromReader(reader)
	requested, err := normalizeSkillLoadSettings(cfg.Requested)
	if err != nil {
		return err
	}
	if len(requested) == 0 || (len(requested) == 1 && requested[0] == "*") {
		return nil
	}
	discovered, err := skills.Discover(skills.DiscoverOptions{Roots: cfg.Roots})
	if err != nil {
		return err
	}
	for i, sk := range discovered {
		loaded, err := skills.LoadFrontmatter(sk, 64*1024)
		if err != nil {
			return err
		}
		discovered[i] = loaded
	}
	seenResolved := map[string]string{}
	for _, query := range requested {
		sk, err := skills.Resolve(discovered, query)
		if err != nil {
			return fmt.Errorf("unknown skill %q", query)
		}
		key := strings.ToLower(strings.TrimSpace(sk.ID))
		if prev, ok := seenResolved[key]; ok {
			return fmt.Errorf("duplicate skill %q matches %q", query, prev)
		}
		seenResolved[key] = query
	}
	return nil
}

func normalizeSkillLoadSettings(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	wildcard := false
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("skills.load entries must be one skill per item")
		}
		if value == "*" {
			wildcard = true
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate skill %q", value)
		}
		seen[key] = struct{}{}
		normalized = append(normalized, value)
	}
	if wildcard && len(normalized) > 1 {
		return nil, fmt.Errorf("skills.load wildcard '*' must be the only entry")
	}
	return normalized, nil
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

func normalizeAgentSettingsConfigView(settings FileSettings, doc *yaml.Node) FileSettings {
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
	settings FileSettings,
	doc *yaml.Node,
	runtimeLLM LLMSettingsPayload,
) (FileSettings, EnvManagedPayload) {
	settings = normalizeAgentSettingsConfigView(settings, doc)
	if runtimeLLM.CurrentProfile != "" {
		settings.LLM.CurrentProfile = runtimeLLM.CurrentProfile
	}
	envManaged := CurrentEnvManaged(runtimeLLM.Provider)
	llmNode := agentSettingsYAMLLLMNode(doc)
	defaultProvider := strings.TrimSpace(settings.LLM.Provider)
	if field, ok := envManaged.LLM["provider"]; ok && strings.TrimSpace(field.Value) != "" {
		defaultProvider = strings.TrimSpace(field.Value)
	}
	envManaged.LLM = applyAgentSettingsYAMLEnvManaged(
		&settings.LLM.LLMConfigFieldsPayload,
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
	profiles []LLMProfileSettingsPayload,
	llmNode *yaml.Node,
	defaultProvider string,
) ([]LLMProfileSettingsPayload, map[string]map[string]EnvManagedField) {
	if len(profiles) == 0 {
		return profiles, nil
	}
	profilesNode := configbootstrap.FindMappingValue(llmNode, "profiles")
	out := append([]LLMProfileSettingsPayload(nil), profiles...)
	envManaged := map[string]map[string]EnvManagedField{}
	for i := range out {
		name := strings.TrimSpace(out[i].Name)
		if name == "" {
			continue
		}
		profileNode := configbootstrap.FindMappingValue(profilesNode, name)
		profileProvider := firstNonEmpty(strings.TrimSpace(out[i].Provider), defaultProvider)
		fields := applyAgentSettingsYAMLEnvManaged(
			&out[i].LLMConfigFieldsPayload,
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
	fields *LLMConfigFieldsPayload,
	envManaged map[string]EnvManagedField,
	node *yaml.Node,
	defaultProvider string,
) map[string]EnvManagedField {
	if fields == nil {
		return envManaged
	}
	if _, ok := envManaged["inference_provider"]; !ok {
		if field, ok := YAMLManagedField(node, defaultProvider, "inference_provider"); ok {
			if envManaged == nil {
				envManaged = map[string]EnvManagedField{}
			}
			envManaged["inference_provider"] = field
		}
	}
	if _, ok := envManaged["provider"]; !ok {
		if field, ok := YAMLManagedField(node, defaultProvider, "provider"); ok {
			if envManaged == nil {
				envManaged = map[string]EnvManagedField{}
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
		"context_window_tokens",
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
		field, ok := YAMLManagedField(node, effectiveProvider, fieldName)
		if !ok {
			continue
		}
		if envManaged == nil {
			envManaged = map[string]EnvManagedField{}
		}
		envManaged[fieldName] = field
	}
	RedactManagedLLMSecrets(fields, envManaged, effectiveProvider)
	if len(envManaged) == 0 {
		return nil
	}
	return envManaged
}

func YAMLManagedField(
	node *yaml.Node,
	provider string,
	field string,
) (EnvManagedField, bool) {
	fieldPathSets := [][]string{}
	switch strings.TrimSpace(field) {
	case "inference_provider":
		fieldPathSets = [][]string{{"inference_provider"}}
	case "provider":
		fieldPathSets = [][]string{{"provider"}}
	case "endpoint":
		normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
		if normalizedProvider != "xai_oauth" {
			fieldPathSets = [][]string{{"endpoint"}}
		}
	case "model":
		fieldPathSets = [][]string{{"model"}}
		if strings.EqualFold(strings.TrimSpace(provider), "azure") {
			fieldPathSets = append([][]string{{"azure", "deployment"}}, fieldPathSets...)
		}
	case "context_window_tokens":
		fieldPathSets = [][]string{{"context_window_tokens"}}
	case "api_key":
		normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
		if normalizedProvider != "cloudflare" && normalizedProvider != "bedrock" &&
			normalizedProvider != "xai_oauth" {
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
		normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
		if normalizedProvider != "openai_codex" && normalizedProvider != "xai_oauth" {
			fieldPathSets = [][]string{{"cloudflare", "api_token"}}
		}
		if strings.EqualFold(strings.TrimSpace(provider), "cloudflare") {
			fieldPathSets = append(fieldPathSets, []string{"api_key"})
		}
	case "cloudflare_account_id":
		normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
		if normalizedProvider != "xai_oauth" {
			fieldPathSets = [][]string{{"cloudflare", "account_id"}}
		}
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
		entry, ok := YAMLPlaceholderField(current, field)
		if ok {
			return entry, true
		}
	}
	return EnvManagedField{}, false
}

func YAMLPlaceholderField(
	node *yaml.Node,
	field string,
) (EnvManagedField, bool) {
	if node == nil || node.Kind != yaml.ScalarNode {
		return EnvManagedField{}, false
	}
	value := strings.TrimSpace(node.Value)
	ref, ok := secref.ParseSingleRef(value)
	if !ok {
		return EnvManagedField{}, false
	}
	out := EnvManagedField{RawValue: value}
	if ref.Kind == secref.RefKindAWSSecretsManager {
		out.Source = string(secref.RefKindAWSSecretsManager)
		return out, true
	}
	if ref.Kind != secref.RefKindEnv || strings.TrimSpace(ref.EnvName) == "" {
		return EnvManagedField{}, false
	}
	out.EnvName = ref.EnvName
	switch strings.TrimSpace(field) {
	case "api_key", "bedrock_aws_key", "bedrock_aws_secret", "cloudflare_api_token":
	default:
		if resolved, ok := os.LookupEnv(ref.EnvName); ok {
			out.Value = strings.TrimSpace(resolved)
		}
	}
	return out, true
}

func agentSettingsYAMLLLMNode(doc *yaml.Node) *yaml.Node {
	root, err := configbootstrap.DocumentMapping(doc)
	if err != nil {
		return nil
	}
	return configbootstrap.FindMappingValue(root, llmSettingsKey)
}

func sortAgentSettingsProfilesByYAMLOrder(profiles []LLMProfileSettingsPayload, doc *yaml.Node) []LLMProfileSettingsPayload {
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
	out := append([]LLMProfileSettingsPayload(nil), profiles...)
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
	llmutil.ConfigReader
	GetBool(string) bool
}) (FileSettings, error) {
	if r == nil {
		return FileSettings{}, fmt.Errorf("config reader is nil")
	}
	values, err := llmutil.RuntimeValuesFromReader(r)
	if err != nil {
		return FileSettings{}, err
	}
	return FileSettings{
		LLM: SettingsPayloadFromRuntimeValues(values),
		Tools: ToolsSettingsPayload{
			WriteFile:    ToolEnabledPayload{Enabled: r.GetBool("tools.write_file.enabled")},
			Spawn:        ToolEnabledPayload{Enabled: r.GetBool("tools.spawn.enabled")},
			Coder:        ToolEnabledPayload{Enabled: r.GetBool("tools.coder.enabled")},
			ContactsSend: ToolEnabledPayload{Enabled: r.GetBool("tools.contacts_send.enabled")},
			TodoUpdate:   ToolEnabledPayload{Enabled: r.GetBool("tools.todo_update.enabled")},
			PlanCreate:   ToolEnabledPayload{Enabled: r.GetBool("tools.plan_create.enabled")},
			URLFetch:     ToolEnabledPayload{Enabled: r.GetBool("tools.url_fetch.enabled")},
			WebSearch:    ToolEnabledPayload{Enabled: r.GetBool("tools.web_search.enabled")},
			Bash:         ToolEnabledPayload{Enabled: r.GetBool("tools.bash.enabled")},
			PowerShell:   ToolEnabledPayload{Enabled: r.GetBool("tools.powershell.enabled")},
		},
	}, nil
}
