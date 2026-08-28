package mixincmd

import (
	awarenessruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/awareness"
	mixinruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/mixin"
)

type Dependencies struct {
	awarenessruntime.Dependencies
	HandleModelCommand mixinruntime.HandleModelCommandFunc
	HandleSkillCommand mixinruntime.HandleSkillCommandFunc
}
