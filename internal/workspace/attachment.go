package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/quailyquaily/mistermorph/internal/fsstore"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

const attachmentStoreVersion = 1

type Attachment struct {
	WorkspaceDir string `json:"workspace_dir"`
}

type Store struct {
	path     string
	lockPath string
	mu       sync.Mutex
}

type attachmentFile struct {
	Version     int                   `json:"version"`
	Attachments map[string]Attachment `json:"attachments"`
}

func NewStore(path string) *Store {
	path = strings.TrimSpace(path)
	lockPath := ""
	if path != "" {
		lockPath = path + ".lck"
	}
	return &Store{
		path:     path,
		lockPath: lockPath,
	}
}

func (s *Store) Get(scopeKey string) (Attachment, bool, error) {
	scopeKey = normalizeScopeKey(scopeKey)
	if s == nil || scopeKey == "" {
		return Attachment{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.readLocked()
	if err != nil {
		return Attachment{}, false, err
	}
	att, ok := data.Attachments[scopeKey]
	if !ok {
		return Attachment{}, false, nil
	}
	if strings.TrimSpace(att.WorkspaceDir) == "" {
		return Attachment{}, false, nil
	}
	return att, true, nil
}

func (s *Store) Set(scopeKey string, attachment Attachment) (Attachment, bool, error) {
	scopeKey = normalizeScopeKey(scopeKey)
	attachment.WorkspaceDir = strings.TrimSpace(attachment.WorkspaceDir)
	if s == nil || scopeKey == "" {
		return Attachment{}, false, nil
	}
	if attachment.WorkspaceDir == "" {
		return Attachment{}, false, fmt.Errorf("workspace dir is required")
	}
	var prev Attachment
	var hadPrev bool
	err := s.withMutationLock(func() error {
		data, err := s.readLocked()
		if err != nil {
			return err
		}
		prev, hadPrev = data.Attachments[scopeKey]
		data.Attachments[scopeKey] = attachment
		return s.writeLocked(data)
	})
	if err != nil {
		return Attachment{}, false, err
	}
	return prev, hadPrev, nil
}

func (s *Store) Delete(scopeKey string) (Attachment, bool, error) {
	scopeKey = normalizeScopeKey(scopeKey)
	if s == nil || scopeKey == "" {
		return Attachment{}, false, nil
	}
	var prev Attachment
	var hadPrev bool
	err := s.withMutationLock(func() error {
		data, err := s.readLocked()
		if err != nil {
			return err
		}
		prev, hadPrev = data.Attachments[scopeKey]
		if !hadPrev {
			return nil
		}
		delete(data.Attachments, scopeKey)
		return s.writeLocked(data)
	})
	if err != nil {
		return Attachment{}, false, err
	}
	return prev, true, nil
}

func (s *Store) withMutationLock(fn func() error) error {
	if s == nil || fn == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(s.lockPath) == "" {
		return fn()
	}
	return fsstore.WithLock(context.Background(), s.lockPath, fn)
}

func (s *Store) readLocked() (attachmentFile, error) {
	data := attachmentFile{
		Version:     attachmentStoreVersion,
		Attachments: map[string]Attachment{},
	}
	if s == nil || strings.TrimSpace(s.path) == "" {
		return data, nil
	}
	var persisted attachmentFile
	found, err := fsstore.ReadJSON(strings.TrimSpace(s.path), &persisted)
	if err != nil {
		return data, err
	}
	if !found {
		return data, nil
	}
	if persisted.Version <= 0 {
		persisted.Version = attachmentStoreVersion
	}
	if persisted.Attachments == nil {
		persisted.Attachments = map[string]Attachment{}
	}
	for scopeKey, att := range persisted.Attachments {
		scopeKey = normalizeScopeKey(scopeKey)
		att.WorkspaceDir = strings.TrimSpace(att.WorkspaceDir)
		if scopeKey == "" || att.WorkspaceDir == "" {
			continue
		}
		data.Attachments[scopeKey] = att
	}
	return data, nil
}

func (s *Store) writeLocked(data attachmentFile) error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return nil
	}
	if data.Attachments == nil {
		data.Attachments = map[string]Attachment{}
	}
	data.Version = attachmentStoreVersion
	return fsstore.WriteJSONAtomic(strings.TrimSpace(s.path), data, fsstore.FileOptions{})
}

type CommandAction string

const (
	CommandStatus CommandAction = "status"
	CommandAttach CommandAction = "attach"
	CommandDetach CommandAction = "detach"
)

type Command struct {
	Action CommandAction
	Dir    string
}

type CommandResult struct {
	Reply        string
	WorkspaceDir string
}

