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
	Command            string
	Args               []string
	Env                map[string]string
	ProfileCWD         string
	CWD                string
	ReadRoots          []string
	WriteRoots         []string
	SessionOptionsMeta map[string]any
}

func (c AgentConfig) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("acp agent name is required")
	}
	if strings.TrimSpace(c.Command) == "" {
		return fmt.Errorf("acp agent %q: command is required", c.Name)
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

func CloneAgents(in []AgentConfig) []AgentConfig {
	if len(in) == 0 {
		return nil
	}
	out := make([]AgentConfig, 0, len(in))
	for _, cfg := range in {
		item := cfg
		item.Args = append([]string(nil), cfg.Args...)
		item.Env = cloneStringMap(cfg.Env)
		item.ReadRoots = append([]string(nil), cfg.ReadRoots...)
		item.WriteRoots = append([]string(nil), cfg.WriteRoots...)
		item.SessionOptions = cloneAnyMap(cfg.SessionOptions)
		out = append(out, item)
	}
	return out
}

func PrepareAgentConfig(cfg AgentConfig, overrideCWD string) (PreparedAgentConfig, error) {
	if err := cfg.Validate(); err != nil {
		return PreparedAgentConfig{}, err
	}

	profileCWD, err := resolveAbsoluteDir(strings.TrimSpace(cfg.CWD), "")
	if err != nil {
		return PreparedAgentConfig{}, fmt.Errorf("resolve acp cwd: %w", err)
	}
	profileCWD, err = freezePreparedPath(profileCWD)
	if err != nil {
		return PreparedAgentConfig{}, fmt.Errorf("resolve acp cwd: %w", err)
	}

	readRoots, err := resolveRoots(profileCWD, cfg.ReadRoots)
	if err != nil {
		return PreparedAgentConfig{}, err
	}
	writeRoots, err := resolveRoots(profileCWD, cfg.WriteRoots)
	if err != nil {
		return PreparedAgentConfig{}, err
	}
	allowedRoots := collectAllowedRoots(profileCWD, readRoots, writeRoots)

	resolvedCWD := profileCWD
	if strings.TrimSpace(overrideCWD) != "" {
		resolvedCWD, err = resolveAbsoluteDir(strings.TrimSpace(overrideCWD), profileCWD)
		if err != nil {
			return PreparedAgentConfig{}, fmt.Errorf("resolve acp cwd: %w", err)
		}
		resolvedCWD, err = freezePreparedPath(resolvedCWD)
		if err != nil {
			return PreparedAgentConfig{}, fmt.Errorf("resolve acp cwd: %w", err)
		}
	}
	if _, err := resolveAllowedPath(resolvedCWD, allowedRoots); err != nil {
		return PreparedAgentConfig{}, fmt.Errorf("resolve acp cwd: %w", err)
	}

	return PreparedAgentConfig{
		Name:               strings.TrimSpace(cfg.Name),
		Command:            strings.TrimSpace(cfg.Command),
		Args:               append([]string(nil), cfg.Args...),
		Env:                cloneStringMap(cfg.Env),
		ProfileCWD:         profileCWD,
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
		frozenRoot, err := freezePreparedPath(absRoot)
		if err != nil {
			return nil, fmt.Errorf("resolve acp root %q: %w", raw, err)
		}
		if _, ok := seen[frozenRoot]; ok {
			continue
		}
		seen[frozenRoot] = struct{}{}
		out = append(out, frozenRoot)
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

func resolveAbsoluteDir(raw string, relativeBase string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "" {
		path = "."
	}
	if !filepath.IsAbs(path) && strings.TrimSpace(relativeBase) != "" {
		path = filepath.Join(relativeBase, path)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("acp cwd %q is not a directory", absPath)
	}
	return absPath, nil
}

func freezePreparedPath(path string) (string, error) {
	resolved, err := resolveRealPath(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func collectAllowedRoots(profileCWD string, readRoots []string, writeRoots []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 1+len(readRoots)+len(writeRoots))
	for _, root := range append([]string{profileCWD}, append(readRoots, writeRoots...)...) {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		out = append(out, root)
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
