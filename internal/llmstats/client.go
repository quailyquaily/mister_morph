package llmstats

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/internal/topiccontext"
	"github.com/quailyquaily/mistermorph/llm"
)

type ClientOptions struct {
	Provider            string
	APIBase             string
	DefaultModel        string
	ContextWindowTokens int64
	JournalDir          string
	RotateMaxFileBytes  int64
	Logger              *slog.Logger
}

type UsageClient struct {
	Base                llm.Client
	Journal             *Journal
	Provider            string
	APIBase             string
	DefaultModel        string
	ContextWindowTokens int64
	Logger              *slog.Logger
	now                 func() time.Time
}

type ImageUsageClient struct {
	Base         llm.ImageClient
	Journal      *Journal
	Provider     string
	APIBase      string
	DefaultModel string
	Logger       *slog.Logger
	now          func() time.Time
}

func WrapClient(base llm.Client, opts ClientOptions) llm.Client {
	if base == nil {
		return nil
	}
	journalDir := strings.TrimSpace(opts.JournalDir)
	if journalDir == "" {
		return base
	}
	return &UsageClient{
		Base:                base,
		Journal:             NewJournal(journalDir, JournalOptions{MaxFileBytes: opts.RotateMaxFileBytes}),
		Provider:            normalizeProvider(opts.Provider),
		APIBase:             normalizeAPIBase(opts.APIBase),
		DefaultModel:        normalizeModel(opts.DefaultModel),
		ContextWindowTokens: opts.ContextWindowTokens,
		Logger:              opts.Logger,
		now:                 time.Now,
	}
}

func WrapImageClient(base llm.ImageClient, opts ClientOptions) llm.ImageClient {
	if base == nil {
		return nil
	}
	journalDir := strings.TrimSpace(opts.JournalDir)
	if journalDir == "" {
		return base
	}
	return &ImageUsageClient{
		Base:         base,
		Journal:      NewJournal(journalDir, JournalOptions{MaxFileBytes: opts.RotateMaxFileBytes}),
		Provider:     normalizeProvider(opts.Provider),
		APIBase:      normalizeAPIBase(opts.APIBase),
		DefaultModel: normalizeModel(opts.DefaultModel),
		Logger:       opts.Logger,
		now:          time.Now,
	}
}

func WrapRuntimeClient(base llm.Client, provider, apiBase, defaultModel string, contextWindowTokens int64, logger *slog.Logger) llm.Client {
	return WrapClient(base, ClientOptions{
		Provider:            provider,
		APIBase:             apiBase,
		DefaultModel:        defaultModel,
		ContextWindowTokens: contextWindowTokens,
		JournalDir:          statepaths.LLMUsageJournalDir(),
		Logger:              logger,
	})
}

func WrapRuntimeImageClient(base llm.ImageClient, provider, apiBase, defaultModel string, logger *slog.Logger) llm.ImageClient {
	return WrapImageClient(base, ClientOptions{
		Provider:     provider,
		APIBase:      apiBase,
		DefaultModel: defaultModel,
		JournalDir:   statepaths.LLMUsageJournalDir(),
		Logger:       logger,
	})
}

