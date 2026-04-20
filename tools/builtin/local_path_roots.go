package builtin

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

func resolveLocalPathRoots(ctx context.Context, fallback pathroots.PathRoots) pathroots.PathRoots {
	return pathroots.Resolve(ctx, fallback)
}

func detectPathAlias(userPath string) (string, string) {
	trimmed := strings.TrimLeft(userPath, "/\\")
	lower := strings.ToLower(trimmed)
	prefixes := []struct {
		Alias  string
		Prefix string
	}{
		{Alias: "workspace_dir", Prefix: "workspace_dir/"},
		{Alias: "workspace_dir", Prefix: "workspace_dir\\"},
		{Alias: "file_cache_dir", Prefix: "file_cache_dir/"},
		{Alias: "file_cache_dir", Prefix: "file_cache_dir\\"},
		{Alias: "file_state_dir", Prefix: "file_state_dir/"},
		{Alias: "file_state_dir", Prefix: "file_state_dir\\"},
	}
	switch lower {
	case "workspace_dir":
		return "workspace_dir", ""
	case "file_cache_dir":
		return "file_cache_dir", ""
	case "file_state_dir":
		return "file_state_dir", ""
	}
	for _, item := range prefixes {
		if !strings.HasPrefix(lower, item.Prefix) {
			continue
		}
		return item.Alias, strings.TrimLeft(trimmed[len(item.Prefix):], "/\\")
	}
	return "", userPath
}

func resolveAliasedPath(roots pathroots.PathRoots, alias string, rest string, requireLeaf bool) (string, error) {
	base := strings.TrimSpace(roots.BaseDir(alias))
	if base == "" {
		return "", fmt.Errorf("base dir %s is not configured", alias)
	}
	baseAbs, err := filepath.Abs(pathutil.ExpandHomePath(base))
	if err != nil {
		return "", err
	}
	rest = strings.TrimLeft(strings.TrimSpace(rest), "/\\")
	if rest == "" {
		if requireLeaf {
			return "", fmt.Errorf("invalid path: alias requires a relative file path (for example: %s/notes/todo.md)", alias)
		}
		return baseAbs, nil
	}
	candidate := filepath.Join(baseAbs, rest)
	candAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	if !pathutil.IsWithinDir(baseAbs, candAbs) {
		return "", fmt.Errorf("refusing to access outside allowed base dir %s", alias)
	}
	return candAbs, nil
}

func formatBaseDirHint(roots pathroots.PathRoots) string {
	parts := make([]string, 0, 3)
	if dir := strings.TrimSpace(roots.WorkspaceDir); dir != "" {
		parts = append(parts, fmt.Sprintf("workspace_dir=%s", dir))
	}
	if dir := strings.TrimSpace(roots.FileCacheDir); dir != "" {
		parts = append(parts, fmt.Sprintf("file_cache_dir=%s", dir))
	}
	if dir := strings.TrimSpace(roots.FileStateDir); dir != "" {
		parts = append(parts, fmt.Sprintf("file_state_dir=%s", dir))
	}
	if len(parts) == 0 {
		return "base_dirs=[]"
	}
	return strings.Join(parts, " ")
}
