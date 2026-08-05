package core

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/llminspect"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/llm"
)

type ChannelBootstrapOptions struct {
	Mode                string
	InspectPrompt       bool
	InspectRequest      bool
	AgentConfig         agent.Config
	EngineToolsConfig   *agent.EngineToolsConfig
	MemoryEnabled       bool
	MemoryShortTermDays int
	Logger              *slog.Logger
}

type ChannelRuntimeBundle struct {
	TaskRuntime      *taskruntime.Runtime
	AddressingRoute  llmutil.ResolvedRoute
	AddressingClient llm.Client
	AddressingModel  string
	Memory           MemoryRuntime
	Cleanup          func()
	done             <-chan struct{}
}

func BootstrapChannelRuntime(ctx context.Context, d depsutil.CommonDependencies, opts ChannelBootstrapOptions) (ChannelRuntimeBundle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	mode := strings.TrimSpace(opts.Mode)
	if mode == "" {
		mode = "channel"
	}
	var requestInspector *llminspect.RequestInspector
	var promptInspector *llminspect.PromptInspector
	cleanupInspectors := func() {
		if promptInspector != nil {
			_ = promptInspector.Close()
		}
		if requestInspector != nil {
			_ = requestInspector.Close()
		}
	}

	var err error
	if opts.InspectRequest {
		requestInspector, err = llminspect.NewRequestInspector(llminspect.Options{
			Mode:            mode,
			Task:            mode,
			TimestampFormat: "20060102_150405",
		})
		if err != nil {
			return ChannelRuntimeBundle{}, err
		}
	}
	if opts.InspectPrompt {
		promptInspector, err = llminspect.NewPromptInspector(llminspect.Options{
			Mode:            mode,
			Task:            mode,
			TimestampFormat: "20060102_150405",
		})
		if err != nil {
			cleanupInspectors()
			return ChannelRuntimeBundle{}, err
		}
	}
	decorateRuntimeClient := func(client llm.Client, route llmutil.ResolvedRoute) llm.Client {
		return llminspect.WrapClient(client, llminspect.ClientOptions{
			PromptInspector:  promptInspector,
			RequestInspector: requestInspector,
			APIBase:          route.ClientConfig.Endpoint,
			Model:            strings.TrimSpace(route.ClientConfig.Model),
		})
	}
	execRuntime, err := taskruntime.Bootstrap(d, taskruntime.BootstrapOptions{
		AgentConfig:       opts.AgentConfig,
		EngineToolsConfig: opts.EngineToolsConfig,
		ClientDecorator:   decorateRuntimeClient,
	})
	if err != nil {
		cleanupInspectors()
		return ChannelRuntimeBundle{}, err
	}
	mainRoute := execRuntime.BootstrapMainRoute
	addressingRoute, err := d.ResolveLLMRoute(llmutil.RoutePurposeAddressing)
	if err != nil {
		_ = execRuntime.Close()
		cleanupInspectors()
		return ChannelRuntimeBundle{}, err
	}
	addressingClient := execRuntime.BootstrapMainClient
	addressingClientOwned := false
	if !addressingRoute.SameProfile(mainRoute) {
		addressingClient, err = d.CreateLLMClient(addressingRoute)
		addressingClientOwned = addressingClient != nil
		addressingClient = execRuntime.OwnBootstrapClient(addressingClient)
		if err != nil {
			closeAddressingClient(execRuntime, addressingClient, addressingClientOwned)
			_ = execRuntime.Close()
			cleanupInspectors()
			return ChannelRuntimeBundle{}, err
		}
		addressingClient = decorateRuntimeClient(addressingClient, addressingRoute)
	}
	memoryDeps := d
	createMemoryClient := d.CreateLLMClient
	memoryDeps.CreateLLMClient = func(route llmutil.ResolvedRoute) (llm.Client, error) {
		client, createErr := createMemoryClient(route)
		return execRuntime.OwnBootstrapClient(client), createErr
	}
	generationCtx, cancelGeneration := context.WithCancel(ctx)
	memRuntime, err := NewMemoryRuntime(memoryDeps, MemoryRuntimeOptions{
		Enabled:       opts.MemoryEnabled,
		ShortTermDays: opts.MemoryShortTermDays,
		Logger:        opts.Logger,
		Decorate:      decorateRuntimeClient,
	})
	if err != nil {
		cancelGeneration()
		closeAddressingClient(execRuntime, addressingClient, addressingClientOwned)
		_ = execRuntime.Close()
		cleanupInspectors()
		return ChannelRuntimeBundle{}, err
	}
	if memRuntime.ProjectionWorker != nil {
		memRuntime.ProjectionWorker.Start(generationCtx)
	}
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			cancelGeneration()
			memRuntime.Cleanup()
			closeAddressingClient(execRuntime, addressingClient, addressingClientOwned)
			_ = execRuntime.Close()
			cleanupInspectors()
		})
	}
	return ChannelRuntimeBundle{
		TaskRuntime:      execRuntime,
		AddressingRoute:  addressingRoute,
		AddressingClient: addressingClient,
		AddressingModel:  strings.TrimSpace(addressingRoute.ClientConfig.Model),
		Memory:           memRuntime,
		Cleanup:          cleanup,
		done:             generationCtx.Done(),
	}, nil
}

func closeAddressingClient(execRuntime *taskruntime.Runtime, client llm.Client, owned bool) {
	if !owned || client == nil {
		return
	}
	if execRuntime != nil && (sameChannelClient(client, execRuntime.BootstrapMainClient) || sameChannelClient(client, execRuntime.PlanClient)) {
		return
	}
	if closer, ok := client.(io.Closer); ok {
		_ = closer.Close()
	}
}

func sameChannelClient(a, b llm.Client) bool {
	if a == nil || b == nil {
		return false
	}
	aType := reflect.TypeOf(a)
	if aType != reflect.TypeOf(b) || !aType.Comparable() {
		return false
	}
	return a == b
}
