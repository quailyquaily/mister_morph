package builtin

import (
	"github.com/quailyquaily/mister_morph/internal/pathutil"
)

func expandHomePath(p string) string {
	return pathutil.ExpandHomePath(p)
}
