package mcphost

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/quailyquaily/mistermorph/tools"
)

type Host struct {
	mu       sync.Mutex
	sessions []*serverSession
	tools    []tools.Tool
	logger   *slog.Logger
}

type serverSession struct {
	name    string
	session *mcp.ClientSession
}

// Connect creates an MCPHost, connects to all configured MCP servers,
// discovers tools, and returns the host. Individual server failures are
// logged and skipped.
func Connect(ctx context.Context, configs []ServerConfig, logger *slog.Logger) (*Host, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if len(configs) == 0 {
		return nil, nil
	}

	h := &Host{logger: logger}

	for i := range configs {
		cfg := &configs[i]
		if err := cfg.Validate(); err != nil {
			logger.Warn("mcp_server_config_invalid", "server", cfg.Name, "err", err)
			continue
		}

		session, serverTools, err := h.connectServer(ctx, cfg)
		if err != nil {
			logger.Warn("mcp_server_connect_failed", "server", cfg.Name, "err", err)
			continue
		}

		h.sessions = append(h.sessions, &serverSession{
			name:    cfg.Name,
			session: session,
		})
		h.tools = append(h.tools, serverTools...)

		toolNames := make([]string, len(serverTools))
		for i, t := range serverTools {
			toolNames[i] = t.Name()
		}
		logger.Info("mcp_tools_loaded",
			"server", cfg.Name,
			"count", len(serverTools),
			"tools", toolNames,
		)
	}

	if len(h.sessions) == 0 {
		return nil, nil
	}

	return h, nil
}

func (h *Host) connectServer(ctx context.Context, cfg *ServerConfig) (*mcp.ClientSession, []tools.Tool, error) {
	client := mcp.NewClient(
		&mcp.Implementation{Name: "mistermorph", Version: "1.0"},
		nil,
	)

	transport, err := h.buildTransport(cfg)
	if err != nil {
		return nil, nil, err
	}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("connect: %w", err)
	}

	toolsList, err := session.ListTools(ctx, nil)
	if err != nil {
		_ = session.Close()
		return nil, nil, fmt.Errorf("list tools: %w", err)
	}

	allowedSet := cfg.AllowedToolSet()

	var adapted []tools.Tool
	for _, t := range toolsList.Tools {
		if allowedSet != nil && !allowedSet[t.Name] {
			continue
		}
		adapter, err := newToolAdapter(cfg.Name, t, session)
		if err != nil {
			h.logger.Warn("mcp_tool_adapt_failed",
				"server", cfg.Name,
				"tool", t.Name,
				"err", err,
			)
			continue
		}
		adapted = append(adapted, adapter)
	}

	return session, adapted, nil
}

func (h *Host) buildTransport(cfg *ServerConfig) (mcp.Transport, error) {
	transport := strings.ToLower(strings.TrimSpace(cfg.Transport))
	if transport == "" {
		transport = "stdio"
	}

	switch transport {
	case "stdio":
		return h.buildStdioTransport(cfg), nil
	case "sse":
		return h.buildSSETransport(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported transport: %s", transport)
	}
}

func (h *Host) buildStdioTransport(cfg *ServerConfig) mcp.Transport {
	cmd := exec.Command(cfg.Command, cfg.Args...)
	if len(cfg.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range cfg.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	return &mcp.CommandTransport{Command: cmd}
}

func (h *Host) buildSSETransport(cfg *ServerConfig) mcp.Transport {
	transport := &mcp.SSEClientTransport{
		Endpoint: cfg.URL,
	}

	headers := cfg.ExpandedHeaders()
	if len(headers) > 0 {
		transport.HTTPClient = &http.Client{
			Transport: &headerInjector{
				base:    http.DefaultTransport,
				headers: headers,
			},
		}
	}

	return transport
}

// Tools returns all adapted MCP tools.
func (h *Host) Tools() []tools.Tool {
	if h == nil {
		return nil
	}
	return h.tools
}

// Close gracefully closes all MCP sessions.
func (h *Host) Close() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	var firstErr error
	for _, s := range h.sessions {
		if err := s.session.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	h.sessions = nil
	h.tools = nil
	return firstErr
}

// RegisterTools connects to all configured MCP servers and registers
// discovered tools into reg. Returns the host (for cleanup) or nil if
// no MCP servers are configured.
func RegisterTools(ctx context.Context, configs []ServerConfig, reg *tools.Registry, logger *slog.Logger) (*Host, error) {
	if len(configs) == 0 {
		return nil, nil
	}
	host, err := Connect(ctx, configs, logger)
	if err != nil {
		return nil, err
	}
	if host == nil {
		return nil, nil
	}
	for _, t := range host.Tools() {
		reg.Register(t)
	}
	return host, nil
}

// headerInjector is an http.RoundTripper that injects custom headers.
type headerInjector struct {
	base    http.RoundTripper
	headers map[string]string
}

func (h *headerInjector) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}
	return h.base.RoundTrip(req)
}
