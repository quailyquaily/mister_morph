package mcphost

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type ServerConfig struct {
	Name         string
	Type         string            // "stdio" (default) | "http"
	Command      string            // stdio only
	Args         []string          // stdio only
	Env          map[string]string // stdio only
	URL          string            // http only
	Headers      map[string]string // http only: custom HTTP headers (auth etc.)
	AllowedTools []string          // whitelist; empty = all
}

func (c *ServerConfig) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("mcp server name is required")
	}
	typ := strings.ToLower(strings.TrimSpace(c.Type))
	if typ == "" {
		typ = "stdio"
	}
	switch typ {
	case "stdio":
		if strings.TrimSpace(c.Command) == "" {
			return fmt.Errorf("mcp server %q: command is required for stdio transport", c.Name)
		}
	case "http":
		if strings.TrimSpace(c.URL) == "" {
			return fmt.Errorf("mcp server %q: url is required for http transport", c.Name)
		}
	default:
		return fmt.Errorf("mcp server %q: unsupported type %q (supported: stdio, http)", c.Name, typ)
	}
	return nil
}

// AllowedToolSet returns a set of allowed tool names for fast lookup.
// Returns nil if no whitelist is configured (all tools allowed).
func (c *ServerConfig) AllowedToolSet() map[string]bool {
	if len(c.AllowedTools) == 0 {
		return nil
	}
	set := make(map[string]bool, len(c.AllowedTools))
	for _, name := range c.AllowedTools {
		name = strings.TrimSpace(name)
		if name != "" {
			set[name] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// MCPConfigFromViper reads MCP server configs from the global viper instance.
func MCPConfigFromViper() []ServerConfig {
	return parseMCPServers(viper.Get("mcp.servers"))
}

// MCPConfigFromReader reads MCP server configs from a local viper instance,
// preserving the integration library's config isolation guarantees.
func MCPConfigFromReader(v *viper.Viper) []ServerConfig {
	if v == nil {
		return nil
	}
	return parseMCPServers(v.Get("mcp.servers"))
}

func parseMCPServers(raw any) []ServerConfig {
	if raw == nil {
		return nil
	}

	items, ok := raw.([]any)
	if !ok {
		return nil
	}

	var configs []ServerConfig
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		cfg := ServerConfig{
			Name:    asString(m["name"]),
			Type:    asString(m["type"]),
			Command: asString(m["command"]),
			URL:     asString(m["url"]),
		}
		if args, ok := m["args"].([]any); ok {
			for _, a := range args {
				if s, ok := a.(string); ok {
					cfg.Args = append(cfg.Args, s)
				}
			}
		}
		if env, ok := m["env"].(map[string]any); ok {
			cfg.Env = make(map[string]string, len(env))
			for k, v := range env {
				cfg.Env[k] = fmt.Sprintf("%v", v)
			}
		}
		if headers, ok := m["headers"].(map[string]any); ok {
			cfg.Headers = make(map[string]string, len(headers))
			for k, v := range headers {
				cfg.Headers[k] = fmt.Sprintf("%v", v)
			}
		}
		if allowed, ok := m["allowed_tools"].([]any); ok {
			for _, a := range allowed {
				if s, ok := a.(string); ok {
					cfg.AllowedTools = append(cfg.AllowedTools, s)
				}
			}
		}
		configs = append(configs, cfg)
	}
	return configs
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
