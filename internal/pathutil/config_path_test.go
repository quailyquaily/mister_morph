package pathutil

import (
	"path/filepath"
	"testing"
)

func TestResolveConfigRelativePath(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	want := filepath.Join(dir, "credentials", "mixin.json")
	if got := ResolveConfigRelativePath("credentials/mixin.json", configPath); got != want {
		t.Fatalf("ResolveConfigRelativePath() = %q, want %q", got, want)
	}
	if got := ResolveConfigRelativePath(want, configPath); got != want {
		t.Fatalf("absolute ResolveConfigRelativePath() = %q, want %q", got, want)
	}
}
