package consolecmd

import (
	"testing"

	heartbeatloop "github.com/quailyquaily/mistermorph/internal/channelruntime/heartbeat"
)

func TestConsoleLocalRoutesOptionsPoke(t *testing.T) {
	rt := &consoleLocalRuntime{}
	if got := rt.routesOptions("token").Poke; got != nil {
		t.Fatalf("Poke = %#v, want nil when heartbeat loop is unavailable", got)
	}

	rt.heartbeatPokeRequests = make(chan heartbeatloop.PokeRequest)
	if got := rt.routesOptions("token").Poke; got == nil {
		t.Fatal("Poke = nil, want non-nil when heartbeat loop is available")
	}
}
