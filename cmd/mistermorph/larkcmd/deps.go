package larkcmd

import (
	heartbeatruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/heartbeat"
	larkruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/lark"
)

// Dependencies defines runtime wiring hooks for lark mode.
type Dependencies struct {
	heartbeatruntime.Dependencies
	HandleModelCommand larkruntime.HandleModelCommandFunc
}
