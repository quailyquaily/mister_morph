package memoryruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/memory"
)

const defaultFallbackSummaryMaxRunes = 1024

type draftResolver struct {
	client    llm.Client
	model     string
	closeOnce sync.Once
	closeErr  error
}

type DraftResolverFactoryOptions struct {
	ResolveLLMRoute func(purpose string) (llmutil.ResolvedRoute, error)
	CreateLLMClient func(route llmutil.ResolvedRoute) (llm.Client, error)
	DecorateClient  func(client llm.Client, route llmutil.ResolvedRoute) llm.Client
}

func NewConfiguredDraftResolver(opts DraftResolverFactoryOptions) (memory.DraftResolver, error) {
	if opts.ResolveLLMRoute == nil {
		return nil, fmt.Errorf("resolve LLM route dependency is missing")
	}
	if opts.CreateLLMClient == nil {
		return nil, fmt.Errorf("create LLM client dependency is missing")
	}
	route, err := opts.ResolveLLMRoute(llmutil.RoutePurposeMemoryDraft)
	if err != nil {
		return nil, err
	}
	client, err := opts.CreateLLMClient(route)
	if err != nil {
		var closeErr error
		if closer, ok := client.(io.Closer); ok {
			closeErr = closer.Close()
		}
		return nil, errors.Join(err, closeErr)
	}
	if opts.DecorateClient != nil {
		client = opts.DecorateClient(client, route)
	}
	return NewDraftResolver(client, strings.TrimSpace(route.ClientConfig.Model)), nil
}

func NewDraftResolver(client llm.Client, model string) memory.DraftResolver {
	return &draftResolver{
		client: client,
		model:  strings.TrimSpace(model),
	}
}

func (r *draftResolver) ResolveDraft(ctx context.Context, event memory.MemoryEvent, existing memory.ShortTermContent) (memory.SessionDraft, error) {
	if r.client != nil && len(event.SourceHistory) > 0 {
		draft, err := BuildLLMDraft(ctx, DraftRequest{
			Client:         r.client,
			Model:          r.model,
			History:        event.SourceHistory,
			Task:           event.TaskText,
			Output:         event.FinalOutput,
			Existing:       existing,
			SessionContext: event.SessionContext,
		})
		if err != nil {
			return memory.SessionDraft{}, err
		}
		draft.Promote = EnforceLongTermPromotionRules(draft.Promote, nil, event.TaskText)
		return draft, nil
	}
	return buildFallbackDraft(event.FinalOutput), nil
}

func (r *draftResolver) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		if closer, ok := r.client.(io.Closer); ok {
			r.closeErr = closer.Close()
		}
	})
	return r.closeErr
}

func buildFallbackDraft(finalOutput string) memory.SessionDraft {
	item := strings.TrimSpace(finalOutput)
	if item == "" {
		return memory.SessionDraft{}
	}
	item = strings.Join(strings.Fields(item), " ")
	if item == "" {
		return memory.SessionDraft{}
	}
	runes := []rune(item)
	if len(runes) > defaultFallbackSummaryMaxRunes {
		item = strings.TrimSpace(string(runes[:defaultFallbackSummaryMaxRunes]))
	}
	if item == "" {
		return memory.SessionDraft{}
	}
	return memory.SessionDraft{SummaryItems: []string{item}}
}
