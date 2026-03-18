package consolecmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/integration"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const (
	llmSettingsKey        = "llm"
	multimodalSettingsKey = "multimodal"
	toolsSettingsKey      = "tools"
)

var supportedMultimodalSources = []string{"telegram", "slack", "line", "remote_download"}

type llmSettingsPayload struct {
	Provider           string `json:"provider"`
	Endpoint           string `json:"endpoint"`
	Model              string `json:"model"`
	APIKey             string `json:"api_key"`
	ReasoningEffort    string `json:"reasoning_effort"`
	ToolsEmulationMode string `json:"tools_emulation_mode"`
}

type multimodalSettingsPayload struct {
	ImageSources []string `json:"image_sources"`
}

type toolsSettingsPayload struct {
	WriteFileEnabled    bool `json:"write_file_enabled"`
	ContactsSendEnabled bool `json:"contacts_send_enabled"`
	TodoUpdateEnabled   bool `json:"todo_update_enabled"`
	PlanCreateEnabled   bool `json:"plan_create_enabled"`
	URLFetchEnabled     bool `json:"url_fetch_enabled"`
	WebSearchEnabled    bool `json:"web_search_enabled"`
	BashEnabled         bool `json:"bash_enabled"`
}

type agentSettingsPayload struct {
	LLM        llmSettingsPayload        `json:"llm"`
	Multimodal multimodalSettingsPayload `json:"multimodal"`
	Tools      toolsSettingsPayload      `json:"tools"`
}

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
	settings, err := readAgentSettings(configPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"llm":         settings.LLM,
		"multimodal":  settings.Multimodal,
		"tools":       settings.Tools,
		"config_path": configPath,
	})
}

func (s *server) handleAgentSettingsPut(w http.ResponseWriter, r *http.Request) {
	var req agentSettingsPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	configPath, err := resolveConsoleConfigPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	serialized, err := writeAgentSettings(configPath, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tmp, err := validateAgentConfigDocument(serialized)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := os.WriteFile(configPath, serialized, 0o600); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	viper.Set(llmSettingsKey, tmp.Get(llmSettingsKey))
	viper.Set(multimodalSettingsKey, tmp.Get(multimodalSettingsKey))
	viper.Set(toolsSettingsKey, tmp.Get(toolsSettingsKey))
	if s != nil && s.localRuntime != nil {
		if err := s.localRuntime.ReloadAgentConfig(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if s != nil && s.managed != nil {
		if err := s.managed.Restart(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	next := readAgentSettingsFromReader(tmp)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"llm":         next.LLM,
		"multimodal":  next.Multimodal,
		"tools":       next.Tools,
		"config_path": configPath,
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
			return readAgentSettingsFromReader(viper.GetViper()), nil
		}
		return agentSettingsPayload{}, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return readAgentSettingsFromReader(viper.GetViper()), nil
	}
	tmp := viper.New()
	integration.ApplyViperDefaults(tmp)
	tmp.SetConfigType("yaml")
	if err := tmp.ReadConfig(bytes.NewReader(data)); err != nil {
		return agentSettingsPayload{}, fmt.Errorf("invalid config yaml: %w", err)
	}
	return readAgentSettingsFromReader(tmp), nil
}

func writeAgentSettings(configPath string, values agentSettingsPayload) ([]byte, error) {
	doc, err := loadYAMLDocument(configPath)
	if err != nil {
		return nil, err
	}
	root, err := documentMapping(doc)
	if err != nil {
		return nil, err
	}

	llmNode := ensureMappingValue(root, llmSettingsKey)
	setOrDeleteMappingScalar(llmNode, "provider", values.LLM.Provider)
	setOrDeleteMappingScalar(llmNode, "endpoint", values.LLM.Endpoint)
	setOrDeleteMappingScalar(llmNode, "model", values.LLM.Model)
	setOrDeleteMappingScalar(llmNode, "api_key", values.LLM.APIKey)
	setOrDeleteMappingScalar(llmNode, "reasoning_effort", values.LLM.ReasoningEffort)
	setOrDeleteMappingScalar(llmNode, "tools_emulation_mode", values.LLM.ToolsEmulationMode)

	multimodalNode := ensureMappingValue(root, multimodalSettingsKey)
	imageNode := ensureMappingValue(multimodalNode, "image")
	setMappingStringList(imageNode, "sources", values.Multimodal.ImageSources)

	toolsNode := ensureMappingValue(root, toolsSettingsKey)
	setMappingBoolPath(toolsNode, "write_file", "enabled", values.Tools.WriteFileEnabled)
	setMappingBoolPath(toolsNode, "contacts_send", "enabled", values.Tools.ContactsSendEnabled)
	setMappingBoolPath(toolsNode, "todo_update", "enabled", values.Tools.TodoUpdateEnabled)
	setMappingBoolPath(toolsNode, "plan_create", "enabled", values.Tools.PlanCreateEnabled)
	setMappingBoolPath(toolsNode, "url_fetch", "enabled", values.Tools.URLFetchEnabled)
	setMappingBoolPath(toolsNode, "web_search", "enabled", values.Tools.WebSearchEnabled)
	setMappingBoolPath(toolsNode, "bash", "enabled", values.Tools.BashEnabled)

	return marshalYAMLDocument(doc)
}

func validateAgentConfigDocument(data []byte) (*viper.Viper, error) {
	tmp := viper.New()
	integration.ApplyViperDefaults(tmp)
	tmp.SetConfigType("yaml")
	if err := tmp.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("invalid config yaml: %w", err)
	}
	values := llmutil.RuntimeValuesFromReader(tmp)
	route, err := llmutil.ResolveRoute(values, llmutil.RoutePurposeMainLoop)
	if err != nil {
		return nil, err
	}
	if _, err := llmutil.ClientFromConfigWithValues(route.ClientConfig, route.Values); err != nil {
		return nil, err
	}
	return tmp, nil
}

func loadYAMLDocument(configPath string) (*yaml.Node, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &yaml.Node{
				Kind: yaml.DocumentNode,
				Content: []*yaml.Node{{
					Kind: yaml.MappingNode,
					Tag:  "!!map",
				}},
			}, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return &yaml.Node{
			Kind: yaml.DocumentNode,
			Content: []*yaml.Node{{
				Kind: yaml.MappingNode,
				Tag:  "!!map",
			}},
		}, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("invalid config yaml: %w", err)
	}
	if len(doc.Content) == 0 {
		doc.Content = []*yaml.Node{{
			Kind: yaml.MappingNode,
			Tag:  "!!map",
		}}
	}
	return &doc, nil
}

func documentMapping(doc *yaml.Node) (*yaml.Node, error) {
	if doc == nil {
		return nil, fmt.Errorf("config document is nil")
	}
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
		}
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config root must be a yaml mapping")
	}
	return doc, nil
}