func ParseCommandArgs(args string) (Command, error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return Command{Action: CommandStatus}, nil
	}
	parts := strings.Fields(args)
	if len(parts) == 0 {
		return Command{Action: CommandStatus}, nil
	}
	switch strings.ToLower(strings.TrimSpace(parts[0])) {
	case "attach":
		dir := strings.TrimSpace(args[len(parts[0]):])
		if dir == "" {
			return Command{}, fmt.Errorf("usage: /workspace | /workspace attach <dir> | /workspace detach")
		}
		return Command{Action: CommandAttach, Dir: dir}, nil
	case "detach":
		if len(parts) != 1 {
			return Command{}, fmt.Errorf("usage: /workspace | /workspace attach <dir> | /workspace detach")
		}
		return Command{Action: CommandDetach}, nil
	default:
		return Command{}, fmt.Errorf("usage: /workspace | /workspace attach <dir> | /workspace detach")
	}
}

func LookupWorkspaceDir(store *Store, scopeKey string) (string, error) {
	if store == nil {
		return "", nil
	}
	att, ok, err := store.Get(scopeKey)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", nil
	}
	return strings.TrimSpace(att.WorkspaceDir), nil
}

func ExecuteStoreCommand(store *Store, scopeKey string, args string, allowRoots []string) (CommandResult, error) {
	if store == nil {
		return CommandResult{}, fmt.Errorf("workspace store is not configured")
	}
	cmd, err := ParseCommandArgs(args)
	if err != nil {
		return CommandResult{}, err
	}
	currentDir, err := LookupWorkspaceDir(store, scopeKey)
	if err != nil {
		return CommandResult{}, err
	}
	switch cmd.Action {
	case CommandStatus:
		return CommandResult{
			Reply:        StatusText(currentDir),
			WorkspaceDir: currentDir,
		}, nil
	case CommandAttach:
		dir, err := ValidateDir(cmd.Dir, allowRoots)
		if err != nil {
			return CommandResult{}, err
		}
		prev, hadPrev, err := store.Set(scopeKey, Attachment{WorkspaceDir: dir})
		if err != nil {
			return CommandResult{}, err
		}
		return CommandResult{
			Reply:        AttachText(prev.WorkspaceDir, dir, hadPrev),
			WorkspaceDir: dir,
		}, nil
	case CommandDetach:
		prev, hadPrev, err := store.Delete(scopeKey)
		if err != nil {
			return CommandResult{}, err
		}
		return CommandResult{
			Reply:        DetachText(prev.WorkspaceDir, hadPrev),
			WorkspaceDir: "",
		}, nil
	default:
		return CommandResult{}, fmt.Errorf("unsupported workspace command")
	}
}

func ResolveInitialWorkspace(cwd string, raw string, disabled bool, allowRoots []string) (string, error) {
	if disabled {
		return "", nil
	}
	target := strings.TrimSpace(raw)
	if target == "" {
		target = strings.TrimSpace(cwd)
	}
	if target == "" {
		return "", nil
	}
	return ValidateDir(target, allowRoots)
}

func ValidateDir(raw string, allowRoots []string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("workspace dir is required")
	}
	resolved, err := filepath.Abs(pathutil.ExpandHomePath(raw))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("workspace dir does not exist: %s", resolved)
		}
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace dir is not a directory: %s", resolved)
	}
	if _, err := os.ReadDir(resolved); err != nil {
		return "", fmt.Errorf("workspace dir is not readable: %s", resolved)
	}
	allowed := normalizedAllowRoots(allowRoots)
	if len(allowed) > 0 {
		for _, root := range allowed {
			if pathutil.IsWithinDir(root, resolved) || filepath.Clean(root) == filepath.Clean(resolved) {
				return resolved, nil
			}
		}
		return "", fmt.Errorf("workspace dir is outside allowed roots: %s", resolved)
	}
	return resolved, nil
}

func StatusText(current string) string {
	current = strings.TrimSpace(current)
	if current == "" {
		return "workspace: (none)"
	}
	return "workspace: " + current
}

func AttachText(oldDir string, newDir string, replaced bool) string {
	oldDir = strings.TrimSpace(oldDir)
	newDir = strings.TrimSpace(newDir)
	if replaced && oldDir != "" && oldDir != newDir {
		return fmt.Sprintf("workspace replaced: %s -> %s", oldDir, newDir)
	}
	if oldDir == newDir && newDir != "" {
		return "workspace unchanged: " + newDir
	}
	return "workspace attached: " + newDir
}

func DetachText(oldDir string, detached bool) string {
	oldDir = strings.TrimSpace(oldDir)
	if !detached || oldDir == "" {
		return "workspace: already detached"
	}
	return "workspace detached: " + oldDir
}

func normalizeScopeKey(scopeKey string) string {
	return strings.TrimSpace(scopeKey)
}

func normalizedAllowRoots(roots []string) []string {
	out := make([]string, 0, len(roots))
	seen := map[string]bool{}
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		absRoot, err := filepath.Abs(pathutil.ExpandHomePath(root))
		if err != nil {
			continue
		}
		absRoot = filepath.Clean(absRoot)
		if seen[absRoot] {
			continue
		}
		seen[absRoot] = true
		out = append(out, absRoot)
	}
	return out
}
