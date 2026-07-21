package main

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/mcphost"
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

func TestRegistryRuntimeResolverPrepareClosesHostReturnedWithConnectError(t *testing.T) {
	connectErr := errors.New("connect MCP")
	closeErr := errors.New("close partial MCP")
	var closeCalls atomic.Int32
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

	err := resolver.Prepare(context.Background())
	if !errors.Is(err, connectErr) || !errors.Is(err, closeErr) {
		t.Fatalf("Prepare() error = %v, want connect and close errors", err)
	}
	if got := closeCalls.Load(); got != 1 {
		t.Fatalf("MCP host close calls = %d, want 1", got)
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