func marshalYAMLDocument(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func findMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if strings.EqualFold(strings.TrimSpace(node.Content[i].Value), key) {
			return node.Content[i+1]
		}
	}
	return nil
}

func ensureMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	if value := findMappingValue(node, key); value != nil {
		if value.Kind == yaml.MappingNode {
			return value
		}
		*value = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		return value
	}
	child := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		child,
	)
	return child
}

func setOrDeleteMappingScalar(node *yaml.Node, key, value string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	value = strings.TrimSpace(value)
	for i := 0; i+1 < len(node.Content); i += 2 {
		if !strings.EqualFold(strings.TrimSpace(node.Content[i].Value), key) {
			continue
		}
		if value == "" {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			return
		}
		node.Content[i+1] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
		return
	}
	if value == "" {
		return
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}

func setMappingBoolPath(node *yaml.Node, section, key string, value bool) {
	sectionNode := ensureMappingValue(node, section)
	if sectionNode == nil {
		return
	}
	for i := 0; i+1 < len(sectionNode.Content); i += 2 {
		if !strings.EqualFold(strings.TrimSpace(sectionNode.Content[i].Value), key) {
			continue
		}
		sectionNode.Content[i+1] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: boolString(value)}
		return
	}
	sectionNode.Content = append(sectionNode.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: boolString(value)},
	)
}

func setMappingStringList(node *yaml.Node, key string, values []string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	list := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
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

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func readAgentSettingsFromReader(r interface {
	GetString(string) string
	GetStringSlice(string) []string
	GetBool(string) bool
}) agentSettingsPayload {
	if r == nil {
		return agentSettingsPayload{}
	}
	return agentSettingsPayload{
		LLM: llmSettingsPayload{
			Provider:           strings.TrimSpace(r.GetString("llm.provider")),
			Endpoint:           strings.TrimSpace(r.GetString("llm.endpoint")),
			Model:              strings.TrimSpace(r.GetString("llm.model")),
			APIKey:             strings.TrimSpace(r.GetString("llm.api_key")),
			ReasoningEffort:    strings.TrimSpace(r.GetString("llm.reasoning_effort")),
			ToolsEmulationMode: strings.TrimSpace(r.GetString("llm.tools_emulation_mode")),
		},
		Multimodal: multimodalSettingsPayload{
			ImageSources: sanitizeMultimodalSources(r.GetStringSlice("multimodal.image.sources")),
		},
		Tools: toolsSettingsPayload{
			WriteFileEnabled:    r.GetBool("tools.write_file.enabled"),
			ContactsSendEnabled: r.GetBool("tools.contacts_send.enabled"),
			TodoUpdateEnabled:   r.GetBool("tools.todo_update.enabled"),
			PlanCreateEnabled:   r.GetBool("tools.plan_create.enabled"),
			URLFetchEnabled:     r.GetBool("tools.url_fetch.enabled"),
			WebSearchEnabled:    r.GetBool("tools.web_search.enabled"),
			BashEnabled:         r.GetBool("tools.bash.enabled"),
		},
	}
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
