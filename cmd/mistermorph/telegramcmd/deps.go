package telegramcmd

import (
	awarenessruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/awareness"
	telegramruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/telegram"
)

// Dependencies defines runtime wiring hooks for telegram + awareness mode.
type Dependencies struct {
	awarenessruntime.Dependencies
	HandleModelCommand telegramruntime.HandleModelCommandFunc
	HandleSkillCommand telegramruntime.HandleSkillCommandFunc
}
