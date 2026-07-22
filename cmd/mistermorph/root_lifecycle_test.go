package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/mcphost"
	"github.com/quailyquaily/mistermorph/tools"
)

func TestRegistryRuntimeResolverPrepareUsesCallerContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var gotContext context.Context
	resolver := newRegistryRuntimeResolver()
	resolver.mcpConfigs = func() []mcphost.ServerConfig {
		return []mcphost.ServerConfig{{Name: "test", Enable: true}}
	}
	resolver.connectMCP = func(ctx context.Context, _ []mcphost.ServerConfig, _ *slog.Logger) (*mcphost.Host, error) {
		gotContext = ctx
		return nil, nil
	}

	if err := resolver.Prepare(ctx); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if gotContext != ctx {
		t.Fatal("MCP connect did not receive the command context")
	}
	if gotContext.Err() != context.Canceled {
		t.Fatalf("MCP connect context error = %v, want context canceled", gotContext.Err())
	}
}

func TestRegistryRuntimeResolverPrepareContinuesAfterMCPConnectError(t *testing.T) {
	connectErr := errors.New("connect MCP")
	closeErr := errors.New("close partial MCP")
	var closeCalls atomic.Int32
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	host := &mcphost.Host{}
	resolver := newRegistryRuntimeResolver()
	resolver.mcpConfigs = func() []mcphost.ServerConfig {
		return []mcphost.ServerConfig{{Name: "test", Enable: true}}
	}
	resolver.connectMCP = func(context.Context, []mcphost.ServerConfig, *slog.Logger) (*mcphost.Host, error) {
		return host, connectErr
	}
	resolver.closeMCP = func(got *mcphost.Host) error {
		if got != host {
			t.Fatalf("close host = %p, want %p", got, host)
		}
		closeCalls.Add(1)
		return closeErr
	}

	if err := resolver.Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare() error = %v, want nil", err)
	}
	if got := closeCalls.Load(); got != 1 {
		t.Fatalf("MCP host close calls = %d, want 1", got)
	}
	if registry := resolver.Registry(); registry == nil {
		t.Fatal("Registry() = nil, want built-in tools after MCP failure")
	} else if _, ok := registry.Get("read_file"); !ok {
		t.Fatal("Registry() does not contain read_file after MCP failure")
	}
	if registry := resolver.AwarenessRegistry(); registry == nil {
		t.Fatal("AwarenessRegistry() = nil, want built-in tools after MCP failure")
	} else if _, ok := registry.Get("read_file"); !ok {
		t.Fatal("AwarenessRegistry() does not contain read_file after MCP failure")
	}
	logOutput := logs.String()
	for _, want := range []string{"mcp_init_failed", connectErr.Error(), closeErr.Error()} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("warning log = %q, want %q", logOutput, want)
		}
	}
}

func TestRegistryRuntimeResolverPrepareContinuesAfterMCPRegistrationError(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprintf("registry_%d", failAt), func(t *testing.T) {
			registerErr := errors.New("register MCP")
			var logs bytes.Buffer
			previousLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() {
				slog.SetDefault(previousLogger)
			})

			host := &mcphost.Host{}
			resolver := newRegistryRuntimeResolver()
			resolver.mcpConfigs = func() []mcphost.ServerConfig {
				return []mcphost.ServerConfig{{Name: "test", Enable: true}}
			}
			resolver.connectMCP = func(context.Context, []mcphost.ServerConfig, *slog.Logger) (*mcphost.Host, error) {
				return host, nil
			}
			registerCalls := 0
			resolver.registerMCP = func(got *mcphost.Host, _ *tools.Registry) error {
				if got != host {
					t.Fatalf("register host = %p, want %p", got, host)
				}
				registerCalls++
				if registerCalls == failAt {
					return registerErr
				}
				return nil
			}

			if err := resolver.Prepare(context.Background()); err != nil {
				t.Fatalf("Prepare() error = %v, want nil", err)
			}
			if registerCalls != failAt {
				t.Fatalf("MCP register calls = %d, want %d", registerCalls, failAt)
			}
			if resolver.mcpHost != nil {
				t.Fatal("resolver retained MCP host after registration failure")
			}
			if registry := resolver.Registry(); registry == nil {
				t.Fatal("Registry() = nil, want built-in tools after MCP registration failure")
			} else if _, ok := registry.Get("read_file"); !ok {
				t.Fatal("Registry() does not contain read_file after MCP registration failure")
			}
			if registry := resolver.AwarenessRegistry(); registry == nil {
				t.Fatal("AwarenessRegistry() = nil, want built-in tools after MCP registration failure")
			} else if _, ok := registry.Get("read_file"); !ok {
				t.Fatal("AwarenessRegistry() does not contain read_file after MCP registration failure")
			}
			logOutput := logs.String()
			for _, want := range []string{"mcp_init_failed", registerErr.Error()} {
				if !strings.Contains(logOutput, want) {
					t.Fatalf("warning log = %q, want %q", logOutput, want)
				}
			}
		})
	}
}

func TestRegistryRuntimeResolverAccessDoesNotStartMCP(t *testing.T) {
	var connectCalls atomic.Int32
	resolver := newRegistryRuntimeResolver()
	resolver.mcpConfigs = func() []mcphost.ServerConfig {
		return []mcphost.ServerConfig{{Name: "test", Enable: true}}
	}
	resolver.connectMCP = func(context.Context, []mcphost.ServerConfig, *slog.Logger) (*mcphost.Host, error) {
		connectCalls.Add(1)
		return nil, nil
	}

	if registry := resolver.Registry(); registry != nil {
		t.Fatalf("Registry() before Prepare = %#v, want nil", registry)
	}
	if registry := resolver.AwarenessRegistry(); registry != nil {
		t.Fatalf("AwarenessRegistry() before Prepare = %#v, want nil", registry)
	}
	if got := connectCalls.Load(); got != 0 {
		t.Fatalf("MCP connect calls before Prepare = %d, want 0", got)
	}
}

func TestRootRuntimeCloseClosesMCPHostOnce(t *testing.T) {
	var closeCalls atomic.Int32
	resolver := newRegistryRuntimeResolver()
	resolver.mcpHost = &mcphost.Host{}
	resolver.closeMCP = func(*mcphost.Host) error {
		closeCalls.Add(1)
		return nil
	}
	runtime := &rootRuntime{registryResolver: resolver}

	if err := runtime.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if got := closeCalls.Load(); got != 1 {
		t.Fatalf("MCP host close calls = %d, want 1", got)
	}
}
