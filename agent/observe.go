package agent

import (
	"strings"
	"time"
)

type ObserveProfile string

const (
	ObserveProfileDefault    ObserveProfile = "default"
	ObserveProfileLongShell  ObserveProfile = "long_shell"
	ObserveProfileWebExtract ObserveProfile = "web_extract"
)

type ObservePolicy struct {
	Profile         ObserveProfile
	MaxLLMChecks    int
	MinInterval     time.Duration
	MinNewBytes     int
	MinNewEvents    int
	ForceOnTerminal bool
	ForceOnFailure  bool
	ForceOnPending  bool
	StreamOutput    bool
}

func NormalizeObserveProfile(raw string) ObserveProfile {
	switch ObserveProfile(strings.ToLower(strings.TrimSpace(raw))) {
	case ObserveProfileLongShell:
		return ObserveProfileLongShell
	case ObserveProfileWebExtract:
		return ObserveProfileWebExtract
	default:
		return ObserveProfileDefault
	}
}

func ObservePolicyForProfile(profile ObserveProfile) ObservePolicy {
	switch NormalizeObserveProfile(string(profile)) {
	case ObserveProfileLongShell:
		return ObservePolicy{
			Profile:         ObserveProfileLongShell,
			MaxLLMChecks:    0,
			MinInterval:     2 * time.Second,
			MinNewBytes:     256,
			MinNewEvents:    0,
			ForceOnTerminal: true,
			ForceOnFailure:  true,
			ForceOnPending:  true,
			StreamOutput:    true,
		}
	case ObserveProfileWebExtract:
		return ObservePolicy{
			Profile:         ObserveProfileWebExtract,
			MaxLLMChecks:    1,
			MinInterval:     0,
			MinNewBytes:     0,
			MinNewEvents:    0,
			ForceOnTerminal: true,
			ForceOnFailure:  true,
			ForceOnPending:  true,
			StreamOutput:    false,
		}
	default:
		return ObservePolicy{
			Profile:         ObserveProfileDefault,
			MaxLLMChecks:    0,
			MinInterval:     0,
			MinNewBytes:     0,
			MinNewEvents:    0,
			ForceOnTerminal: true,
			ForceOnFailure:  true,
			ForceOnPending:  true,
			StreamOutput:    false,
		}
	}
}
