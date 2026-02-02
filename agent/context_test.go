package agent

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/llm"
)

const costEpsilon = 1e-9

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < costEpsilon
}

func TestContextHasRawFinalAnswerField(t *testing.T) {
	ctx := NewContext("test", 5)
	if ctx.RawFinalAnswer != nil {
		t.Error("expected RawFinalAnswer to default to nil")
	}
}

func TestContextRawFinalAnswerAssignment(t *testing.T) {
	ctx := NewContext("test", 5)
	raw := json.RawMessage(`{"output":"hello","sources":["a"]}`)
	ctx.RawFinalAnswer = raw

	var m map[string]any
	if err := json.Unmarshal(ctx.RawFinalAnswer, &m); err != nil {
		t.Fatalf("RawFinalAnswer is not valid JSON: %v", err)
	}
	if m["output"] != "hello" {
		t.Errorf("expected output='hello', got %v", m["output"])
	}
}

func TestAddUsageAccumulatesCost(t *testing.T) {
	ctx := NewContext("test", 5)

	usage1 := llm.Usage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150, Cost: 0.05}
	ctx.AddUsage(usage1, time.Second)
	if !almostEqual(ctx.Metrics.TotalCost, 0.05) {
		t.Errorf("expected TotalCost≈0.05, got %f", ctx.Metrics.TotalCost)
	}

	usage2 := llm.Usage{InputTokens: 200, OutputTokens: 100, TotalTokens: 300, Cost: 0.10}
	ctx.AddUsage(usage2, time.Second)
	if !almostEqual(ctx.Metrics.TotalCost, 0.15) {
		t.Errorf("expected TotalCost≈0.15, got %f", ctx.Metrics.TotalCost)
	}
}

func TestAddUsageFallbackMultiRound(t *testing.T) {
	ctx := NewContext("test", 5)

	// Round 1: TotalTokens=0 → fallback should use Input+Output = 150.
	usage1 := llm.Usage{InputTokens: 100, OutputTokens: 50, TotalTokens: 0, Cost: 0.05}
	ctx.AddUsage(usage1, time.Second)
	if ctx.Metrics.TotalTokens != 150 {
		t.Errorf("round 1: expected TotalTokens=150, got %d", ctx.Metrics.TotalTokens)
	}

	// Round 2: TotalTokens=0 again → fallback MUST still apply, giving 150+300=450.
	usage2 := llm.Usage{InputTokens: 200, OutputTokens: 100, TotalTokens: 0, Cost: 0.10}
	ctx.AddUsage(usage2, time.Second)
	if ctx.Metrics.TotalTokens != 450 {
		t.Errorf("round 2: expected TotalTokens=450, got %d", ctx.Metrics.TotalTokens)
	}

	// Round 3: TotalTokens=0 again → 450+75=525.
	usage3 := llm.Usage{InputTokens: 50, OutputTokens: 25, TotalTokens: 0, Cost: 0.01}
	ctx.AddUsage(usage3, time.Second)
	if ctx.Metrics.TotalTokens != 525 {
		t.Errorf("round 3: expected TotalTokens=525, got %d", ctx.Metrics.TotalTokens)
	}
}

func TestAddUsageZeroCostNoChange(t *testing.T) {
	ctx := NewContext("test", 5)

	usage := llm.Usage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150, Cost: 0}
	ctx.AddUsage(usage, time.Second)
	if ctx.Metrics.TotalCost != 0 {
		t.Errorf("expected TotalCost=0, got %f", ctx.Metrics.TotalCost)
	}
}
