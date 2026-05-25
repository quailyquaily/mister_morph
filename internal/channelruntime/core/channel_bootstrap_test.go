package core

import (
	"context"
	"log/slog"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type channelBootstrapClient struct {
	seq int
}

func (c *channelBootstrapClient) Chat(context.Context, llm.Request) (llm.Result, error) {
	return llm.Result{}, nil
}

func TestBootstrapChannelRuntimeReusesMainClientForSameAddressingProfile(t *testing.T) {
	created := []*channelBootstrapClient{}
	bundle, err := BootstrapChannelRuntime(context.Background(), channelBootstrapDeps("main", "main", &created), ChannelBootstrapOptions{
		Mode: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Cleanup()

	if len(created) != 1 {
		t.Fatalf("created clients = %d, want 1", len(created))
	}
	if bundle.AddressingClient != bundle.TaskRuntime.BootstrapMainClient {
		t.Fatalf("addressing client should reuse main client for same profile")
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
	defer bundle.Cleanup()

	if len(created) != 2 {
		t.Fatalf("created clients = %d, want 2", len(created))
	}
	if bundle.AddressingClient == bundle.TaskRuntime.BootstrapMainClient {
		t.Fatalf("addressing client should differ from main client for different profile")
	}
	if bundle.AddressingModel != "addressing-model" {
		t.Fatalf("addressing model = %q, want %q", bundle.AddressingModel, "addressing-model")
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
	}
}
