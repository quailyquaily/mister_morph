package integration

import (
	"log/slog"

	"github.com/quailyquaily/mistermorph/guard"
)

func (rt *Runtime) buildGuard(snapshot guard.Snapshot, logger *slog.Logger) (*guard.Guard, error) {
	if rt == nil || !rt.features.Guard {
		return nil, nil
	}
	return guard.NewChecked(snapshot, logger)
}
