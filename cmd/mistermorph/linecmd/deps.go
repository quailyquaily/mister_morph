package linecmd

import (
	heartbeatruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/heartbeat"
	lineruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/line"
)

// Dependencies defines runtime wiring hooks for line mode.
type Dependencies struct {
	heartbeatruntime.Dependencies
	HandleModelCommand lineruntime.HandleModelCommandFunc
}
