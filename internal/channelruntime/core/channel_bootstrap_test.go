package core

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type channelBootstrapClient struct {
	seq        int
	closeCalls int
}

func (c *channelBootstrapClient) Chat(context.Context, llm.Request) (llm.Result, error) {
	return llm.Result{}, nil
}

func (c *channelBootstrapClient) Close() error {
	c.closeCalls++
	return nil
}

func TestBootstrapChannelRuntimeReusesMainClientForSameAddressingProfile(t *testing.T) {
	created := []*channelBootstrapClient{}
	bundle, err := BootstrapChannelRuntime(context.Background(), channelBootstrapDeps("main", "main", &created), ChannelBootstrapOptions{
		Mode: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 {
		t.Fatalf("created clients = %d, want 1", len(created))
	}
	if bundle.AddressingClient != bundle.TaskRuntime.BootstrapMainClient {
		t.Fatalf("addressing client should reuse main client for same profile")
	}
	bundle.Cleanup()
	bundle.Cleanup()
	if created[0].closeCalls != 1 {
		t.Fatalf("main close calls = %d, want 1", created[0].closeCalls)
	}
}

func TestBootstrapChannelRuntimeCreatesAddressingClientForDifferentProfile(t *testing.T) {
	created := []*channelBootstrapClient{}
	bundle, err := BootstrapChannelRuntime(context.Background(), channelBootstrapDeps("main", "addressing", &created), ChannelBootstrapOptions{
		Mode: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 2 {
		t.Fatalf("created clients = %d, want 2", len(created))
	}
	if bundle.AddressingClient == bundle.TaskRuntime.BootstrapMainClient {
		t.Fatalf("addressing client should differ from main client for different profile")
	}
	if bundle.AddressingModel != "addressing-model" {
		t.Fatalf("addressing model = %q, want %q", bundle.AddressingModel, "addressing-model")
	}
	bundle.Cleanup()
	bundle.Cleanup()
	for _, client := range created {
		if client.closeCalls != 1 {
			t.Fatalf("client %d close calls = %d, want 1", client.seq, client.closeCalls)
		}
	}
}

func TestBootstrapChannelRuntimeCleanupCancelsGenerationContext(t *testing.T) {
	created := []*channelBootstrapClient{}
	bundle, err := BootstrapChannelRuntime(context.Background(), channelBootstrapDeps("main", "main", &created), ChannelBootstrapOptions{
		Mode: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-bundle.done:
		t.Fatal("generation context canceled before cleanup")
	default:
	}

	bundle.Cleanup()
	select {
	case <-bundle.done:
	default:
		t.Fatal("generation context remains active after cleanup")
	}
}

func TestBootstrapChannelRuntimeClosesTaskRuntimeOnAddressingRouteFailure(t *testing.T) {
	created := []*channelBootstrapClient{}
	deps := channelBootstrapDeps("main", "addressing", &created)
	resolve := deps.ResolveLLMRoute
	deps.ResolveLLMRoute = func(purpose string) (llmutil.ResolvedRoute, error) {
		if purpose == llmutil.RoutePurposeAddressing {
			return llmutil.ResolvedRoute{}, errors.New("resolve addressing")
		}
		return resolve(purpose)
	}

	if _, err := BootstrapChannelRuntime(context.Background(), deps, ChannelBootstrapOptions{Mode: "test"}); err == nil {
		t.Fatal("BootstrapChannelRuntime() error = nil, want failure")
	}
	if len(created) != 1 || created[0].closeCalls != 1 {
		t.Fatalf("created clients = %#v, want one closed task client", created)
	}
}

func TestBootstrapChannelRuntimeDoesNotCloseSharedInstanceTwice(t *testing.T) {
	shared := &channelBootstrapClient{seq: 1}
	created := []*channelBootstrapClient{}
	deps := channelBootstrapDeps("main", "addressing", &created)
	deps.CreateLLMClient = func(llmutil.ResolvedRoute) (llm.Client, error) {
		created = append(created, shared)
		return shared, nil
	}

	bundle, err := BootstrapChannelRuntime(context.Background(), deps, ChannelBootstrapOptions{Mode: "test"})
	if err != nil {
		t.Fatalf("BootstrapChannelRuntime() error = %v", err)
	}
	bundle.Cleanup()
	bundle.Cleanup()
	if shared.closeCalls != 1 {
		t.Fatalf("shared close calls = %d, want 1", shared.closeCalls)
	}
}

func TestBootstrapChannelRuntimeDoesNotCloseSharedInspectedInstanceTwice(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)
	shared := &channelBootstrapClient{seq: 1}
	created := []*channelBootstrapClient{}
	deps := channelBootstrapDeps("main", "addressing", &created)
	deps.CreateLLMClient = func(llmutil.ResolvedRoute) (llm.Client, error) {
		created = append(created, shared)
		return shared, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bundle, err := BootstrapChannelRuntime(ctx, deps, ChannelBootstrapOptions{
		Mode:          "test",
		InspectPrompt: true,
	})
	if err != nil {
		t.Fatalf("BootstrapChannelRuntime() error = %v", err)
	}
	bundle.Cleanup()
	bundle.Cleanup()
	if shared.closeCalls != 1 {
		t.Fatalf("shared inspected close calls = %d, want 1", shared.closeCalls)
	}
}

func channelBootstrapDeps(mainProfile, addressingProfile string, created *[]*channelBootstrapClient) depsutil.CommonDependencies {
	return depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			return slog.Default(), nil
		},
		ResolveLLMRoute: func(purpose string) (llmutil.ResolvedRoute, error) {
			profile := mainProfile
			model := "main-model"
			if purpose == llmutil.RoutePurposeAddressing {
				profile = addressingProfile
				model = "addressing-model"
			}
			return llmutil.ResolvedRoute{
				Purpose: purpose,
				Profile: profile,
				ClientConfig: llmconfig.ClientConfig{
					Provider: "test",
					Model:    model,
				},
			}, nil
		},
		CreateLLMClient: func(route llmutil.ResolvedRoute) (llm.Client, error) {
			client := &channelBootstrapClient{seq: len(*created) + 1}
			*created = append(*created, client)
			return client, nil
		},
		Registry: func() *tools.Registry {
			return tools.NewRegistry()
		},
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), nil, nil
		},
	}
}
