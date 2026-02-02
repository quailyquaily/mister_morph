package agent

import "github.com/quailyquaily/mister_morph/guard"

func WithGuard(g *guard.Guard) Option {
	return func(e *Engine) {
		e.guard = g
	}
}

