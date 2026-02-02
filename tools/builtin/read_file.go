package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ReadFileTool struct {
	MaxBytes    int64
	DenyPaths   []string
	AllowedDirs []string
}

func NewReadFileTool(maxBytes int64) *ReadFileTool {
	return &ReadFileTool{MaxBytes: maxBytes}
}

func NewReadFileToolWithDenyPaths(maxBytes int64, denyPaths []string) *ReadFileTool {
	return &ReadFileTool{MaxBytes: maxBytes, DenyPaths: denyPaths}
}

func NewReadFileToolWithOptions(maxBytes int64, denyPaths []string, allowedDirs []string) *ReadFileTool {
	return &ReadFileTool{MaxBytes: maxBytes, DenyPaths: denyPaths, AllowedDirs: allowedDirs}
}

func (t *ReadFileTool) Name() string { return "read_file" }

func (t *ReadFileTool) Description() string {
	return "Reads a local text file from disk and returns its content (truncated to a maximum size)."
}

func (t *ReadFileTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "File path to read."},
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

	if containsDotDot(path) {
		return "", fmt.Errorf("path traversal not allowed: %s", path)
	}

	path = expandHomePath(path)

	if offending, ok := denyPath(path, t.DenyPaths); ok {
		return "", fmt.Errorf("read_file denied for path %q (matched %q)", path, offending)
	}

	cleaned := filepath.Clean(path)

	if len(t.AllowedDirs) > 0 {
		absPath, err := filepath.Abs(cleaned)
		if err != nil {
			return "", fmt.Errorf("invalid path: %w", err)
		}
		if !isWithinAnyDir(absPath, t.AllowedDirs) {
			return "", fmt.Errorf("read_file denied: path %q is not within any allowed directory", path)
		}
	}

	// When allowed_dirs is set, reject symlinks to prevent allowlist bypass
	// (a symlink inside an allowed directory could point outside it).
	if len(t.AllowedDirs) > 0 {
		fi, err := os.Lstat(cleaned)
		if err != nil {
			return "", err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("read_file denied: refusing symlink %q", cleaned)
		}
	}

	data, err := os.ReadFile(cleaned)
	if err != nil {
		return "", err
	}
	if t.MaxBytes > 0 && int64(len(data)) > t.MaxBytes {
		data = data[:t.MaxBytes]
	}
	return string(data), nil
}

// containsDotDot returns true if the path contains a ".." component.
// This must be called on the raw (uncleaned) path, since filepath.Clean
// resolves ".." in absolute paths, hiding the traversal.
func containsDotDot(rawPath string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rawPath), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

// isWithinAnyDir checks whether absPath is within at least one of the allowed directories.
func isWithinAnyDir(absPath string, allowedDirs []string) bool {
	for _, dir := range allowedDirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		dirAbs, err := filepath.Abs(expandHomePath(dir))
		if err != nil {
			continue
		}
		if isWithinDir(dirAbs, absPath) {
			return true
		}
	}
	return false
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
