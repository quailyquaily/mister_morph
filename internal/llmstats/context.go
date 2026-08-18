package llmstats

import (
	"context"
	"strings"
)

type runIDContextKey struct{}
type originEventIDContextKey struct{}

func WithRunID(ctx context.Context, runID string) context.Context {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, runIDContextKey{}, runID)
}

func RunIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	runID, _ := ctx.Value(runIDContextKey{}).(string)
	return runID
}

func WithOriginEventID(ctx context.Context, eventID string) context.Context {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, originEventIDContextKey{}, eventID)
}

func OriginEventIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	eventID, _ := ctx.Value(originEventIDContextKey{}).(string)
	return eventID
}

func WithMetadata(ctx context.Context, runID string, originEventID string) context.Context {
	ctx = WithRunID(ctx, runID)
	ctx = WithOriginEventID(ctx, originEventID)
	return ctx
}
