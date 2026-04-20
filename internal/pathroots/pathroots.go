package pathroots

import (
	"context"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

type PathRoots struct {
	WorkspaceDir string
	FileCacheDir string
	FileStateDir string
}

func New(workspaceDir string, fileCacheDir string, fileStateDir string) PathRoots {
	return PathRoots{
		WorkspaceDir: normalizeRoot(workspaceDir),
		FileCacheDir: normalizeRoot(fileCacheDir),
		FileStateDir: normalizeRoot(fileStateDir),
	}
}

func (r PathRoots) WithWorkspaceDir(dir string) PathRoots {
	r.WorkspaceDir = normalizeRoot(dir)
	return r
}

func (r PathRoots) BaseDir(alias string) string {
	switch strings.ToLower(strings.TrimSpace(alias)) {
	case "workspace_dir":
		return strings.TrimSpace(r.WorkspaceDir)
	case "file_cache_dir":
		return strings.TrimSpace(r.FileCacheDir)
	case "file_state_dir":
		return strings.TrimSpace(r.FileStateDir)
	default:
		return ""
	}
}

func (r PathRoots) DefaultFileDir() string {
	if dir := strings.TrimSpace(r.WorkspaceDir); dir != "" {
		return dir
	}
	return strings.TrimSpace(r.FileCacheDir)
}

func (r PathRoots) AllowedBaseDirs() []string {
	out := make([]string, 0, 3)
	for _, dir := range []string{r.WorkspaceDir, r.FileCacheDir, r.FileStateDir} {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		out = append(out, dir)
	}
	return out
}

type workspaceDirContextKey struct{}

func WithWorkspaceDir(ctx context.Context, dir string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, workspaceDirContextKey{}, normalizeRoot(dir))
}

func WorkspaceDirFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	dir, ok := ctx.Value(workspaceDirContextKey{}).(string)
	if !ok {
		return "", false
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", false
	}
	return dir, true
}

func Resolve(ctx context.Context, fallback PathRoots) PathRoots {
	fallback = New(fallback.WorkspaceDir, fallback.FileCacheDir, fallback.FileStateDir)
	if dir, ok := WorkspaceDirFromContext(ctx); ok {
		return fallback.WithWorkspaceDir(dir)
	}
	return fallback
}

func normalizeRoot(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	return pathutil.ExpandHomePath(dir)
}
