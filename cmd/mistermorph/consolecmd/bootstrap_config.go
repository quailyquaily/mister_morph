package consolecmd

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/integration"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

type BootstrapLLMConfig struct {
	Provider            string
	Endpoint            string
	Model               string
	APIKey              string
	CloudflareAccountID string
	CloudflareAPIToken  string
}

type BootstrapConsoleEndpoint struct {
	Name      string
	URL       string
	AuthToken string
}

type BootstrapConsoleConfig struct {
	Listen       string
	BasePath     string
	Password     string
	ManagedKinds []string
	Endpoints    []BootstrapConsoleEndpoint
}

type BootstrapConfig struct {
	FileStateDir    string
	ServerAuthToken string
	LLM             BootstrapLLMConfig
	Console         *BootstrapConsoleConfig
}

func ApplyBootstrapConfig(base []byte, cfg BootstrapConfig) ([]byte, error) {
	doc, err := loadBootstrapDocument(base)
	if err != nil {
		return nil, err
	}
	root, err := documentMapping(doc)
	if err != nil {
		return nil, err
	}
	if dir := strings.TrimSpace(cfg.FileStateDir); dir != "" {
		setOrDeleteMappingScalar(root, "file_state_dir", dir)
	}

	currentAgent, err := readBootstrapAgentSettings(base)
	if err != nil {
		return nil, err
	}
	if err := applyAgentSettingsUpdateDocument(doc, currentAgent, buildBootstrapAgentUpdate(cfg.LLM)); err != nil {
		return nil, err
	}

	if consoleCfg := cfg.Console; consoleCfg != nil {
		applyBootstrapConsoleConfig(root, *consoleCfg)
	}
	if token := strings.TrimSpace(cfg.ServerAuthToken); token != "" {
		serverNode := ensureMappingValue(root, "server")
		setOrDeleteMappingScalar(serverNode, "auth_token", token)
	}

	return marshalYAMLDocument(doc)
}

func loadBootstrapDocument(base []byte) (*yaml.Node, error) {
	if len(bytes.TrimSpace(base)) == 0 {
		return newEmptyYAMLDocument(), nil
	}
	return loadYAMLDocumentBytes(base)
}

func readBootstrapAgentSettings(base []byte) (agentSettingsPayload, error) {
	if len(bytes.TrimSpace(base)) == 0 {
		return defaultAgentSettingsPayload(), nil
	}
	tmp := viper.New()
	integration.ApplyViperDefaults(tmp)
	tmp.SetConfigType("yaml")
	if err := tmp.ReadConfig(bytes.NewReader(base)); err != nil {
		return agentSettingsPayload{}, fmt.Errorf("invalid config yaml: %w", err)
	}
	return readAgentSettingsFromReader(tmp), nil
}

func buildBootstrapAgentUpdate(cfg BootstrapLLMConfig) agentSettingsUpdatePayload {
	defaults := defaultAgentSettingsPayload()
	provider := strings.TrimSpace(cfg.Provider)
	if provider == "" {
		provider = defaults.LLM.Provider
	}
	return agentSettingsUpdatePayload{
		LLM: llmSettingsUpdatePayload{
			llmConfigFieldsUpdatePayload: llmConfigFieldsUpdatePayload{
				Provider:            stringPointer(provider),
				Endpoint:            stringPointer(strings.TrimSpace(cfg.Endpoint)),
				Model:               stringPointer(strings.TrimSpace(cfg.Model)),
				APIKey:              stringPointer(strings.TrimSpace(cfg.APIKey)),
				CloudflareAccountID: stringPointer(strings.TrimSpace(cfg.CloudflareAccountID)),
				CloudflareAPIToken:  stringPointer(strings.TrimSpace(cfg.CloudflareAPIToken)),
			},
		},
		Multimodal: &multimodalSettingsUpdatePayload{
			ImageSources: stringSlicePointer(defaults.Multimodal.ImageSources),
		},
		Tools: &toolsSettingsUpdatePayload{
			WriteFile:    toolEnabledUpdatePayloadPointer(defaults.Tools.WriteFile.Enabled),
			Spawn:        toolEnabledUpdatePayloadPointer(defaults.Tools.Spawn.Enabled),
			ContactsSend: toolEnabledUpdatePayloadPointer(defaults.Tools.ContactsSend.Enabled),
			TodoUpdate:   toolEnabledUpdatePayloadPointer(defaults.Tools.TodoUpdate.Enabled),
			PlanCreate:   toolEnabledUpdatePayloadPointer(defaults.Tools.PlanCreate.Enabled),
			URLFetch:     toolEnabledUpdatePayloadPointer(defaults.Tools.URLFetch.Enabled),
			WebSearch:    toolEnabledUpdatePayloadPointer(defaults.Tools.WebSearch.Enabled),
			Bash:         toolEnabledUpdatePayloadPointer(defaults.Tools.Bash.Enabled),
			PowerShell:   toolEnabledUpdatePayloadPointer(defaults.Tools.PowerShell.Enabled),
		},
	}
}

func applyBootstrapConsoleConfig(root *yaml.Node, cfg BootstrapConsoleConfig) {
	consoleNode := ensureMappingValue(root, consoleSettingsKey)
	if listen := strings.TrimSpace(cfg.Listen); listen != "" {
		setOrDeleteMappingScalar(consoleNode, "listen", listen)
	}
	if basePath := strings.TrimSpace(cfg.BasePath); basePath != "" {
		setOrDeleteMappingScalar(consoleNode, "base_path", basePath)
	}
	if password := strings.TrimSpace(cfg.Password); password != "" {
		setOrDeleteMappingScalar(consoleNode, "password", password)
	}
	if cfg.ManagedKinds != nil {
		setMappingOrderedStringList(consoleNode, "managed_runtimes", cfg.ManagedKinds)
	}
	setBootstrapConsoleEndpoints(consoleNode, cfg.Endpoints)
}

func setBootstrapConsoleEndpoints(consoleNode *yaml.Node, endpoints []BootstrapConsoleEndpoint) {
	if consoleNode == nil || consoleNode.Kind != yaml.MappingNode {
		return
	}
	list := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, endpoint := range endpoints {
		name := strings.TrimSpace(endpoint.Name)
		url := strings.TrimSpace(endpoint.URL)
		authToken := strings.TrimSpace(endpoint.AuthToken)
		if name == "" || url == "" || authToken == "" {
			continue
		}
		item := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setOrDeleteMappingScalar(item, "name", name)
		setOrDeleteMappingScalar(item, "url", url)
		setOrDeleteMappingScalar(item, "auth_token", authToken)
		list.Content = append(list.Content, item)
	}
	for i := 0; i+1 < len(consoleNode.Content); i += 2 {
		if !strings.EqualFold(strings.TrimSpace(consoleNode.Content[i].Value), "endpoints") {
			continue
		}
		consoleNode.Content[i+1] = list
		return
	}
	consoleNode.Content = append(consoleNode.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "endpoints"},
		list,
	)
}
