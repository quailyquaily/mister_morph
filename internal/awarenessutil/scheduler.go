package awarenessutil

import "strings"

type TickOutcome int

const (
	TickEnqueued TickOutcome = iota
	TickSkipped
	TickBuildError
)

const (
	SkipReasonInvalidConfig  = "invalid_config"
	SkipReasonAlreadyRunning = "already_running"
	SkipReasonEmptyTask      = "empty_task"
)

type TaskBuilder func() (task string, checklistEmpty bool, err error)

type TaskEnqueuer func(task string, checklistEmpty bool) (skipReason string)

type TickResult struct {
	Behavior     Behavior
	Outcome      TickOutcome
	SkipReason   string
	BuildError   error
	AlertMessage string
}

func Tick(state *State, behavior Behavior, buildTask TaskBuilder, enqueueTask TaskEnqueuer) TickResult {
	behavior = NormalizeBehavior(string(behavior))
	if state == nil || buildTask == nil || enqueueTask == nil {
		return TickResult{
			Behavior:   behavior,
			Outcome:    TickSkipped,
			SkipReason: SkipReasonInvalidConfig,
		}
	}
	if !state.Start() {
		return TickResult{
			Behavior:   behavior,
			Outcome:    TickSkipped,
			SkipReason: SkipReasonAlreadyRunning,
		}
	}

	task, checklistEmpty, err := buildTask()
	if err != nil {
		alert, msg := state.EndFailure(err)
		result := TickResult{
			Behavior:   behavior,
			Outcome:    TickBuildError,
			BuildError: err,
		}
		if alert {
			result.AlertMessage = msg
		}
		return result
	}
	if strings.TrimSpace(task) == "" {
		state.EndSkipped()
		return TickResult{
			Behavior:   behavior,
			Outcome:    TickSkipped,
			SkipReason: SkipReasonEmptyTask,
		}
	}

	reason := strings.TrimSpace(enqueueTask(task, checklistEmpty))
	if reason != "" {
		state.EndSkipped()
		return TickResult{
			Behavior:   behavior,
			Outcome:    TickSkipped,
			SkipReason: reason,
		}
	}

	return TickResult{Behavior: behavior, Outcome: TickEnqueued}
}
