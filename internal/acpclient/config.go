package acpclient

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cast"
	"github.com/spf13/viper"
)

type AgentConfig struct {
	Name           string
	Enable         bool
	Type           string
	Command        string
	Args           []string
	Env            map[string]string
	CWD            string
	ReadRoots      []string
	WriteRoots     []string
	SessionOptions map[string]any
}

type PreparedAgentConfig struct {
	Name               string
	Type               string
	Command            string
	Args               []string
	Env                map[string]string
	CWD                string
	ReadRoots          []string
	WriteRoots         []string
	SessionOptionsMeta map[string]any
}

func (c AgentConfig) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("acp agent name is required")
	}
	typ := strings.ToLower(strings.TrimSpace(c.Type))
	if typ == "" {
		typ = "stdio"
	}
	switch typ {
	case "stdio":
		if strings.TrimSpace(c.Command) == "" {
			return fmt.Errorf("acp agent %q: command is required for stdio transport", c.Name)
		}
	default:
		return fmt.Errorf("acp agent %q: unsupported type %q (supported: stdio)", c.Name, typ)
	}
	return nil
}

func AgentsFromViper() []AgentConfig {
	return parseAgents(viper.Get("acp.agents"))
}

func AgentsFromReader(v *viper.Viper) []AgentConfig {
	if v == nil {
		return nil
	}
	return parseAgents(v.Get("acp.agents"))
}

func FindAgent(configs []AgentConfig, name string) (AgentConfig, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return AgentConfig{}, false
	}
	for _, cfg := range configs {
		if strings.ToLower(strings.TrimSpace(cfg.Name)) != name {
			continue
		}
		return cfg, true
	}
	return AgentConfig{}, false
}

func PrepareAgentConfig(cfg AgentConfig, overrideCWD string) (PreparedAgentConfig, error) {
	if err := cfg.Validate(); err != nil {
		return PreparedAgentConfig{}, err
	}
	if !cfg.Enable {
		return PreparedAgentConfig{}, fmt.Errorf("acp agent %q is disabled", strings.TrimSpace(cfg.Name))
	}

	cwd := strings.TrimSpace(overrideCWD)
	if cwd == "" {
		cwd = strings.TrimSpace(cfg.CWD)
	}
	if cwd == "" {
		cwd = "."
	}
	resolvedCWD, err := filepath.Abs(cwd)
	if err != nil {
		return PreparedAgentConfig{}, fmt.Errorf("resolve acp cwd: %w", err)
	}
	info, err := os.Stat(resolvedCWD)
	if err != nil {
		return PreparedAgentConfig{}, fmt.Errorf("stat acp cwd: %w", err)
	}
	if !info.IsDir() {
		return PreparedAgentConfig{}, fmt.Errorf("acp cwd %q is not a directory", resolvedCWD)
	}

	readRoots, err := resolveRoots(resolvedCWD, cfg.ReadRoots)
	if err != nil {
		return PreparedAgentConfig{}, err
	}
	writeRoots, err := resolveRoots(resolvedCWD, cfg.WriteRoots)
	if err != nil {
		return PreparedAgentConfig{}, err
	}

	return PreparedAgentConfig{
		Name:               strings.TrimSpace(cfg.Name),
		Type:               strings.ToLower(strings.TrimSpace(cfg.Type)),
		Command:            strings.TrimSpace(cfg.Command),
		Args:               append([]string(nil), cfg.Args...),
		Env:                cloneStringMap(cfg.Env),
		CWD:                resolvedCWD,
		ReadRoots:          readRoots,
		WriteRoots:         writeRoots,
		SessionOptionsMeta: cloneAnyMap(cfg.SessionOptions),
	}, nil
}

func parseAgents(raw any) []AgentConfig {
	if raw == nil {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	configs := make([]AgentConfig, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		configs = append(configs, AgentConfig{
			Name:           strings.TrimSpace(cast.ToString(m["name"])),
			Enable:         m["enable"] == nil || cast.ToBool(m["enable"]),
			Type:           strings.TrimSpace(cast.ToString(m["type"])),
			Command:        strings.TrimSpace(cast.ToString(m["command"])),
			Args:           cleanStrings(cast.ToStringSlice(m["args"])),
			Env:            cast.ToStringMapString(m["env"]),
			CWD:            strings.TrimSpace(cast.ToString(m["cwd"])),
			ReadRoots:      cleanStrings(cast.ToStringSlice(m["read_roots"])),
			WriteRoots:     cleanStrings(cast.ToStringSlice(m["write_roots"])),
			SessionOptions: cloneAnyMap(cast.ToStringMap(m["session_options"])),
		})
	}
	return configs
}

func resolveRoots(cwd string, roots []string) ([]string, error) {
	if len(roots) == 0 {
		return []string{cwd}, nil
	}
	out := make([]string, 0, len(roots))
	seen := map[string]struct{}{}
	for _, raw := range roots {
		root := strings.TrimSpace(raw)
		if root == "" {
			continue
		}
		if !filepath.IsAbs(root) {
			root = filepath.Join(cwd, root)
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("resolve acp root %q: %w", raw, err)
		}
		if _, ok := seen[absRoot]; ok {
			continue
		}
		seen[absRoot] = struct{}{}
		out = append(out, absRoot)
	}
	if len(out) == 0 {
		return []string{cwd}, nil
	}
	return out, nil
}

func cleanStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		out[key] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		out[key] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
