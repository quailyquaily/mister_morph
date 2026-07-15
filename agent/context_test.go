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

	usage1 := llm.Usage{
		InputTokens:  100,
		OutputTokens: 50,
		TotalTokens:  150,
		Cost:         &llm.UsageCost{Currency: "USD", Total: 0.05},
	}
	ctx.AddUsage(usage1, time.Second)
	if !almostEqual(ctx.Metrics.TotalCost, 0.05) {
		t.Errorf("expected TotalCost≈0.05, got %f", ctx.Metrics.TotalCost)
	}

	usage2 := llm.Usage{
		InputTokens:  200,
		OutputTokens: 100,
		TotalTokens:  300,
		Cost:         &llm.UsageCost{Currency: "USD", Total: 0.10},
	}
	ctx.AddUsage(usage2, time.Second)
	if !almostEqual(ctx.Metrics.TotalCost, 0.15) {
		t.Errorf("expected TotalCost≈0.15, got %f", ctx.Metrics.TotalCost)
	}
}

func TestAddUsageNilCostNoChange(t *testing.T) {
	ctx := NewContext("test", 5)

	usage := llm.Usage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150}
	ctx.AddUsage(usage, time.Second)
	if ctx.Metrics.TotalCost != 0 {
		t.Errorf("expected TotalCost=0, got %f", ctx.Metrics.TotalCost)
	}
}

func TestAddUsageFallsBackPerRoundWhenTotalTokensMissing(t *testing.T) {
	ctx := NewContext("test", 5)
	ctx.AddUsage(llm.Usage{InputTokens: 70, OutputTokens: 30, TotalTokens: 100}, time.Second)
	ctx.AddUsage(llm.Usage{InputTokens: 10, OutputTokens: 5}, time.Second)
	if ctx.Metrics.TotalTokens != 115 {
		t.Fatalf("TotalTokens = %d, want 115", ctx.Metrics.TotalTokens)
	}
}
