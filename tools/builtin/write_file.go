package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type WriteFileTool struct {
	Enabled  bool
	MaxBytes int
	BaseDir  string
}

func NewWriteFileTool(enabled bool, maxBytes int, baseDir string) *WriteFileTool {
	if maxBytes <= 0 {
		maxBytes = 512 * 1024
	}
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		baseDir = "/tmp/.morph-cache"
	}
	return &WriteFileTool{
		Enabled:  enabled,
		MaxBytes: maxBytes,
		BaseDir:  baseDir,
	}
}

func (t *WriteFileTool) Name() string { return "write_file" }

func (t *WriteFileTool) Description() string {
	return "Writes text content to a local file (overwrite or append). Writes are restricted to file_cache_dir."
}

func (t *WriteFileTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path to write. Relative paths are resolved under file_cache_dir. Absolute paths are allowed only if they resolve within file_cache_dir.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Text content to write.",
			},
			"mode": map[string]any{
				"type":        "string",
				"description": "Write mode: overwrite|append (default: overwrite).",
			},
			"mkdirs": map[string]any{
				"type":        "boolean",
				"description": "If true, creates parent directories as needed (under file_cache_dir).",
			},
		},
		"required": []string{"path", "content"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *WriteFileTool) Execute(_ context.Context, params map[string]any) (string, error) {
	if !t.Enabled {
		return "", fmt.Errorf("write_file tool is disabled (enable via config: tools.write_file.enabled=true)")
	}

	path, _ := params["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("missing required param: path")
	}
	baseDir, resolvedPath, err := resolveWritePath(t.BaseDir, path)
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

	mkdirs := false
	if v, ok := params["mkdirs"].(bool); ok {
		mkdirs = v
	}

	if mkdirs {
		dir := filepath.Dir(path)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return "", err
			}
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
		"mkdirs":    mkdirs,
		"max_bytes": t.MaxBytes,
	}, "", "  ")
	return string(out), nil
}

func resolveWritePath(baseDirCfg string, userPath string) (string, string, error) {
	baseDir := expandHomePath(baseDirCfg)
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return "", "", fmt.Errorf("file_cache_dir is not configured")
	}
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return "", "", err
	}

	if err := os.MkdirAll(baseAbs, 0o700); err != nil {
		return "", "", err
	}
	fi, err := os.Lstat(baseAbs)
	if err != nil {
		return "", "", err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return "", "", fmt.Errorf("refusing symlink base dir: %s", baseAbs)
	}
	if !fi.IsDir() {
		return "", "", fmt.Errorf("base dir is not a directory: %s", baseAbs)
	}
	if fi.Mode().Perm() != 0o700 {
		_ = os.Chmod(baseAbs, 0o700)
	}

	userPath = expandHomePath(userPath)
	if strings.TrimSpace(userPath) == "" {
		return "", "", fmt.Errorf("missing required param: path")
	}

	var candidate string
	if filepath.IsAbs(userPath) {
		candidate = filepath.Clean(userPath)
	} else {
		candidate = filepath.Join(baseAbs, userPath)
	}
	candAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", "", err
	}
	if !isWithinDir(baseAbs, candAbs) {
		return "", "", fmt.Errorf("refusing to write outside file_cache_dir (file_cache_dir=%s path=%s)", baseAbs, candAbs)
	}
	return baseAbs, candAbs, nil
}

func isWithinDir(baseAbs string, candAbs string) bool {
	baseAbs = filepath.Clean(baseAbs)
	candAbs = filepath.Clean(candAbs)
	rel, err := filepath.Rel(baseAbs, candAbs)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	return true
}
