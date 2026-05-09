package larkcmd

import (
	awarenessruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/awareness"
	larkruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/lark"
)

// Dependencies defines runtime wiring hooks for lark mode.
type Dependencies struct {
	awarenessruntime.Dependencies
	HandleModelCommand larkruntime.HandleModelCommandFunc
	HandleSkillCommand larkruntime.HandleSkillCommandFunc
}
