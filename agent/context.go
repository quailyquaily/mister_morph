package agent

import (
	"encoding/json"
	"time"

	"github.com/quailyquaily/mister_morph/llm"
)

type Metrics struct {
	LLMRounds    int
	TotalTokens  int
	TotalCost    float64
	StartTime    time.Time
	ElapsedMs    int64
	ToolCalls    int
	ParseRetries int
}

type Context struct {
	Task           string
	Steps          []Step
	MaxSteps       int
	Plan           *Plan
	Metrics        *Metrics
	RawFinalAnswer json.RawMessage
}

func NewContext(task string, maxSteps int) *Context {
	return &Context{
		Task:     task,
		Steps:    []Step{},
		MaxSteps: maxSteps,
		Metrics:  &Metrics{StartTime: time.Now()},
	}
}

func (c *Context) RecordStep(step Step) {
	c.Steps = append(c.Steps, step)
	c.Metrics.ToolCalls++
}

func (c *Context) AddUsage(usage llm.Usage, dur time.Duration) {
	c.Metrics.LLMRounds++
	c.Metrics.TotalTokens += usage.TotalTokens
	if c.Metrics.TotalTokens == 0 {
		c.Metrics.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	c.Metrics.TotalCost += usage.Cost
	c.Metrics.ElapsedMs = time.Since(c.Metrics.StartTime).Milliseconds()
	_ = dur
}
