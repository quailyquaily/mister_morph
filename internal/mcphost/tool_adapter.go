package mcphost

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolAdapter wraps an MCP server tool as a tools.Tool implementation.
type ToolAdapter struct {
	serverName   string
	originalName string
	description  string
	inputSchema  string
	session      *mcp.ClientSession
}

// NamespacedToolName returns "mcp_<server>__<tool>".
func NamespacedToolName(serverName, toolName string) string {
	return "mcp_" + serverName + "__" + toolName
}

func newToolAdapter(serverName string, tool *mcp.Tool, session *mcp.ClientSession) (*ToolAdapter, error) {
	schemaJSON := "{}"
	if tool.InputSchema != nil {
		b, err := json.Marshal(tool.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("marshal input schema for tool %q: %w", tool.Name, err)
		}
		schemaJSON = string(b)
	}

	return &ToolAdapter{
		serverName:   serverName,
		originalName: tool.Name,
		description:  strings.TrimSpace(tool.Description),
		inputSchema:  schemaJSON,
		session:      session,
	}, nil
}

func (t *ToolAdapter) Name() string {
	return NamespacedToolName(t.serverName, t.originalName)
}

func (t *ToolAdapter) Description() string {
	return fmt.Sprintf("[mcp:%s] %s", t.serverName, t.description)
}

func (t *ToolAdapter) ParameterSchema() string {
	return t.inputSchema
}

func (t *ToolAdapter) Execute(ctx context.Context, params map[string]any) (string, error) {
	result, err := t.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      t.originalName,
		Arguments: params,
	})
	if err != nil {
		return "", fmt.Errorf("mcp tool %q call failed: %w", t.Name(), err)
	}
	text := formatCallToolResult(result)
	if result != nil && result.IsError {
		return text, fmt.Errorf("mcp tool %q returned error: %s", t.Name(), text)
	}
	return text, nil
}

func formatCallToolResult(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}

	var parts []string
	for _, c := range result.Content {
		switch v := c.(type) {
		case *mcp.TextContent:
			parts = append(parts, v.Text)
		case *mcp.ImageContent:
			parts = append(parts, fmt.Sprintf("[image: %s, %d bytes]", v.MIMEType, len(v.Data)))
		case *mcp.AudioContent:
			parts = append(parts, fmt.Sprintf("[audio: %s, %d bytes]", v.MIMEType, len(v.Data)))
		case *mcp.EmbeddedResource:
			if v.Resource != nil {
				if v.Resource.Text != "" {
					parts = append(parts, v.Resource.Text)
				} else {
					parts = append(parts, fmt.Sprintf("[resource: %s]", v.Resource.URI))
				}
			}
		default:
			b, err := json.Marshal(c)
			if err == nil {
				parts = append(parts, string(b))
			}
		}
	}

	return strings.Join(parts, "\n")
}
