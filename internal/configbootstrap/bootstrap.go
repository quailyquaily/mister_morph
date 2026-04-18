package configbootstrap

import (
	"strings"

	"github.com/quailyquaily/mistermorph/integration"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

type LLMConfig struct {
	Provider            string
	Endpoint            string
	Model               string
	APIKey              string
	CloudflareAccountID string
	CloudflareAPIToken  string
}

type ConsoleEndpoint struct {
	Name      string
	URL       string
	AuthToken string
}

type ConsoleConfig struct {
	Listen       string
	BasePath     string
	Password     string
	ManagedKinds []string
	Endpoints    []ConsoleEndpoint
}

type Config struct {
	FileStateDir    string
	ServerAuthToken string
	LLM             LLMConfig
	Console         *ConsoleConfig
}

func Apply(base []byte, cfg Config) ([]byte, error) {
	doc, err := loadDocument(base)
	if err != nil {
		return nil, err
	}
	root, err := DocumentMapping(doc)
	if err != nil {
		return nil, err
	}
	if dir := strings.TrimSpace(cfg.FileStateDir); dir != "" {
		SetOrDeleteMappingScalar(root, "file_state_dir", dir)
	}

	values := defaultRuntimeValues()
	applyAgentDefaults(root, values, cfg.LLM)

	if consoleCfg := cfg.Console; consoleCfg != nil {
		applyConsoleConfig(root, *consoleCfg)
	}
	if token := strings.TrimSpace(cfg.ServerAuthToken); token != "" {
		serverNode := EnsureMappingValue(root, "server")
		SetOrDeleteMappingScalar(serverNode, "auth_token", token)
	}

	return MarshalDocument(doc)
}

type runtimeValues struct {
	Provider          string
	MultimodalSources []string
	ToolsWriteFile    bool
	ToolsSpawn        bool
	ToolsContactsSend bool
	ToolsTodoUpdate   bool
	ToolsPlanCreate   bool
	ToolsURLFetch     bool
	ToolsWebSearch    bool
	ToolsBash         bool
	ToolsPowerShell   bool
}

func loadDocument(base []byte) (*yaml.Node, error) {
	return LoadDocumentBytes(base)
}

func defaultRuntimeValues() runtimeValues {
	tmp := viper.New()
	integration.ApplyViperDefaults(tmp)
	return runtimeValues{
		Provider:          strings.TrimSpace(tmp.GetString("llm.provider")),
		MultimodalSources: append([]string(nil), tmp.GetStringSlice("multimodal.image.sources")...),
		ToolsWriteFile:    tmp.GetBool("tools.write_file.enabled"),
		ToolsSpawn:        tmp.GetBool("tools.spawn.enabled"),
		ToolsContactsSend: tmp.GetBool("tools.contacts_send.enabled"),
		ToolsTodoUpdate:   tmp.GetBool("tools.todo_update.enabled"),
		ToolsPlanCreate:   tmp.GetBool("tools.plan_create.enabled"),
		ToolsURLFetch:     tmp.GetBool("tools.url_fetch.enabled"),
		ToolsWebSearch:    tmp.GetBool("tools.web_search.enabled"),
		ToolsBash:         tmp.GetBool("tools.bash.enabled"),
		ToolsPowerShell:   tmp.GetBool("tools.powershell.enabled"),
	}
}

func applyAgentDefaults(root *yaml.Node, values runtimeValues, cfg LLMConfig) {
	llmNode := EnsureMappingValue(root, "llm")
	provider := strings.TrimSpace(cfg.Provider)
	if provider == "" {
		provider = strings.TrimSpace(values.Provider)
	}
	SetOrDeleteMappingScalar(llmNode, "provider", provider)
	SetOrDeleteMappingScalar(llmNode, "endpoint", strings.TrimSpace(cfg.Endpoint))
	SetOrDeleteMappingScalar(llmNode, "model", strings.TrimSpace(cfg.Model))

	if strings.EqualFold(provider, "cloudflare") {
		SetOrDeleteMappingScalar(llmNode, "api_key", "")
		cloudflareNode := EnsureMappingValue(llmNode, "cloudflare")
		SetOrDeleteMappingScalar(cloudflareNode, "account_id", strings.TrimSpace(cfg.CloudflareAccountID))
		SetOrDeleteMappingScalar(cloudflareNode, "api_token", strings.TrimSpace(cfg.CloudflareAPIToken))
		if len(cloudflareNode.Content) == 0 {
			DeleteMappingKey(llmNode, "cloudflare")
		}
	} else {
		SetOrDeleteMappingScalar(llmNode, "api_key", strings.TrimSpace(cfg.APIKey))
		DeleteMappingKey(llmNode, "cloudflare")
	}

	multimodalNode := EnsureMappingValue(root, "multimodal")
	imageNode := EnsureMappingValue(multimodalNode, "image")
	SetMappingStringList(imageNode, "sources", normalizeLowercaseList(values.MultimodalSources))

	toolsNode := EnsureMappingValue(root, "tools")
	SetMappingBoolPath(toolsNode, "write_file", "enabled", values.ToolsWriteFile)
	SetMappingBoolPath(toolsNode, "spawn", "enabled", values.ToolsSpawn)
	SetMappingBoolPath(toolsNode, "contacts_send", "enabled", values.ToolsContactsSend)
	SetMappingBoolPath(toolsNode, "todo_update", "enabled", values.ToolsTodoUpdate)
	SetMappingBoolPath(toolsNode, "plan_create", "enabled", values.ToolsPlanCreate)
	SetMappingBoolPath(toolsNode, "url_fetch", "enabled", values.ToolsURLFetch)
	SetMappingBoolPath(toolsNode, "web_search", "enabled", values.ToolsWebSearch)
	SetMappingBoolPath(toolsNode, "bash", "enabled", values.ToolsBash)
	SetMappingBoolPath(toolsNode, "powershell", "enabled", values.ToolsPowerShell)
}

func applyConsoleConfig(root *yaml.Node, cfg ConsoleConfig) {
	consoleNode := EnsureMappingValue(root, "console")
	if listen := strings.TrimSpace(cfg.Listen); listen != "" {
		SetOrDeleteMappingScalar(consoleNode, "listen", listen)
	}
	if basePath := strings.TrimSpace(cfg.BasePath); basePath != "" {
		SetOrDeleteMappingScalar(consoleNode, "base_path", basePath)
	}
	if password := strings.TrimSpace(cfg.Password); password != "" {
		SetOrDeleteMappingScalar(consoleNode, "password", password)
	}
	if cfg.ManagedKinds != nil {
		SetMappingStringList(consoleNode, "managed_runtimes", normalizeTrimmedList(cfg.ManagedKinds))
	}
	setConsoleEndpoints(consoleNode, cfg.Endpoints)
}

func setConsoleEndpoints(consoleNode *yaml.Node, endpoints []ConsoleEndpoint) {
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
		SetOrDeleteMappingScalar(item, "name", name)
		SetOrDeleteMappingScalar(item, "url", url)
		SetOrDeleteMappingScalar(item, "auth_token", authToken)
		list.Content = append(list.Content, item)
	}
	if existing := FindMappingValue(consoleNode, "endpoints"); existing != nil {
		*existing = *list
		return
	}
	consoleNode.Content = append(consoleNode.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "endpoints"},
		list,
	)
}

func normalizeTrimmedList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
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

func normalizeLowercaseList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(strings.ToLower(raw))
		if value == "" {
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
