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

type ReadFileTool struct {
	MaxBytes  int64
	DenyPaths []string
	BaseDirs  []string
}

func NewReadFileTool(maxBytes int64) *ReadFileTool {
	return &ReadFileTool{MaxBytes: maxBytes}
}

func NewReadFileToolWithDenyPaths(maxBytes int64, denyPaths []string, baseDirs ...string) *ReadFileTool {
	tool := &ReadFileTool{MaxBytes: maxBytes, DenyPaths: denyPaths}
	tool.BaseDirs = normalizeBaseDirs(baseDirs)
	return tool
}

func (t *ReadFileTool) Name() string { return "read_file" }

func (t *ReadFileTool) Description() string {
	return "Reads a local text file from disk and returns its content (truncated to a maximum size)."
}

func (t *ReadFileTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path to read. Supports aliases `file_cache_dir/<path>` and `file_state_dir/<path>`.",
			},
		},
		"required": []string{"path"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *ReadFileTool) Execute(_ context.Context, params map[string]any) (string, error) {
	path, _ := params["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("missing required param: path")
	}
	var err error
	path, err = t.resolvePath(path)
	if err != nil {
		return "", err
	}

	if offending, ok := denyPath(path, t.DenyPaths); ok {
		return "", fmt.Errorf("read_file denied for path %q (matched %q)", path, offending)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if t.MaxBytes > 0 && int64(len(data)) > t.MaxBytes {
		data = data[:t.MaxBytes]
	}
	return string(data), nil
}

func (t *ReadFileTool) resolvePath(rawPath string) (string, error) {
	rawPath = strings.TrimSpace(rawPath)
	alias, rest := detectWritePathAlias(rawPath)
	if alias == "" {
		return pathutil.ExpandHomePath(rawPath), nil
	}
	rest = strings.TrimLeft(strings.TrimSpace(rest), "/\\")
	if rest == "" {
		return "", fmt.Errorf("invalid path: alias requires a relative file path (for example: %s/notes/todo.md)", alias)
	}

	base := selectBaseForAlias(t.BaseDirs, alias)
	if strings.TrimSpace(base) == "" {
		return "", fmt.Errorf("base dir %s is not configured", alias)
	}
	baseAbs, err := filepath.Abs(pathutil.ExpandHomePath(base))
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(baseAbs, rest)
	candAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	if !isWithinDir(baseAbs, candAbs) {
		return "", fmt.Errorf("refusing to read outside allowed base dir %s", alias)
	}
	return candAbs, nil
}

func denyPath(path string, denyPaths []string) (string, bool) {
	if len(denyPaths) == 0 {
		return "", false
	}
	p := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	base := filepath.Base(p)

	for _, d := range denyPaths {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		dClean := filepath.ToSlash(filepath.Clean(d))

		// If user provided a basename (common), deny any file with that basename.
		if !strings.Contains(dClean, "/") {
			if base == dClean {
				return d, true
			}
			continue
		}

		// If a full path was provided, deny exact match or path-suffix match.
		if p == dClean || strings.HasSuffix(p, "/"+dClean) {
			return d, true
		}

		// Also deny by basename of the deny path.
		if b := filepath.Base(dClean); b != "" && base == b {
			return d, true
		}
	}
	return "", false
}
