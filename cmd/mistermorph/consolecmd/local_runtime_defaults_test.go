package consolecmd

import (
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestConsoleDefaultTimeoutFromReader(t *testing.T) {
	if got := consoleDefaultTimeoutFromReader(nil); got != time.Hour {
		t.Errorf("nil reader timeout = %s, want 1h", got)
	}
	for _, value := range []string{"", "0s", "-1s", "7s"} {
		t.Run(value, func(t *testing.T) {
			v := viper.New()
			if value != "" {
				v.Set("timeout", value)
			}
			want := time.Hour
			if value == "7s" {
				want = 7 * time.Second
			}
			if got := consoleDefaultTimeoutFromReader(v); got != want {
				t.Errorf("timeout = %s, want %s", got, want)
			}
		})
	}
}
