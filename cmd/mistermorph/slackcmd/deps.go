package slackcmd

import (
	awarenessruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/awareness"
	slackruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/slack"
)

// Dependencies defines runtime wiring hooks for slack + awareness mode.
type Dependencies struct {
	awarenessruntime.Dependencies
	HandleModelCommand slackruntime.HandleModelCommandFunc
	HandleSkillCommand slackruntime.HandleSkillCommandFunc
}
