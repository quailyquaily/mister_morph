package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverFollowsSkillRootSymlink(t *testing.T) {
	tmp := t.TempDir()
	realRoot := filepath.Join(tmp, "real-skills")
	skillDir := filepath.Join(realRoot, "writer")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\ndescription: Root symlink skill\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	linkRoot := filepath.Join(tmp, "linked-skills")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	got, err := Discover(DiscoverOptions{Roots: []string{linkRoot}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("Discover() count = %d, want 1: %#v", len(got), got)
	}
	if got[0].ID != "writer" {
		t.Fatalf("Discover()[0].ID = %q, want writer", got[0].ID)
	}
	if filepath.Clean(got[0].RootDir) != filepath.Clean(linkRoot) {
		t.Fatalf("Discover()[0].RootDir = %q, want %q", got[0].RootDir, linkRoot)
	}
	wantDir := filepath.Join(linkRoot, "writer")
	if filepath.Clean(got[0].Dir) != filepath.Clean(wantDir) {
		t.Fatalf("Discover()[0].Dir = %q, want %q", got[0].Dir, wantDir)
	}
	wantSkillMD := filepath.Join(wantDir, "SKILL.md")
	if filepath.Clean(got[0].SkillMD) != filepath.Clean(wantSkillMD) {
		t.Fatalf("Discover()[0].SkillMD = %q, want %q", got[0].SkillMD, wantSkillMD)
	}

	loaded, err := LoadFrontmatter(got[0], 0)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Description != "Root symlink skill" {
		t.Fatalf("LoadFrontmatter().Description = %q, want Root symlink skill", loaded.Description)
	}
}

func TestDiscoverFollowsDirectSkillDirSymlink(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "skills")
	realSkillDir := filepath.Join(tmp, "external", "painter")
	if err := os.MkdirAll(realSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realSkillDir, "SKILL.md"), []byte("---\ndescription: Linked skill directory\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	linkSkillDir := filepath.Join(root, "painter")
	if err := os.Symlink(realSkillDir, linkSkillDir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	got, err := Discover(DiscoverOptions{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("Discover() count = %d, want 1: %#v", len(got), got)
	}
	if got[0].ID != "painter" {
		t.Fatalf("Discover()[0].ID = %q, want painter", got[0].ID)
	}
	if filepath.Clean(got[0].Dir) != filepath.Clean(linkSkillDir) {
		t.Fatalf("Discover()[0].Dir = %q, want %q", got[0].Dir, linkSkillDir)
	}
	if filepath.Clean(got[0].SkillMD) != filepath.Clean(filepath.Join(linkSkillDir, "SKILL.md")) {
		t.Fatalf("Discover()[0].SkillMD = %q, want link path", got[0].SkillMD)
	}

	loaded, err := LoadFrontmatter(got[0], 0)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Description != "Linked skill directory" {
		t.Fatalf("LoadFrontmatter().Description = %q, want Linked skill directory", loaded.Description)
	}
}
