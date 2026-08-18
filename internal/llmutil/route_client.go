package llmutil

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/llm"
)

type weightedRouteCandidate struct {
	Profile string
	Model   string
	Weight  int
	Client  llm.Client
}

type weightedRouteClient struct {
	candidates []weightedRouteCandidate
	fallbacks  []FallbackCandidate
	logger     *slog.Logger
}

func buildWeightedRouteClient(route ResolvedRoute, primaryOverride *llmconfig.ClientConfig, build BaseClientBuilder, wrap ClientWrapFunc, logger *slog.Logger) (llm.Client, error) {
	candidates := make([]weightedRouteCandidate, 0, len(route.Candidates))
	builtClients := make([]llm.Client, 0, len(route.Candidates)+len(route.Fallbacks))
	for _, candidate := range route.Candidates {
		cfg := candidate.ClientConfig
		if primaryOverride != nil {
			cfg = mergeClientConfig(cfg, *primaryOverride)
		}
		client, err := build(cfg, candidate.Values)
		if err != nil {
			return nil, errors.Join(err, closeDistinctClients(append(builtClients, client)...))
		}
		if wrap != nil {
			client = wrap(client, cfg, candidate.Profile)
		}
		builtClients = append(builtClients, client)
		candidates = append(candidates, weightedRouteCandidate{
			Profile: candidate.Profile,
			Model:   strings.TrimSpace(cfg.Model),
			Weight:  candidate.Weight,
			Client:  client,
		})
	}

	fallbacks := make([]FallbackCandidate, 0, len(route.Fallbacks))
	for _, fallback := range route.Fallbacks {
		client, err := build(fallback.ClientConfig, fallback.Values)
		if err != nil {
			return nil, errors.Join(err, closeDistinctClients(append(builtClients, client)...))
		}
		if wrap != nil {
			client = wrap(client, fallback.ClientConfig, fallback.Profile)
		}
		builtClients = append(builtClients, client)
		fallbacks = append(fallbacks, FallbackCandidate{
			Profile: fallback.Profile,
			Model:   strings.TrimSpace(fallback.ClientConfig.Model),
			Client:  client,
		})
	}

	return &weightedRouteClient{
		candidates: candidates,
		fallbacks:  fallbacks,
		logger:     logger,
	}, nil
}

func mergeClientConfig(base llmconfig.ClientConfig, override llmconfig.ClientConfig) llmconfig.ClientConfig {
	if provider := strings.TrimSpace(override.Provider); provider != "" {
		base.Provider = provider
	}
	if endpoint := strings.TrimSpace(override.Endpoint); endpoint != "" {
		base.Endpoint = endpoint
	}
	if apiKey := strings.TrimSpace(override.APIKey); apiKey != "" {
		base.APIKey = apiKey
	}
	if model := strings.TrimSpace(override.Model); model != "" {
		base.Model = model
	}
	base.Headers = mergeStringMaps(base.Headers, override.Headers)
	if override.RequestTimeout > 0 {
		base.RequestTimeout = override.RequestTimeout
	}
	return base
}

func (c *weightedRouteClient) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	if c == nil || len(c.candidates) == 0 {
		return llm.Result{}, io.ErrClosedPipe
	}
	primaryIdx := c.pickPrimaryIndex(ctx, req)
	primary := c.candidates[primaryIdx]
	fallbacks := make([]FallbackCandidate, 0, len(c.candidates)-1+len(c.fallbacks))
	for idx, candidate := range c.candidates {
		if idx == primaryIdx {
			continue
		}
		fallbacks = append(fallbacks, FallbackCandidate{
			Profile: candidate.Profile,
			Model:   candidate.Model,
			Client:  candidate.Client,
		})
	}
	fallbacks = append(fallbacks, c.fallbacks...)
	client := NewFallbackClient(FallbackClientOptions{
		Primary:        primary.Client,
		PrimaryProfile: primary.Profile,
		PrimaryModel:   primary.Model,
		Fallbacks:      fallbacks,
		Logger:         c.logger,
	})
	fallbackReq := req
	fallbackReq.Model = primary.Model
	return client.Chat(ctx, fallbackReq)
}

func (c *weightedRouteClient) Close() error {
	if c == nil {
		return nil
	}
	clients := make([]llm.Client, 0, len(c.candidates)+len(c.fallbacks))
	for _, candidate := range c.candidates {
		clients = append(clients, candidate.Client)
	}
	for _, fallback := range c.fallbacks {
		clients = append(clients, fallback.Client)
	}
	return closeDistinctClients(clients...)
}

func (c *weightedRouteClient) pickPrimaryIndex(ctx context.Context, req llm.Request) int {
	key := selectionKey(ctx, req)
	weights := make([]int, len(c.candidates))
	for i, candidate := range c.candidates {
		weights[i] = candidate.Weight
	}
	return weightedIndex(key, weights)
}

func selectionKey(ctx context.Context, req llm.Request) string {
	if runID := llmstats.RunIDFromContext(ctx); runID != "" {
		return runID
	}
	if originEventID := llmstats.OriginEventIDFromContext(ctx); originEventID != "" {
		return originEventID
	}
	if scene := strings.TrimSpace(req.Scene); scene != "" {
		return scene
	}
	return ""
}
