package mixincmd

import "testing"

func TestCommandDoesNotExposeUnavailableGroupTriggerOptions(t *testing.T) {
	t.Parallel()

	cmd := NewCommand(Dependencies{})
	for _, name := range []string{
		"mixin-group-trigger-mode",
		"mixin-addressing-confidence-threshold",
		"mixin-addressing-interject-threshold",
	} {
		if cmd.Flags().Lookup(name) != nil {
			t.Fatalf("unexpected flag --%s", name)
		}
	}
}
