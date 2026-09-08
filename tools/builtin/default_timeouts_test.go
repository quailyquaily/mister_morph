package builtin

import (
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/pathroots"
)

func TestToolDefaultTimeouts(t *testing.T) {
	for _, input := range []time.Duration{0, -time.Second, 7 * time.Second} {
		t.Run(input.String(), func(t *testing.T) {
			want := input
			if input <= 0 {
				want = time.Minute
			}
			for name, got := range map[string]time.Duration{
				"web_search": NewWebSearchTool(true, "", input, 0, "").Timeout,
				"bash":       NewBashTool(true, input, 0, pathroots.PathRoots{}).DefaultTimeout,
				"powershell": NewPowerShellTool(true, input, 0, pathroots.PathRoots{}).DefaultTimeout,
			} {
				if got != want {
					t.Errorf("%s timeout = %s, want %s", name, got, want)
				}
			}
		})
	}
}
