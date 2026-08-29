package configbootstrap

import (
	"strings"

	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

type LLMConfig struct {
	InferenceProvider   string
	Endpoint            string
	Model               string
	APIKey              string
	PricingFile         string
	CloudflareAccountID string
	CloudflareAPIToken  string
}

type Config struct {
	FileStateDir string
	LLM          LLMConfig
}

func Apply(base []byte, cfg Config) ([]byte, error) {
	doc, err := LoadDocumentBytes(base)
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

	consoleNode := EnsureMappingValue(root, "console")
	SetMappingStringList(consoleNode, "managed_runtimes", nil)
	SetMappingStringList(consoleNode, "endpoints", nil)

	return MarshalDocument(doc)
}

type runtimeValues struct {
	ToolsWriteFile    bool
	ToolsSpawn        bool
	ToolsCoder        bool
	ToolsContactsSend bool
	ToolsTodoUpdate   bool
	ToolsPlanCreate   bool
	ToolsURLFetch     bool
	ToolsWebSearch    bool
	ToolsBash         bool
	ToolsPowerShell   bool
}

func defaultRuntimeValues() runtimeValues {
	tmp := viper.New()
	configdefaults.Apply(tmp)
	return runtimeValues{
		ToolsWriteFile:    tmp.GetBool("tools.write_file.enabled"),
		ToolsSpawn:        tmp.GetBool("tools.spawn.enabled"),
		ToolsCoder:        tmp.GetBool("tools.coder.enabled"),
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
	inferenceProvider := strings.TrimSpace(cfg.InferenceProvider)
	SetOrDeleteMappingScalar(llmNode, "inference_provider", inferenceProvider)
	DeleteMappingKey(llmNode, "provider")
	SetOrDeleteMappingScalar(llmNode, "endpoint", strings.TrimSpace(cfg.Endpoint))
	SetOrDeleteMappingScalar(llmNode, "model", strings.TrimSpace(cfg.Model))
	SetOrDeleteMappingScalar(llmNode, "pricing_file", strings.TrimSpace(cfg.PricingFile))

	if strings.EqualFold(inferenceProvider, "cloudflare") {
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
	DeleteMappingKey(llmNode, "azure")
	DeleteMappingKey(llmNode, "bedrock")

	DeleteMappingKey(root, "multimodal")

	toolsNode := EnsureMappingValue(root, "tools")
	SetMappingBoolPath(toolsNode, "write_file", "enabled", values.ToolsWriteFile)
	SetMappingBoolPath(toolsNode, "spawn", "enabled", values.ToolsSpawn)
	SetMappingBoolPath(toolsNode, "coder", "enabled", values.ToolsCoder)
	SetMappingBoolPath(toolsNode, "contacts_send", "enabled", values.ToolsContactsSend)
	SetMappingBoolPath(toolsNode, "todo_update", "enabled", values.ToolsTodoUpdate)
	SetMappingBoolPath(toolsNode, "plan_create", "enabled", values.ToolsPlanCreate)
	SetMappingBoolPath(toolsNode, "url_fetch", "enabled", values.ToolsURLFetch)
	SetMappingBoolPath(toolsNode, "web_search", "enabled", values.ToolsWebSearch)
	SetMappingBoolPath(toolsNode, "bash", "enabled", values.ToolsBash)
	SetMappingBoolPath(toolsNode, "powershell", "enabled", values.ToolsPowerShell)
}
