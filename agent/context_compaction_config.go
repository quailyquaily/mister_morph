package agent

import "fmt"

const (
	defaultContextCompactionTriggerRatio = 0.80
	contextCompactionTargetRatio         = 0.60
)

type ContextCompactionConfig struct {
	Enabled             *bool
	TriggerRatio        float64
	OutputReserveTokens int
}

func NewContextCompactionConfig(enabled bool, triggerRatio float64, outputReserveTokens int) ContextCompactionConfig {
	return ContextCompactionConfig{
		Enabled:             &enabled,
		TriggerRatio:        triggerRatio,
		OutputReserveTokens: outputReserveTokens,
	}
}

func (config ContextCompactionConfig) Validate() error {
	usesDefaultTriggerRatio := config.TriggerRatio == 0 && config.Enabled == nil
	if !usesDefaultTriggerRatio && (config.TriggerRatio <= 0 || config.TriggerRatio >= 1) {
		return fmt.Errorf("context compaction trigger ratio must be greater than 0 and less than 1")
	}
	if config.OutputReserveTokens < 0 {
		return fmt.Errorf("context compaction output reserve tokens cannot be negative")
	}
	return nil
}

type resolvedContextCompactionConfig struct {
	Enabled             bool
	TriggerRatio        float64
	OutputReserveTokens int
}

func resolveContextCompactionConfig(config ContextCompactionConfig, disabledForRun bool) resolvedContextCompactionConfig {
	enabled := true
	if config.Enabled != nil {
		enabled = *config.Enabled
	}
	ratio := config.TriggerRatio
	if ratio <= 0 || ratio >= 1 {
		ratio = defaultContextCompactionTriggerRatio
	}
	reserve := config.OutputReserveTokens
	if reserve < 0 {
		reserve = 0
	}
	return resolvedContextCompactionConfig{
		Enabled:             enabled && !disabledForRun,
		TriggerRatio:        ratio,
		OutputReserveTokens: reserve,
	}
}

func contextInputLimits(contextWindowTokens int64, config resolvedContextCompactionConfig, requestMaxTokens int) (inputLimit int, trigger int, outputReserve int) {
	if contextWindowTokens <= 0 || contextWindowTokens > int64(^uint(0)>>1) {
		return 0, 0, 0
	}
	window := int(contextWindowTokens)
	outputReserve = requestMaxTokens
	if outputReserve <= 0 {
		outputReserve = config.OutputReserveTokens
	}
	if outputReserve <= 0 {
		outputReserve = defaultContextOutputReserve(window)
	}
	if outputReserve >= window {
		return 0, 0, outputReserve
	}
	inputLimit = window - outputReserve
	trigger = int(float64(inputLimit) * config.TriggerRatio)
	if trigger < 1 {
		trigger = 1
	}
	return inputLimit, trigger, outputReserve
}

func defaultContextOutputReserve(contextWindowTokens int) int {
	if contextWindowTokens <= 0 {
		return 0
	}
	reserve := contextWindowTokens / 10
	if reserve < 4096 {
		reserve = 4096
	}
	if reserve > 32768 {
		reserve = 32768
	}
	half := contextWindowTokens / 2
	if reserve > half {
		reserve = half
	}
	return reserve
}

func checkpointMaxOutputTokens(outputReserve int) int {
	if outputReserve <= 0 {
		return 0
	}
	if outputReserve > 4096 {
		return 4096
	}
	return outputReserve
}
