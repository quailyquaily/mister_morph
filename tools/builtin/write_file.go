package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

type WriteFileTool struct {
	Enabled  bool
	MaxBytes int
	BaseDirs []string
}

func NewWriteFileTool(enabled bool, maxBytes int, baseDirs ...string) *WriteFileTool {
	if maxBytes <= 0 {
		maxBytes = 512 * 1024
	}
	cleaned := make([]string, 0, len(baseDirs))
	for _, dir := range baseDirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		cleaned = append(cleaned, dir)
	}
	if len(cleaned) == 0 {
		cleaned = []string{"/var/cache/morph"}
	}
	return &WriteFileTool{
		Enabled:  enabled,
		MaxBytes: maxBytes,
		BaseDirs: cleaned,
	}
}

func (t *WriteFileTool) Name() string { return "write_file" }

func (t *WriteFileTool) Description() string {
	return "Writes text content to a local file (overwrite or append). Writes are restricted to file_cache_dir or file_state_dir."
}

func (t *WriteFileTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path to write. Relative paths are resolved under file_cache_dir. Absolute paths are allowed only if they resolve within file_cache_dir or file_state_dir. Prefix with file_state_dir/ to force state dir.",
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

func (t *WriteFileTool) Execute(_ context.Context, params map[string]any) (string, error) {
	if !t.Enabled {
		return "", fmt.Errorf("write_file tool is disabled (enable via config: tools.write_file.enabled=true)")
	}

	path, _ := params["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("missing required param: path")
	}
	baseDir, resolvedPath, err := resolveWritePath(t.BaseDirs, path)
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

func resolveWritePath(baseDirs []string, userPath string) (string, string, error) {
	bases := normalizeBaseDirs(baseDirs)
	if len(bases) == 0 {
		return "", "", fmt.Errorf("file_cache_dir/file_state_dir is not configured")
	}

	userPath = pathutil.ExpandHomePath(userPath)
	userPath = strings.TrimSpace(userPath)
	if userPath == "" {
		return "", "", fmt.Errorf("missing required param: path")
	}

	if alias, rest := detectWritePathAlias(userPath); alias != "" {
		base := selectBaseForAlias(bases, alias)
		if strings.TrimSpace(base) == "" {
			return "", "", fmt.Errorf("base dir %s is not configured", alias)
		}
		return resolveWritePathWithBase(base, rest, formatBaseDirHint(bases))
	}

	if filepath.IsAbs(userPath) {
		candAbs, err := filepath.Abs(filepath.Clean(userPath))
		if err != nil {
			return "", "", err
		}
		for _, base := range bases {
			baseAbs, err := filepath.Abs(base)
			if err != nil {
				continue
			}
			if !isWithinDir(baseAbs, candAbs) {
				continue
			}
			baseAbs, err = ensureWriteBaseDir(baseAbs)
			if err != nil {
				return "", "", err
			}
			return baseAbs, candAbs, nil
		}
		return "", "", fmt.Errorf("refusing to write outside allowed base dirs (%s path=%s)", formatBaseDirHint(bases), candAbs)
	}

	return resolveWritePathWithBase(bases[0], userPath, formatBaseDirHint(bases))
}

func normalizeBaseDirs(baseDirs []string) []string {
	out := make([]string, 0, len(baseDirs))
	for _, dir := range baseDirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		out = append(out, pathutil.ExpandHomePath(dir))
	}
	return out
}

func detectWritePathAlias(userPath string) (string, string) {
	trimmed := strings.TrimLeft(userPath, "/\\")
	lower := strings.ToLower(trimmed)
	cachePrefix := "file_cache_dir/"
	cachePrefixAlt := "file_cache_dir\\"
	statePrefix := "file_state_dir/"
	statePrefixAlt := "file_state_dir\\"
	switch {
	case lower == "file_cache_dir":
		return "file_cache_dir", ""
	case lower == "file_state_dir":
		return "file_state_dir", ""
	case strings.HasPrefix(lower, cachePrefix):
		return "file_cache_dir", strings.TrimLeft(trimmed[len(cachePrefix):], "/\\")
	case strings.HasPrefix(lower, cachePrefixAlt):
		return "file_cache_dir", strings.TrimLeft(trimmed[len(cachePrefixAlt):], "/\\")
	case strings.HasPrefix(lower, statePrefix):
		return "file_state_dir", strings.TrimLeft(trimmed[len(statePrefix):], "/\\")
	case strings.HasPrefix(lower, statePrefixAlt):
		return "file_state_dir", strings.TrimLeft(trimmed[len(statePrefixAlt):], "/\\")
	default:
		return "", userPath
	}
}

func selectBaseForAlias(bases []string, alias string) string {
	if len(bases) == 0 {
		return ""
	}
	switch alias {
	case "file_cache_dir":
		return bases[0]
	case "file_state_dir":
		if len(bases) > 1 {
			return bases[1]
		}
	}
	return ""
}

func resolveWritePathWithBase(baseDir string, userPath string, hint string) (string, string, error) {
	baseAbs, err := ensureWriteBaseDir(baseDir)
	if err != nil {
		return "", "", err
	}
	userPath = strings.TrimLeft(strings.TrimSpace(userPath), "/\\")
	if userPath == "" {
		return "", "", fmt.Errorf("invalid path: alias requires a relative file path (for example: file_state_dir/notes/todo.md)")
	}
	candidate := filepath.Join(baseAbs, userPath)
	candAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", "", err
	}
	if !isWithinDir(baseAbs, candAbs) {
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

func formatBaseDirHint(bases []string) string {
	if len(bases) == 0 {
		return "base_dirs=[]"
	}
	parts := make([]string, 0, len(bases))
	parts = append(parts, fmt.Sprintf("file_cache_dir=%s", bases[0]))
	if len(bases) > 1 {
		parts = append(parts, fmt.Sprintf("file_state_dir=%s", bases[1]))
	}
	for i := 2; i < len(bases); i++ {
		parts = append(parts, fmt.Sprintf("base_dir_%d=%s", i+1, bases[i]))
	}
	return strings.Join(parts, " ")
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
