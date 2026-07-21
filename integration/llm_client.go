package integration

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"

	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llminspect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/mcphost"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/quailyquaily/mistermorph/internal/topiccontext"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type mcpRegistration struct {
	tools []tools.Tool
	close func() error
}

type runtimeBuildDependencies struct {
	buildClient      llmutil.BaseClientBuilder
	buildImageClient func(llmutil.RuntimeValues, *slog.Logger) (llm.ImageClient, error)
	connectMCP       func(context.Context, []mcphost.ServerConfig, *slog.Logger) (mcpRegistration, error)
}

func normalizeRuntimeBuildDependencies(in runtimeBuildDependencies) runtimeBuildDependencies {
	if in.buildClient == nil {
		in.buildClient = llmutil.ClientFromConfigWithValues
	}
	if in.buildImageClient == nil {
		in.buildImageClient = func(values llmutil.RuntimeValues, _ *slog.Logger) (llm.ImageClient, error) {
			return llmutil.ImageClientFromValues(values)
		}
	}
	if in.connectMCP == nil {
		in.connectMCP = connectIntegrationMCP
	}
	return in
}

func connectIntegrationMCP(ctx context.Context, configs []mcphost.ServerConfig, logger *slog.Logger) (mcpRegistration, error) {
	host, err := mcphost.Connect(ctx, configs, logger)
	if err != nil {
		return mcpRegistration{}, err
	}
	if host == nil {
		return mcpRegistration{}, nil
	}
	return mcpRegistration{
		tools: append([]tools.Tool(nil), host.Tools()...),
		close: host.Close,
	}, nil
}

func registerIntegrationMCPTools(reg *tools.Registry, mcpTools []tools.Tool) error {
	registered := make([]string, 0, len(mcpTools))
	for _, tool := range mcpTools {
		if err := reg.Register(tool); err != nil {
			for _, name := range registered {
				reg.Remove(name)
			}
			return err
		}
		registered = append(registered, tool.Name())
	}
	return nil
}

func closeDistinctResources(resources ...any) error {
	seen := make(map[io.Closer]struct{}, len(resources))
	var errs []error
	for _, resource := range resources {
		closer, ok := resource.(io.Closer)
		if !ok || closer == nil {
			continue
		}
		closerType := reflect.TypeOf(closer)
		if closerType != nil && closerType.Comparable() {
			if _, exists := seen[closer]; exists {
				continue
			}
			seen[closer] = struct{}{}
		}
		if err := closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (rt *Runtime) buildLLMClient(route llmutil.ResolvedRoute, logger *slog.Logger, wrap llmutil.ClientWrapFunc) (llm.Client, error) {
	return llmutil.BuildRouteClient(route, nil, rt.buildDeps.buildClient, wrap, logger)
}

func integrationUsageClientWrap(paths runtimepaths.Paths, logger *slog.Logger) llmutil.ClientWrapFunc {
	var mu sync.Mutex
	sharedClosers := map[io.Closer]*onceCloseClient{}
	topicStore := topiccontext.NewStore(paths.TopicContextPath)
	return func(client llm.Client, cfg llmconfig.ClientConfig, _ string) llm.Client {
		base := client
		if closer, ok := client.(io.Closer); ok && closer != nil {
			closerType := reflect.TypeOf(closer)
			if closerType != nil && closerType.Comparable() {
				mu.Lock()
				shared := sharedClosers[closer]
				if shared == nil {
					shared = &onceCloseClient{Client: client, closer: closer}
					shared.onClose = func() {
						mu.Lock()
						if sharedClosers[closer] == shared {
							delete(sharedClosers, closer)
						}
						mu.Unlock()
					}
					sharedClosers[closer] = shared
				}
				mu.Unlock()
				base = shared
			} else {
				base = &onceCloseClient{Client: client, closer: closer}
			}
		}
		return llmstats.WrapClient(base, llmstats.ClientOptions{
			Provider:            cfg.Provider,
			APIBase:             cfg.Endpoint,
			DefaultModel:        cfg.Model,
			ContextWindowTokens: cfg.ContextWindowTokens,
			JournalDir:          paths.LLMUsageJournalDir,
			TopicContextStore:   topicStore,
			Logger:              logger,
		})
	}
}

type onceCloseClient struct {
	llm.Client
	closer  io.Closer
	once    sync.Once
	onClose func()
	err     error
}

func (c *onceCloseClient) Close() error {
	if c == nil || c.closer == nil {
		return nil
	}
	c.once.Do(func() {
		c.err = c.closer.Close()
		if c.onClose != nil {
			c.onClose()
		}
	})
	return c.err
}

func inspectClientWrap(promptInspector *llminspect.PromptInspector, requestInspector *llminspect.RequestInspector) llmutil.ClientWrapFunc {
	if promptInspector == nil && requestInspector == nil {
		return nil
	}
	var mu sync.Mutex
	sharedClosers := map[io.Closer]*onceCloseClient{}
	return func(client llm.Client, cfg llmconfig.ClientConfig, _ string) llm.Client {
		base := client
		if closer, ok := client.(io.Closer); ok && closer != nil {
			closerType := reflect.TypeOf(closer)
			if closerType != nil && closerType.Comparable() {
				mu.Lock()
				shared := sharedClosers[closer]
				if shared == nil {
					shared = &onceCloseClient{Client: client, closer: closer}
					shared.onClose = func() {
						mu.Lock()
						if sharedClosers[closer] == shared {
							delete(sharedClosers, closer)
						}
						mu.Unlock()
					}
					sharedClosers[closer] = shared
				}
				mu.Unlock()
				base = shared
			} else {
				base = &onceCloseClient{Client: client, closer: closer}
			}
		}
		return llminspect.WrapClient(base, llminspect.ClientOptions{
			PromptInspector:  promptInspector,
			RequestInspector: requestInspector,
			APIBase:          cfg.Endpoint,
			Model:            strings.TrimSpace(cfg.Model),
		})
	}
}
