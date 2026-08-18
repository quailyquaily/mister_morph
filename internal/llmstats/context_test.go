package llmstats

import (
	"context"
	"testing"
)

func TestMetadataContextStoresNormalizedValues(t *testing.T) {
	ctx := WithMetadata(nil, "  run-1  ", "  event-1  ")
	if got := RunIDFromContext(ctx); got != "run-1" {
		t.Fatalf("RunIDFromContext() = %q, want run-1", got)
	}
	if got := OriginEventIDFromContext(ctx); got != "event-1" {
		t.Fatalf("OriginEventIDFromContext() = %q, want event-1", got)
	}

	base := context.Background()
	if got := WithRunID(base, "  "); got != base {
		t.Fatal("WithRunID() should retain the original context for an empty id")
	}
	if got := WithOriginEventID(base, ""); got != base {
		t.Fatal("WithOriginEventID() should retain the original context for an empty id")
	}
}