func (c *UsageClient) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	if c == nil || c.Base == nil {
		return llm.Result{}, fmt.Errorf("usage client is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	start := c.now()
	res, err := c.Base.Chat(ctx, req)
	if err != nil {
		return res, err
	}
	if c.Journal == nil {
		return res, nil
	}

	finished := c.now()
	rec := requestRecordFromUsage(ctx, requestRecordInput{
		TS:         finished,
		Provider:   c.Provider,
		APIBase:    c.APIBase,
		Model:      firstNonEmpty(strings.TrimSpace(req.Model), c.DefaultModel),
		Operation:  operationChat,
		Scene:      strings.TrimSpace(req.Scene),
		Usage:      res.Usage,
		DurationMs: durationMillis(res.Duration, finished.Sub(start)),
	})
	appendUsageRecord(c.Journal, c.Logger, rec)
	topiccontext.ObserveUsage(ctx, topiccontext.UsageSample{
		RunID:                    rec.RunID,
		OriginEventID:            rec.OriginEventID,
		Scene:                    rec.Scene,
		Provider:                 rec.Provider,
		APIBase:                  rec.APIBase,
		Model:                    rec.Model,
		ContextWindowTokens:      c.ContextWindowTokens,
		InputTokens:              rec.InputTokens,
		CachedInputTokens:        rec.CachedInputTokens,
		CacheCreationInputTokens: rec.CacheCreationInputTokens,
		UpdatedAt:                finished,
	})
	return res, nil
}

func (c *ImageUsageClient) GenerateImage(ctx context.Context, req llm.ImageRequest) (llm.ImageResult, error) {
	if c == nil || c.Base == nil {
		return llm.ImageResult{}, fmt.Errorf("image usage client is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	start := c.now()
	res, err := c.Base.GenerateImage(ctx, req)
	if err != nil {
		return res, err
	}
	finished := c.now()
	rec := requestRecordFromUsage(ctx, requestRecordInput{
		TS:         finished,
		Provider:   c.Provider,
		APIBase:    c.APIBase,
		Model:      firstNonEmpty(strings.TrimSpace(req.Model), c.DefaultModel),
		Operation:  operationImageGenerate,
		Scene:      "tool.image_generate",
		Usage:      res.Usage,
		DurationMs: durationMillis(res.Duration, finished.Sub(start)),
	})
	appendUsageRecord(c.Journal, c.Logger, rec)
	return res, nil
}

func (c *ImageUsageClient) EditImage(ctx context.Context, req llm.ImageEditRequest) (llm.ImageResult, error) {
	if c == nil || c.Base == nil {
		return llm.ImageResult{}, fmt.Errorf("image usage client is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	start := c.now()
	res, err := c.Base.EditImage(ctx, req)
	if err != nil {
		return res, err
	}
	finished := c.now()
	rec := requestRecordFromUsage(ctx, requestRecordInput{
		TS:         finished,
		Provider:   c.Provider,
		APIBase:    c.APIBase,
		Model:      firstNonEmpty(strings.TrimSpace(req.Model), c.DefaultModel),
		Operation:  operationImageEdit,
		Scene:      "tool.image_edit",
		Usage:      res.Usage,
		DurationMs: durationMillis(res.Duration, finished.Sub(start)),
	})
	appendUsageRecord(c.Journal, c.Logger, rec)
	return res, nil
}

type requestRecordInput struct {
	TS         time.Time
	Provider   string
	APIBase    string
	Model      string
	Operation  string
	Scene      string
	Usage      llm.Usage
	DurationMs int64
}

func requestRecordFromUsage(ctx context.Context, in requestRecordInput) RequestRecord {
	rec := RequestRecord{
		TS:                       in.TS.UTC().Format(time.RFC3339),
		RunID:                    RunIDFromContext(ctx),
		OriginEventID:            OriginEventIDFromContext(ctx),
		Provider:                 in.Provider,
		APIBase:                  in.APIBase,
		Model:                    in.Model,
		Operation:                in.Operation,
		Scene:                    in.Scene,
		InputTokens:              int64(in.Usage.InputTokens),
		OutputTokens:             int64(in.Usage.OutputTokens),
		TotalTokens:              int64(in.Usage.TotalTokens),
		CachedInputTokens:        int64(in.Usage.Cache.CachedInputTokens),
		CacheCreationInputTokens: int64(in.Usage.Cache.CacheCreationInputTokens),
		CacheDetails:             toInt64Map(in.Usage.Cache.Details),
		DurationMs:               in.DurationMs,
	}
	if in.Usage.Cost != nil {
		rec.CostCurrency = strings.TrimSpace(in.Usage.Cost.Currency)
		rec.CostEstimated = in.Usage.Cost.Estimated
		rec.InputCost = in.Usage.Cost.Input
		rec.CachedInputCost = in.Usage.Cost.CachedInput
		rec.CacheCreationInputCost = in.Usage.Cost.CacheCreationInput
		rec.OutputCost = in.Usage.Cost.Output
		rec.TotalCost = in.Usage.Cost.Total
	}
	return normalizeRequestRecord(rec)
}

func appendUsageRecord(journal *Journal, logger *slog.Logger, rec RequestRecord) {
	if journal == nil {
		return
	}
	if _, recErr := journal.Append(rec); recErr != nil && logger != nil {
		logger.Warn(
			"llm_usage_record_error",
			"error", recErr.Error(),
			"provider", rec.Provider,
			"api_host", rec.APIHost,
			"model", rec.Model,
		)
	}
}

func toInt64Map(in map[string]int) map[string]int64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int64, len(in))
	for key, value := range in {
		out[key] = int64(value)
	}
	return out
}

func (c *UsageClient) Close() error {
	if c == nil {
		return nil
	}
	var firstErr error
	if c.Journal != nil {
		if err := c.Journal.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if closer, ok := c.Base.(io.Closer); ok {
		if err := closer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (c *ImageUsageClient) Close() error {
	if c == nil {
		return nil
	}
	var firstErr error
	if c.Journal != nil {
		if err := c.Journal.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if closer, ok := c.Base.(io.Closer); ok {
		if err := closer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func firstNonEmpty(values ...string) string {
	for _, raw := range values {
		if s := strings.TrimSpace(raw); s != "" {
			return s
		}
	}
	return ""
}

func durationMillis(values ...time.Duration) int64 {
	for _, d := range values {
		if d > 0 {
			return d.Milliseconds()
		}
	}
	return 0
}
