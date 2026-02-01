package skills

import "testing"

func TestParseFrontmatter_AuthProfiles(t *testing.T) {
	in := `---
name: jsonbill
description: Call JSONBill API safely.
auth_profiles: ["jsonbill", "other", "jsonbill"]
---

# rest
`
	fm, ok := ParseFrontmatter(in)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(fm.AuthProfiles) != 2 || fm.AuthProfiles[0] != "jsonbill" || fm.AuthProfiles[1] != "other" {
		t.Fatalf("unexpected auth_profiles: %#v", fm.AuthProfiles)
	}
}

func TestParseFrontmatter_NoFrontmatter(t *testing.T) {
	_, ok := ParseFrontmatter("# hi\n")
	if ok {
		t.Fatal("expected ok=false")
	}
}
