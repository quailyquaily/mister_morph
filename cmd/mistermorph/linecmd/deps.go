package linecmd

import (
	awarenessruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/awareness"
	lineruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/line"
)

// Dependencies defines runtime wiring hooks for line mode.
type Dependencies struct {
	awarenessruntime.Dependencies
	HandleModelCommand lineruntime.HandleModelCommandFunc
	HandleSkillCommand lineruntime.HandleSkillCommandFunc
}
