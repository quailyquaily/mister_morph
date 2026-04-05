package agent

const (
	DefaultMaxSteps        = 15
	DefaultParseRetries    = 2
	DefaultToolRepeatLimit = 3
)

// Limits groups loop-control knobs so upper layers can pass agent limits
// as a single value instead of repeating individual fields.
type Limits struct {
	MaxSteps        int
	ParseRetries    int
	MaxTokenBudget  int
	ToolRepeatLimit int
}

func (l Limits) ToConfig() Config {
	return Config{
		MaxSteps:        l.MaxSteps,
		ParseRetries:    l.ParseRetries,
		MaxTokenBudget:  l.MaxTokenBudget,
		ToolRepeatLimit: l.ToolRepeatLimit,
	}
}

// NormalizeForRuntime applies channel/runtime defaults that historically
// treated <=0 values as unset for retries/repeat limits.
func (l Limits) NormalizeForRuntime() Limits {
	if l.MaxSteps <= 0 {
		l.MaxSteps = DefaultMaxSteps
	}
	if l.ParseRetries <= 0 {
		l.ParseRetries = DefaultParseRetries
	}
	if l.ToolRepeatLimit <= 0 {
		l.ToolRepeatLimit = DefaultToolRepeatLimit
	}
	if l.MaxTokenBudget < 0 {
		l.MaxTokenBudget = 0
	}
	return l
}
