package guard

import (
	"reflect"
	"testing"
)

func TestGuardWarningsAreNormalizedAndCopied(t *testing.T) {
	g := NewWithWarnings(Config{}, nil, nil, []string{" First ", "first", "", "Second"})

	got := g.Warnings()
	if want := []string{"First", "Second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Warnings() = %#v, want %#v", got, want)
	}

	got[0] = "changed"
	if want := []string{"First", "Second"}; !reflect.DeepEqual(g.Warnings(), want) {
		t.Fatalf("Warnings() returned mutable state: %#v", g.Warnings())
	}
}
