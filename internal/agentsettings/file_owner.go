package agentsettings

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
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
	"github.com/quailyquaily/mistermorph/internal/mcphost"
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
	mcpSettingsKey    = "mcp"
)

var llmSecretFieldNames = []string{"api_key", "bedrock_aws_key", "bedrock_aws_secret", "cloudflare_api_token"}

type FileSettings struct {
	LLM   LLMSettingsPayload
	Tools ToolsSettingsPayload
	MCP   MCPSettingsPayload
}

type agentSettingsTestRequest struct {
	LLM           LLMSettingsPayload
	TargetProfile *string
}

type FileOwnerOptions struct {
	ConfigPath   string
	Reader       Reader
	SecretSource secref.Source
	OSStore      secref.OSStore
}

type FileOwner struct {
	mu           sync.RWMutex
	configPath   string
	base         *ReaderSnapshot
	reader       *ReaderSnapshot
	secretSource secref.Source
	osStore      secref.OSStore
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
	osStore := opts.OSStore
	secretSource := opts.SecretSource
	if secretSource == nil {
		if osStore != nil {
			secretSource = secref.NewDefaultSourceWithOSStore(configutil.AWSSecretsManagerConfigFromReader(reader), osStore)
		} else {
			secretSource = configutil.SecretRefSourceFromReader(reader)
		}
	}
	return &FileOwner{
		configPath:   resolveFileOwnerConfigPath(opts.ConfigPath, opts.Reader),
		base:         reader,
		reader:       reader,
		secretSource: secretSource,
		osStore:      osStore,
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
		if err := configutil.ReadExpandedConfigWithSource(fileReader, configPath, o.secretSource, nil); err != nil && !os.IsNotExist(err) {
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
	for _, key := range []string{llmSettingsKey, skillsSettingsKey, toolsSettingsKey, mcpSettingsKey} {
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
	settings, err := readFileSettingsWithSource(o.configPath, o.secretSource)
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
	settings, envManaged, secretFields := buildAgentSettingsResponseView(settings, doc, runtimeLLM)
	skills, err := buildAgentSkillsSettingsResponse(o.configPath, configValid, o.secretSource)
	if err != nil {
		return AgentSettingsView{}, err
	}
	return AgentSettingsView{
		LLM:          settings.LLM,
		EnvManaged:   envManaged,
		SecretFields: secretFields,
		Skills:       skills,
		Tools:        settings.Tools,
		MCP:          settings.MCP,
		ConfigPath:   o.configPath,
		ConfigExists: configExists,
		ConfigValid:  configValid,
		ConfigSource: configSource,
		ReadOnly:     false,
	}, nil
}

func (o *FileOwner) Update(ctx context.Context, update AgentSettingsUpdate) (AgentSettingsView, error) {
	if o == nil {
		return AgentSettingsView{}, fmt.Errorf("agent settings owner is nil")
	}
	previousDoc, err := loadYAMLDocument(o.configPath)
	if err != nil && !os.IsNotExist(err) {
		return AgentSettingsView{}, err
	}
	previousSecretIDs := secref.OSSecretIDsInYAML(previousDoc)
	if err := protectManagedFields(o.configPath, o.CurrentReader(), o.secretSource, &update); err != nil {
		return AgentSettingsView{}, err
	}
	newSecretIDs, err := o.prepareLLMSecretUpdates(ctx, &update)
	if err != nil {
		return AgentSettingsView{}, err
	}
	committed := false
	defer func() {
		if !committed {
			deleteOSSecretIDs(ctx, o.osStore, newSecretIDs)
		}
	}()
	serialized, err := marshalFileSettingsUpdateWithSource(o.configPath, update, o.secretSource)
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
	committed = true
	view, err := o.persistedView(serialized)
	if err != nil {
		return AgentSettingsView{}, err
	}
	doc, err := configbootstrap.LoadDocumentBytes(serialized)
	if err != nil {
		return AgentSettingsView{}, err
	}
	currentSecretIDs := secref.OSSecretIDsInYAML(doc)
	if o.osStore != nil {
		for id := range previousSecretIDs {
			if !currentSecretIDs[id] {
				_ = o.osStore.Delete(ctx, id)
			}
		}
	}
	return view, nil
}

func deleteOSSecretIDs(ctx context.Context, store secref.OSStore, ids []string) {
	if store == nil {
		return
	}
	for _, id := range ids {
		_ = store.Delete(ctx, id)
	}
}

func (o *FileOwner) prepareLLMSecretUpdates(ctx context.Context, update *AgentSettingsUpdate) ([]string, error) {
	if o == nil || update == nil || o.osStore == nil {
		return nil, nil
	}
	doc, err := loadYAMLDocument(o.configPath)
	if err != nil {
		return nil, err
	}
	current, err := readFileSettingsWithSource(o.configPath, o.secretSource)
	if err != nil {
		return nil, err
	}
	llmNode := agentSettingsYAMLLLMNode(doc)
	fields := []struct {
		name  string
		value **string
	}{
		{name: "api_key", value: &update.LLM.APIKey},
		{name: "bedrock_aws_key", value: &update.LLM.BedrockAWSKey},
		{name: "bedrock_aws_secret", value: &update.LLM.BedrockAWSSecret},
		{name: "cloudflare_api_token", value: &update.LLM.CloudflareAPIToken},
	}
	updates := make([]llmSecretUpdate, 0, len(fields))
	for _, field := range fields {
		if field.value == nil || *field.value == nil {
			continue
		}
		value := **field.value
		updates = append(updates, llmSecretUpdate{
			name: field.name, configKey: llmSecretConfigKey("llm", field.name), value: &value, target: field.value,
		})
	}
	if update.LLM.Profile != nil {
		profile := update.LLM.Profile
		currentName := firstNonEmpty(profile.OriginalName, profile.Name)
		profilesNode := configbootstrap.FindMappingValue(llmNode, "profiles")
		profileNode := configbootstrap.FindMappingValue(profilesNode, currentName)
		provider := profile.Provider
		for _, currentProfile := range current.LLM.Profiles {
			if strings.EqualFold(strings.TrimSpace(currentProfile.Name), strings.TrimSpace(currentName)) {
				provider = currentProfile.Provider
				break
			}
		}
		updates = append(updates,
			llmSecretUpdate{node: profileNode, provider: provider, name: "api_key", configKey: llmSecretConfigKey("llm.profiles."+strings.TrimSpace(profile.Name), "api_key"), value: &profile.APIKey, preserveExisting: !profile.secretFieldProvided("api_key", profile.APIKey)},
			llmSecretUpdate{node: profileNode, provider: provider, name: "bedrock_aws_key", configKey: llmSecretConfigKey("llm.profiles."+strings.TrimSpace(profile.Name), "bedrock_aws_key"), value: &profile.BedrockAWSKey, preserveExisting: !profile.secretFieldProvided("bedrock_aws_key", profile.BedrockAWSKey)},
			llmSecretUpdate{node: profileNode, provider: provider, name: "bedrock_aws_secret", configKey: llmSecretConfigKey("llm.profiles."+strings.TrimSpace(profile.Name), "bedrock_aws_secret"), value: &profile.BedrockAWSSecret, preserveExisting: !profile.secretFieldProvided("bedrock_aws_secret", profile.BedrockAWSSecret)},
			llmSecretUpdate{node: profileNode, provider: provider, name: "cloudflare_api_token", configKey: llmSecretConfigKey("llm.profiles."+strings.TrimSpace(profile.Name), "cloudflare_api_token"), value: &profile.CloudflareAPIToken, preserveExisting: !profile.secretFieldProvided("cloudflare_api_token", profile.CloudflareAPIToken)},
		)
	}
	if update.LLM.Profiles != nil {
		profilesNode := configbootstrap.FindMappingValue(llmNode, "profiles")
		for i := range *update.LLM.Profiles {
			profile := &(*update.LLM.Profiles)[i]
			profileNode := configbootstrap.FindMappingValue(profilesNode, strings.TrimSpace(profile.Name))
			provider := profile.Provider
			for _, currentProfile := range current.LLM.Profiles {
				if strings.EqualFold(strings.TrimSpace(currentProfile.Name), strings.TrimSpace(profile.Name)) {
					provider = currentProfile.Provider
					break
				}
			}
			updates = append(updates,
				llmSecretUpdate{node: profileNode, provider: provider, name: "api_key", configKey: llmSecretConfigKey("llm.profiles."+strings.TrimSpace(profile.Name), "api_key"), value: &profile.APIKey, preserveExisting: true},
				llmSecretUpdate{node: profileNode, provider: provider, name: "bedrock_aws_key", configKey: llmSecretConfigKey("llm.profiles."+strings.TrimSpace(profile.Name), "bedrock_aws_key"), value: &profile.BedrockAWSKey, preserveExisting: true},
				llmSecretUpdate{node: profileNode, provider: provider, name: "bedrock_aws_secret", configKey: llmSecretConfigKey("llm.profiles."+strings.TrimSpace(profile.Name), "bedrock_aws_secret"), value: &profile.BedrockAWSSecret, preserveExisting: true},
				llmSecretUpdate{node: profileNode, provider: provider, name: "cloudflare_api_token", configKey: llmSecretConfigKey("llm.profiles."+strings.TrimSpace(profile.Name), "cloudflare_api_token"), value: &profile.CloudflareAPIToken, preserveExisting: true},
			)
		}
	}
	newSecretIDs, err := o.storeLLMSecretUpdates(ctx, updates)
	if err != nil {
		slog.Warn("os_secret_store_write_failed", "scope", "agent_settings", "error", err)
		return nil, nil
	}
	return newSecretIDs, nil
}

type llmSecretUpdate struct {
	node             *yaml.Node
	provider         string
	name             string
	configKey        string
	value            *string
	target           **string
	preserveExisting bool
}

func (o *FileOwner) storeLLMSecretUpdates(
	ctx context.Context,
	updates []llmSecretUpdate,
) ([]string, error) {
	type replacement struct {
		update llmSecretUpdate
		value  string
	}
	var newSecretIDs []string
	var replacements []replacement
	for _, update := range updates {
		value := strings.TrimSpace(*update.value)
		if value == "" {
			if update.preserveExisting {
				if id := osSecretIDFromYAML(update.node, update.provider, update.name); id != "" {
					*update.value = secref.OSSecretRef(id)
				}
			}
			continue
		}
		if isSecretReference(value) {
			continue
		}
		id, err := secref.NewOSSecretID()
		if err != nil {
			deleteOSSecretIDs(ctx, o.osStore, newSecretIDs)
			return nil, fmt.Errorf("%s: %w", update.name, err)
		}
		if err := o.osStore.Put(ctx, id, update.configKey, []byte(value)); err != nil {
			deleteOSSecretIDs(ctx, o.osStore, newSecretIDs)
			return nil, fmt.Errorf("%s: %w", update.name, err)
		}
		newSecretIDs = append(newSecretIDs, id)
		replacements = append(replacements, replacement{update: update, value: secref.OSSecretRef(id)})
	}
	for _, replacement := range replacements {
		if replacement.update.target != nil {
			value := replacement.value
			*replacement.update.target = &value
		} else {
			*replacement.update.value = replacement.value
		}
	}
	return newSecretIDs, nil
}

func llmSecretConfigKey(prefix, field string) string {
	suffix := field
	switch field {
	case "bedrock_aws_key":
		suffix = "bedrock.aws_key"
	case "bedrock_aws_secret":
		suffix = "bedrock.aws_secret"
	case "cloudflare_api_token":
		suffix = "cloudflare.api_token"
	}
	return prefix + "." + suffix
}

func isSecretReference(value string) bool {
	_, ok := secref.ParseSingleRef(value)
	return ok
}

func osSecretIDFromYAML(node *yaml.Node, provider, field string) string {
	for _, current := range agentSettingsYAMLFieldNodes(node, provider, field) {
		ref, ok := secref.ParseSingleRef(strings.TrimSpace(current.Value))
		if ok && ref.Kind == secref.RefKindOS {
			return ref.SecretID
		}
	}
	return ""
}

func (o *FileOwner) persistedView(serialized []byte) (AgentSettingsView, error) {
	expanded, err := readExpandedAgentSettingsConfigWithSource(o.configPath, o.secretSource)
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
	next, envManaged, secretFields := buildAgentSettingsResponseView(next, doc, current)
	skills, err := buildAgentSkillsSettingsPayloadFromReader(expanded)
	if err != nil {
		return AgentSettingsView{}, err
	}
	return AgentSettingsView{
		LLM:          next.LLM,
		EnvManaged:   envManaged,
		SecretFields: secretFields,
		Skills:       skills,
		Tools:        next.Tools,
		MCP:          mcpSettingsFromDocument(doc),
		ConfigPath:   o.configPath,
		ConfigExists: true,
		ConfigValid:  true,
		ConfigSource: "config",
		ReadOnly:     false,
	}, nil
}

func protectManagedFields(configPath string, reader Reader, source secref.Source, update *AgentSettingsUpdate) error {
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
	current, err := readFileSettingsWithSource(configPath, source)
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
	profiles, managedProfiles := buildAgentSettingsProfileResponseView(current.LLM.Profiles, llmNode)
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
	return readFileSettingsWithSource(configPath, nil)
}

func readFileSettingsWithSource(configPath string, source secref.Source) (FileSettings, error) {
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
	tmp, err := readExpandedAgentSettingsConfigWithSource(configPath, source)
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
	settings = normalizeAgentSettingsConfigView(settings, doc)
	settings.MCP = mcpSettingsFromDocument(doc)
	return settings, nil
}

func readExpandedAgentSettingsConfigWithSource(configPath string, source secref.Source) (*viper.Viper, error) {
	tmp := viper.New()
	configdefaults.Apply(tmp)
	if err := configutil.ReadExpandedConfigWithSource(tmp, configPath, source, nil); err != nil {
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

func buildAgentSkillsSettingsResponse(configPath string, configValid bool, source secref.Source) (SkillsSettingsPayload, error) {
	reader := viper.New()
	configdefaults.Apply(reader)
	if configValid {
		if _, err := os.Stat(configPath); err != nil {
			if os.IsNotExist(err) {
				return buildAgentSkillsSettingsPayloadFromReader(reader)
			}
			return SkillsSettingsPayload{}, err
		}
		expanded, err := readExpandedAgentSettingsConfigWithSource(configPath, source)
		if err != nil {
			return SkillsSettingsPayload{}, err
		}
		reader = expanded
	}
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

func MarshalFileSettingsUpdate(configPath string, values AgentSettingsUpdate) ([]byte, error) {
	return marshalFileSettingsUpdateWithSource(configPath, values, nil)
}

func marshalFileSettingsUpdateWithSource(configPath string, values AgentSettingsUpdate, source secref.Source) ([]byte, error) {
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
	if existing, readErr := readFileSettingsWithSource(configPath, source); readErr == nil {
		current = existing
	} else if !isInvalidConfigYAMLError(readErr) && !os.IsNotExist(readErr) {
		return nil, readErr
	}
	if err := applyAgentSettingsUpdateDocument(doc, current, values); err != nil {
		return nil, err
	}
	return configbootstrap.MarshalDocument(doc)
}

func applyAgentSettingsUpdateDocument(doc *yaml.Node, current FileSettings, values AgentSettingsUpdate) error {
	if values.LLM.Profiles != nil && (values.LLM.Profile != nil || values.LLM.DeleteProfile != nil) {
		return fmt.Errorf("profiles cannot be combined with a single profile update")
	}
	if values.LLM.Profile != nil && values.LLM.DeleteProfile != nil {
		return fmt.Errorf("profile and delete_profile cannot be combined")
	}
	nextLLM := applyLLMSettingsUpdate(current.LLM, values.LLM)
	root, err := configbootstrap.DocumentMapping(doc)
	if err != nil {
		return err
	}

	llmNode := configbootstrap.EnsureMappingValue(root, llmSettingsKey)
	applyLLMConfigFieldsUpdate(llmNode, nextLLM.LLMConfigFieldsPayload, values.LLM.LLMConfigFieldsUpdate)
	if values.LLM.Profiles != nil {
		profiles, err := normalizeLLMProfileSettings(*values.LLM.Profiles)
		if err != nil {
			return err
		}
		if err := setLLMProfilesNode(llmNode, profiles); err != nil {
			return err
		}
	}
	if values.LLM.Profile != nil {
		if err := setSingleLLMProfileNode(llmNode, values.LLM.Profile); err != nil {
			return err
		}
	}
	if values.LLM.DeleteProfile != nil {
		if err := deleteSingleLLMProfileNode(llmNode, *values.LLM.DeleteProfile); err != nil {
			return err
		}
	}
	fallbackProfiles := values.LLM.FallbackProfiles
	if values.LLM.Profile != nil {
		originalName := strings.TrimSpace(values.LLM.Profile.OriginalName)
		nextName := strings.TrimSpace(values.LLM.Profile.Name)
		if originalName != "" && originalName != nextName {
			next := append([]string(nil), current.LLM.FallbackProfiles...)
			for i := range next {
				if strings.EqualFold(strings.TrimSpace(next[i]), originalName) {
					next[i] = nextName
				}
			}
			fallbackProfiles = &next
		}
	}
	if values.LLM.DeleteProfile != nil {
		name := strings.TrimSpace(*values.LLM.DeleteProfile)
		next := make([]string, 0, len(current.LLM.FallbackProfiles))
		for _, fallback := range current.LLM.FallbackProfiles {
			if !strings.EqualFold(strings.TrimSpace(fallback), name) {
				next = append(next, fallback)
			}
		}
		fallbackProfiles = &next
	}
	if fallbackProfiles != nil {
		setMainLoopFallbackProfilesNode(llmNode, *fallbackProfiles)
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
	if values.MCP != nil && values.MCP.Servers != nil {
		servers, err := normalizeMCPServers(*values.MCP.Servers)
		if err != nil {
			return err
		}
		mcpNode := configbootstrap.EnsureMappingValue(root, mcpSettingsKey)
		if err := setMCPServersNode(mcpNode, servers); err != nil {
			return err
		}
	}
	return nil
}

func mcpSettingsFromDocument(doc *yaml.Node) MCPSettingsPayload {
	root, err := configbootstrap.DocumentMapping(doc)
	if err != nil {
		return MCPSettingsPayload{}
	}
	mcpNode := configbootstrap.FindMappingValue(root, mcpSettingsKey)
	serversNode := configbootstrap.FindMappingValue(mcpNode, "servers")
	if serversNode == nil {
		return MCPSettingsPayload{}
	}
	var raw any
	if err := serversNode.Decode(&raw); err != nil {
		return MCPSettingsPayload{}
	}
	return MCPSettingsPayload{Servers: mcphost.ParseServers(raw)}
}

func mcpSettingsFromReader(reader Reader) MCPSettingsPayload {
	if reader == nil {
		return MCPSettingsPayload{}
	}
	current := viper.New()
	if err := current.MergeConfigMap(reader.AllSettings()); err != nil {
		return MCPSettingsPayload{}
	}
	return MCPSettingsPayload{Servers: mcphost.MCPConfigFromReader(current)}
}

func normalizeMCPServers(values []MCPServerSettings) ([]MCPServerSettings, error) {
	servers := make([]MCPServerSettings, len(values))
	seen := make(map[string]struct{}, len(values))
	for i, value := range values {
		value.Name = strings.TrimSpace(value.Name)
		value.Type = strings.ToLower(strings.TrimSpace(value.Type))
		if value.Type == "" {
			value.Type = "stdio"
		}
		value.Command = strings.TrimSpace(value.Command)
		value.URL = strings.TrimSpace(value.URL)
		value.Args = append([]string(nil), value.Args...)
		value.Env = cloneStringMap(value.Env)
		value.Headers = cloneStringMap(value.Headers)
		value.AllowedTools = normalizeMCPStringList(value.AllowedTools)

		nameKey := strings.ToLower(value.Name)
		if _, exists := seen[nameKey]; exists {
			return nil, fmt.Errorf("mcp server name %q is duplicated", value.Name)
		}
		seen[nameKey] = struct{}{}
		if err := validateMCPMapKeys("environment variable", value.Env); err != nil {
			return nil, fmt.Errorf("mcp server %q: %w", value.Name, err)
		}
		if err := validateMCPMapKeys("header", value.Headers); err != nil {
			return nil, fmt.Errorf("mcp server %q: %w", value.Name, err)
		}
		if value.Enable {
			if err := value.Validate(); err != nil {
				return nil, err
			}
		} else if value.Name == "" {
			return nil, fmt.Errorf("mcp server name is required")
		} else if value.Type != "stdio" && value.Type != "http" {
			return nil, fmt.Errorf("mcp server %q: unsupported type %q (supported: stdio, http)", value.Name, value.Type)
		}
		servers[i] = value
	}
	return servers, nil
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[strings.TrimSpace(key)] = value
	}
	return cloned
}

func validateMCPMapKeys(kind string, values map[string]string) error {
	for key := range values {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("%s name is required", kind)
		}
	}
	return nil
}

func normalizeMCPStringList(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func setMCPServersNode(mcpNode *yaml.Node, servers []MCPServerSettings) error {
	var node yaml.Node
	if err := node.Encode(servers); err != nil {
		return err
	}
	if current := configbootstrap.FindMappingValue(mcpNode, "servers"); current != nil {
		*current = node
		return nil
	}
	mcpNode.Content = append(mcpNode.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "servers"},
		&node,
	)
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
	snapshot := req.LLM
	if targetProfile == "" || strings.EqualFold(targetProfile, llmutil.RouteProfileDefault) {
		var err error
		snapshot, err = resolveAgentSettingsLLMFromReader(reader, LLMSettingsPayloadAsNonEmptyUpdate(req.LLM))
		if err != nil {
			return LLMSettingsPayload{}, err
		}
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

func stringPointer(value string) *string {
	next := value
	return &next
}

func toolEnabledUpdateValue(update *ToolEnabledUpdate) *bool {
	if update == nil {
		return nil
	}
	return update.Enabled
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

func setSingleLLMProfileNode(llmNode *yaml.Node, update *LLMProfileUpdate) error {
	normalized, err := normalizeLLMProfileSettings([]LLMProfileSettingsPayload{
		update.LLMProfileSettingsPayload,
	})
	if err != nil {
		return err
	}
	profile := normalized[0]
	originalName := strings.TrimSpace(update.OriginalName)
	profilesNode := configbootstrap.FindMappingValue(llmNode, "profiles")
	targetIndex := -1
	if originalName != "" && profilesNode != nil && profilesNode.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(profilesNode.Content); i += 2 {
			if strings.EqualFold(strings.TrimSpace(profilesNode.Content[i].Value), originalName) {
				targetIndex = i
				break
			}
		}
	}
	if originalName != "" && targetIndex < 0 {
		return fmt.Errorf("profile %q not found", originalName)
	}
	if profilesNode != nil && profilesNode.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(profilesNode.Content); i += 2 {
			if i != targetIndex && strings.EqualFold(strings.TrimSpace(profilesNode.Content[i].Value), profile.Name) {
				return fmt.Errorf("duplicate profile %q", profile.Name)
			}
		}
	}

	var profileNode *yaml.Node
	if targetIndex >= 0 {
		profilesNode.Content[targetIndex].Value = profile.Name
		profileNode = profilesNode.Content[targetIndex+1]
	} else {
		profilesNode = configbootstrap.EnsureMappingValue(llmNode, "profiles")
		profileNode = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		profilesNode.Content = append(profilesNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: profile.Name},
			profileNode,
		)
	}

	fields := llmProfileSettingsAsUpdate(profile)
	if !update.secretFieldProvided("api_key", update.APIKey) {
		fields.APIKey = nil
	}
	if !update.secretFieldProvided("bedrock_aws_key", update.BedrockAWSKey) {
		fields.BedrockAWSKey = nil
	}
	if !update.secretFieldProvided("bedrock_aws_secret", update.BedrockAWSSecret) {
		fields.BedrockAWSSecret = nil
	}
	if !update.secretFieldProvided("cloudflare_api_token", update.CloudflareAPIToken) {
		fields.CloudflareAPIToken = nil
	}
	applyLLMConfigFieldsUpdate(profileNode, profile.LLMConfigFieldsPayload, fields)
	return nil
}

func deleteSingleLLMProfileNode(llmNode *yaml.Node, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("profile name is required")
	}
	profilesNode := configbootstrap.FindMappingValue(llmNode, "profiles")
	if profilesNode != nil && profilesNode.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(profilesNode.Content); i += 2 {
			if !strings.EqualFold(strings.TrimSpace(profilesNode.Content[i].Value), name) {
				continue
			}
			profilesNode.Content = append(profilesNode.Content[:i], profilesNode.Content[i+2:]...)
			if len(profilesNode.Content) == 0 {
				configbootstrap.DeleteMappingKey(llmNode, "profiles")
			}
			return nil
		}
	}
	return fmt.Errorf("profile %q not found", name)
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

func setLLMProfilesNode(llmNode *yaml.Node, profiles []LLMProfileSettingsPayload) error {
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
) (FileSettings, EnvManagedPayload, SecretFieldsPayload) {
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
	)
	if len(envManaged.LLM) == 0 {
		envManaged.LLM = nil
	}
	if len(envManaged.LLMProfiles) == 0 {
		envManaged.LLMProfiles = nil
	}
	secretFields := buildAgentSecretFields(settings.LLM, doc, envManaged)
	redactAgentSettingsSecrets(&settings.LLM)
	return settings, envManaged, secretFields
}

func buildAgentSettingsProfileResponseView(
	profiles []LLMProfileSettingsPayload,
	llmNode *yaml.Node,
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
		profileFields := ResolveInferenceProviderSettingsFields(out[i].LLMConfigFieldsPayload)
		fields := applyAgentSettingsYAMLEnvManaged(
			&out[i].LLMConfigFieldsPayload,
			nil,
			profileNode,
			strings.TrimSpace(profileFields.Provider),
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
	provider string,
) map[string]EnvManagedField {
	if fields == nil {
		return envManaged
	}
	if _, ok := envManaged["inference_provider"]; !ok {
		if field, ok := YAMLManagedField(node, provider, "inference_provider"); ok {
			if envManaged == nil {
				envManaged = map[string]EnvManagedField{}
			}
			envManaged["inference_provider"] = field
		}
	}
	if _, ok := envManaged["provider"]; !ok {
		if field, ok := YAMLManagedField(node, provider, "provider"); ok {
			if envManaged == nil {
				envManaged = map[string]EnvManagedField{}
			}
			envManaged["provider"] = field
		}
	}
	effectiveProvider := firstNonEmpty(strings.TrimSpace(fields.Provider), provider)
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
	for _, current := range agentSettingsYAMLFieldNodes(node, provider, field) {
		entry, ok := YAMLPlaceholderField(current, field)
		if ok {
			return entry, true
		}
	}
	return EnvManagedField{}, false
}

func agentSettingsYAMLFieldNodes(node *yaml.Node, provider, field string) []*yaml.Node {
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
	nodes := make([]*yaml.Node, 0, len(fieldPathSets))
	for _, path := range fieldPathSets {
		current := node
		for _, key := range path {
			current = configbootstrap.FindMappingValue(current, key)
			if current == nil {
				break
			}
		}
		if current != nil {
			nodes = append(nodes, current)
		}
	}
	return nodes
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

func buildAgentSecretFields(settings LLMSettingsPayload, doc *yaml.Node, managed EnvManagedPayload) SecretFieldsPayload {
	out := SecretFieldsPayload{}
	llmNode := agentSettingsYAMLLLMNode(doc)
	out.LLM = mergeManagedSecretStatuses(llmSecretFieldStatuses(llmNode, settings.Provider), managed.LLM)
	profilesNode := configbootstrap.FindMappingValue(llmNode, "profiles")
	for _, profile := range settings.Profiles {
		name := strings.TrimSpace(profile.Name)
		if name == "" {
			continue
		}
		profileNode := configbootstrap.FindMappingValue(profilesNode, name)
		statuses := mergeManagedSecretStatuses(llmSecretFieldStatuses(profileNode, profile.Provider), managed.LLMProfiles[name])
		if len(statuses) == 0 {
			continue
		}
		if out.LLMProfiles == nil {
			out.LLMProfiles = map[string]map[string]SecretFieldStatus{}
		}
		out.LLMProfiles[name] = statuses
	}
	return out
}

func mergeManagedSecretStatuses(statuses map[string]SecretFieldStatus, managed map[string]EnvManagedField) map[string]SecretFieldStatus {
	for _, field := range llmSecretFieldNames {
		entry, ok := managed[field]
		if !ok {
			continue
		}
		if statuses == nil {
			statuses = map[string]SecretFieldStatus{}
		}
		source := strings.TrimSpace(entry.Source)
		if source == "" {
			source = string(secref.RefKindEnv)
		}
		statuses[field] = SecretFieldStatus{Configured: true, Source: source, Editable: false}
	}
	return statuses
}

func llmSecretFieldStatuses(node *yaml.Node, provider string) map[string]SecretFieldStatus {
	statuses := map[string]SecretFieldStatus{}
	for _, field := range llmSecretFieldNames {
		for _, current := range agentSettingsYAMLFieldNodes(node, provider, field) {
			value := strings.TrimSpace(current.Value)
			if value == "" {
				continue
			}
			status := SecretFieldStatus{Configured: true, Source: "file", Editable: true}
			if ref, ok := secref.ParseSingleRef(value); ok {
				switch ref.Kind {
				case secref.RefKindEnv:
					status.Source = string(secref.RefKindEnv)
					status.Editable = false
				case secref.RefKindAWSSecretsManager:
					status.Source = string(secref.RefKindAWSSecretsManager)
					status.Editable = false
				case secref.RefKindOS:
					status.Source = string(secref.RefKindOS)
				}
			}
			statuses[field] = status
			break
		}
	}
	if len(statuses) == 0 {
		return nil
	}
	return statuses
}

func redactAgentSettingsSecrets(settings *LLMSettingsPayload) {
	if settings == nil {
		return
	}
	redactLLMConfigFields(&settings.LLMConfigFieldsPayload)
	for i := range settings.Profiles {
		redactLLMConfigFields(&settings.Profiles[i].LLMConfigFieldsPayload)
	}
}

func redactLLMConfigFields(fields *LLMConfigFieldsPayload) {
	if fields == nil {
		return
	}
	fields.APIKey = ""
	fields.BedrockAWSKey = ""
	fields.BedrockAWSSecret = ""
	fields.CloudflareAPIToken = ""
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
