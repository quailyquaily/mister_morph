package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

type WriteFileTool struct {
	Enabled  bool
	MaxBytes int
	Roots    pathroots.PathRoots
}

func NewWriteFileTool(enabled bool, maxBytes int, roots pathroots.PathRoots) *WriteFileTool {
	if maxBytes <= 0 {
		maxBytes = 512 * 1024
	}
	return &WriteFileTool{
		Enabled:  enabled,
		MaxBytes: maxBytes,
		Roots:    pathroots.New(roots.WorkspaceDir, roots.FileCacheDir, roots.FileStateDir),
	}
}

func (t *WriteFileTool) Name() string { return "write_file" }

func (t *WriteFileTool) Description() string {
	return "Writes text content to a local file (overwrite or append). Writes are restricted to workspace_dir, file_cache_dir, or file_state_dir."
}

func (t *WriteFileTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path to write. Relative paths are resolved under workspace_dir when attached, otherwise under file_cache_dir. Absolute paths are allowed only if they resolve within workspace_dir, file_cache_dir, or file_state_dir. Prefix with workspace_dir/ or file_state_dir/ to force a base dir.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Text content to write.",
			},
			"mode": map[string]any{
				"type":        "string",
				"description": "Write mode: overwrite|append (default: overwrite).",
			},
		},
		"required": []string{"path", "content"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *WriteFileTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if !t.Enabled {
		return "", fmt.Errorf("write_file tool is disabled (enable via config: tools.write_file.enabled=true)")
	}

	path, _ := params["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("missing required param: path")
	}
	roots := resolveLocalPathRoots(ctx, t.Roots)
	baseDir, resolvedPath, err := resolveWritePath(roots, path)
	if err != nil {
		return "", err
	}
	path = resolvedPath

	content, _ := params["content"].(string)
	if t.MaxBytes > 0 && len(content) > t.MaxBytes {
		return "", fmt.Errorf("content too large (%d bytes > %d max)", len(content), t.MaxBytes)
	}

	mode, _ := params["mode"].(string)
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "overwrite"
	}

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
	}

	switch mode {
	case "overwrite":
		err = os.WriteFile(path, []byte(content), 0o644)
	case "append":
		f, openErr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if openErr != nil {
			return "", openErr
		}
		_, err = f.WriteString(content)
		_ = f.Close()
	default:
		return "", fmt.Errorf("invalid mode: %s (expected overwrite|append)", mode)
	}
	if err != nil {
		return "", err
	}

	abs, _ := filepath.Abs(path)
	out, _ := json.MarshalIndent(map[string]any{
		"path":      path,
		"abs_path":  abs,
		"base_dir":  baseDir,
		"bytes":     len(content),
		"mode":      mode,
		"max_bytes": t.MaxBytes,
	}, "", "  ")
	return string(out), nil
}

func resolveWritePath(roots pathroots.PathRoots, userPath string) (string, string, error) {
	roots = pathroots.New(roots.WorkspaceDir, roots.FileCacheDir, roots.FileStateDir)
	if strings.TrimSpace(roots.FileCacheDir) == "" && strings.TrimSpace(roots.FileStateDir) == "" && strings.TrimSpace(roots.WorkspaceDir) == "" {
		return "", "", fmt.Errorf("workspace_dir/file_cache_dir/file_state_dir is not configured")
	}

	userPath = pathutil.ExpandHomePath(userPath)
	userPath = strings.TrimSpace(userPath)
	if userPath == "" {
		return "", "", fmt.Errorf("missing required param: path")
	}

	if alias, rest := detectPathAlias(userPath); alias != "" {
		resolved, err := resolveAliasedPath(roots, alias, rest, true)
		if err != nil {
			return "", "", err
		}
		base := strings.TrimSpace(roots.BaseDir(alias))
		baseAbs, err := ensureWriteBaseDir(base)
		if err != nil {
			return "", "", err
		}
		return baseAbs, resolved, nil
	}

	if filepath.IsAbs(userPath) {
		candAbs, err := filepath.Abs(filepath.Clean(userPath))
		if err != nil {
			return "", "", err
		}
		for _, base := range roots.AllowedBaseDirs() {
			baseAbs, err := filepath.Abs(base)
			if err != nil {
				continue
			}
			if !pathutil.IsWithinDir(baseAbs, candAbs) && filepath.Clean(baseAbs) != filepath.Clean(candAbs) {
				continue
			}
			baseAbs, err = ensureWriteBaseDir(baseAbs)
			if err != nil {
				return "", "", err
			}
			return baseAbs, candAbs, nil
		}
		return "", "", fmt.Errorf("refusing to write outside allowed base dirs (%s path=%s)", formatBaseDirHint(roots), candAbs)
	}

	defaultBase := strings.TrimSpace(roots.DefaultFileDir())
	defaultAlias := "file_cache_dir"
	if strings.TrimSpace(roots.WorkspaceDir) != "" {
		defaultAlias = "workspace_dir"
	}
	return resolveWritePathWithBase(defaultBase, defaultAlias, userPath, formatBaseDirHint(roots))
}

func resolveWritePathWithBase(baseDir string, alias string, userPath string, hint string) (string, string, error) {
	baseAbs, err := ensureWriteBaseDir(baseDir)
	if err != nil {
		return "", "", err
	}
	userPath = strings.TrimLeft(strings.TrimSpace(userPath), "/\\")
	if userPath == "" {
		return "", "", fmt.Errorf("invalid path: alias requires a relative file path (for example: %s/notes/todo.md)", alias)
	}
	candidate := filepath.Join(baseAbs, userPath)
	candAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", "", err
	}
	if !pathutil.IsWithinDir(baseAbs, candAbs) {
		return "", "", fmt.Errorf("refusing to write outside allowed base dirs (%s path=%s)", hint, candAbs)
	}
	return baseAbs, candAbs, nil
}

func ensureWriteBaseDir(baseDir string) (string, error) {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return "", fmt.Errorf("missing base dir")
	}
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(baseAbs, 0o700); err != nil {
		return "", err
	}
	fi, err := os.Lstat(baseAbs)
	if err != nil {
		return "", err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("refusing symlink base dir: %s", baseAbs)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("base dir is not a directory: %s", baseAbs)
	}
	if fi.Mode().Perm() != 0o700 {
		_ = os.Chmod(baseAbs, 0o700)
	}
	return baseAbs, nil
}
