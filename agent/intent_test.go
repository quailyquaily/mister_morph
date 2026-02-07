package agent

import (
	"strings"
	"testing"
)

func TestNormalizeIntentStripsMetaConstraints(t *testing.T) {
	in := Intent{
		Goal:        "Greet the user",
		Deliverable: "A brief greeting",
		Constraints: []string{
			"Return a compact, structured intent summary.",
			"Use the same language as the user for values.",
			"Reply in Chinese.",
		},
	}
	out := normalizeIntent(in)
	if len(out.Constraints) != 1 {
		t.Fatalf("expected 1 constraint after sanitize, got %d: %#v", len(out.Constraints), out.Constraints)
	}
	if out.Constraints[0] != "Reply in Chinese." {
		t.Fatalf("unexpected remaining constraint: %q", out.Constraints[0])
	}
}

func TestIntentBlockContainsInternalOnlyNotice(t *testing.T) {
	blk := IntentBlock(Intent{Goal: "x"})
	if blk.Title != "Intent (inferred)" {
		t.Fatalf("unexpected block title: %q", blk.Title)
	}
	if !strings.Contains(blk.Content, "Internal planning context only") {
		t.Fatalf("missing internal-only notice in content: %q", blk.Content)
	}
}
