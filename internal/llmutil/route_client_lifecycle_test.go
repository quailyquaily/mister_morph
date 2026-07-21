package llmutil

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/llm"
)

type lifecycleRouteClient struct {
	closeCalls int
}

func (*lifecycleRouteClient) Chat(context.Context, llm.Request) (llm.Result, error) {
	return llm.Result{}, nil
}

func (c *lifecycleRouteClient) Close() error {
	c.closeCalls++
	return nil
}

func TestBuildRouteClientClosesClientReturnedWithBuildError(t *testing.T) {
	buildErr := errors.New("build primary")
	client := &lifecycleRouteClient{}

	got, err := BuildRouteClient(ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{Model: "main"},
	}, nil, func(llmconfig.ClientConfig, RuntimeValues) (llm.Client, error) {
		return client, buildErr
	}, nil, nil)
	if got != nil {
		t.Fatalf("BuildRouteClient() client = %T, want nil", got)
	}
	if !errors.Is(err, buildErr) {
		t.Fatalf("BuildRouteClient() error = %v, want %v", err, buildErr)
	}
	if client.closeCalls != 1 {
		t.Fatalf("returned client close calls = %d, want 1", client.closeCalls)
	}
}

func TestBuildRouteClientClosesFallbackReturnedWithBuildError(t *testing.T) {
	buildErr := errors.New("build fallback")
	primary := &lifecycleRouteClient{}
	failed := &lifecycleRouteClient{}

	_, err := BuildRouteClient(ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{Model: "main"},
		Fallbacks: []ResolvedFallback{
			{ClientConfig: llmconfig.ClientConfig{Model: "fallback"}},
		},
	}, nil, func(cfg llmconfig.ClientConfig, _ RuntimeValues) (llm.Client, error) {
		if cfg.Model == "fallback" {
			return failed, buildErr
		}
		return primary, nil
	}, nil, nil)
	if !errors.Is(err, buildErr) {
		t.Fatalf("BuildRouteClient() error = %v, want %v", err, buildErr)
	}
	if primary.closeCalls != 1 || failed.closeCalls != 1 {
		t.Fatalf("close calls = primary:%d failed:%d, want 1 each", primary.closeCalls, failed.closeCalls)
	}
}

func TestBuildRouteClientClosesWeightedCandidateReturnedWithBuildError(t *testing.T) {
	buildErr := errors.New("build candidate")
	first := &lifecycleRouteClient{}
	failed := &lifecycleRouteClient{}

	_, err := BuildRouteClient(ResolvedRoute{
		Candidates: []ResolvedCandidate{
			{ClientConfig: llmconfig.ClientConfig{Model: "candidate-1"}, Weight: 1},
			{ClientConfig: llmconfig.ClientConfig{Model: "candidate-2"}, Weight: 1},
		},
	}, nil, func(cfg llmconfig.ClientConfig, _ RuntimeValues) (llm.Client, error) {
		if cfg.Model == "candidate-2" {
			return failed, buildErr
		}
		return first, nil
	}, nil, nil)
	if !errors.Is(err, buildErr) {
		t.Fatalf("BuildRouteClient() error = %v, want %v", err, buildErr)
	}
	if first.closeCalls != 1 || failed.closeCalls != 1 {
		t.Fatalf("close calls = first:%d failed:%d, want 1 each", first.closeCalls, failed.closeCalls)
	}
}

func TestBuildRouteClientRollsBackFallbackClientsOnBuildFailure(t *testing.T) {
	buildErr := errors.New("build fallback")
	primary := &lifecycleRouteClient{}
	fallback := &lifecycleRouteClient{}

	_, err := BuildRouteClient(ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{Model: "main"},
		Fallbacks: []ResolvedFallback{
			{ClientConfig: llmconfig.ClientConfig{Model: "fallback-1"}},
			{ClientConfig: llmconfig.ClientConfig{Model: "fallback-2"}},
		},
	}, nil, func(cfg llmconfig.ClientConfig, _ RuntimeValues) (llm.Client, error) {
		switch cfg.Model {
		case "main":
			return primary, nil
		case "fallback-1":
			return fallback, nil
		default:
			return nil, buildErr
		}
	}, nil, nil)
	if !errors.Is(err, buildErr) {
		t.Fatalf("BuildRouteClient() error = %v, want %v", err, buildErr)
	}
	if primary.closeCalls != 1 || fallback.closeCalls != 1 {
		t.Fatalf("close calls = main:%d fallback:%d, want 1 each", primary.closeCalls, fallback.closeCalls)
	}
}

func TestBuildRouteClientRollsBackWeightedCandidatesOnBuildFailure(t *testing.T) {
	buildErr := errors.New("build candidate")
	first := &lifecycleRouteClient{}
	second := &lifecycleRouteClient{}

	_, err := BuildRouteClient(ResolvedRoute{
		Candidates: []ResolvedCandidate{
			{ClientConfig: llmconfig.ClientConfig{Model: "candidate-1"}, Weight: 1},
			{ClientConfig: llmconfig.ClientConfig{Model: "candidate-2"}, Weight: 1},
			{ClientConfig: llmconfig.ClientConfig{Model: "candidate-3"}, Weight: 1},
		},
	}, nil, func(cfg llmconfig.ClientConfig, _ RuntimeValues) (llm.Client, error) {
		switch cfg.Model {
		case "candidate-1":
			return first, nil
		case "candidate-2":
			return second, nil
		default:
			return nil, buildErr
		}
	}, nil, nil)
	if !errors.Is(err, buildErr) {
		t.Fatalf("BuildRouteClient() error = %v, want %v", err, buildErr)
	}
	if first.closeCalls != 1 || second.closeCalls != 1 {
		t.Fatalf("close calls = first:%d second:%d, want 1 each", first.closeCalls, second.closeCalls)
	}
}

func TestRouteClientCloseDoesNotCloseSameClientTwice(t *testing.T) {
	shared := &lifecycleRouteClient{}
	client, err := BuildRouteClient(ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{Model: "main"},
		Fallbacks: []ResolvedFallback{
			{ClientConfig: llmconfig.ClientConfig{Model: "fallback"}},
		},
	}, nil, func(llmconfig.ClientConfig, RuntimeValues) (llm.Client, error) {
		return shared, nil
	}, nil, nil)
	if err != nil {
		t.Fatalf("BuildRouteClient() error = %v", err)
	}
	closer, ok := client.(io.Closer)
	if !ok {
		t.Fatalf("route client type %T does not implement io.Closer", client)
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if shared.closeCalls != 1 {
		t.Fatalf("shared client close calls = %d, want 1", shared.closeCalls)
	}
}
