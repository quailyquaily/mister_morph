package imagesession

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

const (
	manifestVersion = 1
	maxRecentImages = 12
)

var ErrActiveImageMissing = errors.New("active image file is missing")

type Scope struct {
	Runtime        string `json:"runtime,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
}

type ImageRecord struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	MIMEType  string    `json:"mime_type,omitempty"`
	Bytes     int       `json:"bytes,omitempty"`
	Source    string    `json:"source,omitempty"`
	ParentIDs []string  `json:"parent_ids,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Manifest struct {
	Version       int           `json:"version"`
	Scope         Scope         `json:"scope"`
	ActiveImageID string        `json:"active_image_id,omitempty"`
	Images        []ImageRecord `json:"images,omitempty"`
}

type Store struct {
	mu       sync.Mutex
	stateDir string
}

func NewStore(fileStateDir string) *Store {
	fileStateDir = strings.TrimSpace(fileStateDir)
	if fileStateDir == "" {
		return nil
	}
	return &Store{stateDir: filepath.Join(fileStateDir, "image_sessions")}
}

func NewScope(conversationID string) Scope {
	conversationID = strings.TrimSpace(conversationID)
	return Scope{
		Runtime:        runtimeFromConversationID(conversationID),
		ConversationID: conversationID,
	}
}

func (s Scope) Empty() bool {
	return strings.TrimSpace(s.ConversationID) == ""
}

func (s *Store) Record(scope Scope, roots pathroots.PathRoots, rec ImageRecord) (ImageRecord, error) {
	if s == nil || scope.Empty() || strings.TrimSpace(rec.Path) == "" {
		return rec, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	manifest, err := s.loadLocked(scope)
	if err != nil {
		return rec, err
	}
	manifest, _, err = pruneMissingImages(manifest, roots)
	if err != nil {
		return rec, err
	}
	if strings.TrimSpace(rec.ID) == "" {
		rec.ID = "img_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	rec.Path = filepath.ToSlash(strings.TrimSpace(rec.Path))
	rec.MIMEType = strings.TrimSpace(rec.MIMEType)
	rec.Source = strings.TrimSpace(rec.Source)
	rec.ParentIDs = cleanIDs(rec.ParentIDs)
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	} else {
		rec.CreatedAt = rec.CreatedAt.UTC()
	}
	manifest.Images = append(manifest.Images, rec)
	if len(manifest.Images) > maxRecentImages {
		manifest.Images = manifest.Images[len(manifest.Images)-maxRecentImages:]
	}
	manifest.ActiveImageID = rec.ID
	return rec, s.saveLocked(manifest)
}

func (s *Store) Active(scope Scope, roots pathroots.PathRoots) (*ImageRecord, error) {
	if s == nil || scope.Empty() {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	manifest, err := s.loadLocked(scope)
	if err != nil {
		return nil, err
	}
	activeBefore := strings.TrimSpace(manifest.ActiveImageID)
	manifest, changed, err := pruneMissingImages(manifest, roots)
	if err != nil {
		return nil, err
	}
	if changed {
		if saveErr := s.saveLocked(manifest); saveErr != nil {
			return nil, saveErr
		}
	}
	if activeBefore != "" && strings.TrimSpace(manifest.ActiveImageID) == "" {
		return nil, ErrActiveImageMissing
	}
	for i := range manifest.Images {
		if manifest.Images[i].ID == manifest.ActiveImageID {
			out := manifest.Images[i]
			return &out, nil
		}
	}
	return nil, nil
}

func (s *Store) ClearActive(scope Scope) error {
	if s == nil || scope.Empty() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	manifest, err := s.loadLocked(scope)
	if err != nil {
		return err
	}
	manifest.ActiveImageID = ""
	return s.saveLocked(manifest)
}

func (s *Store) PromptBlock(scope Scope, roots pathroots.PathRoots, limit int) (agent.PromptBlock, error) {
	if s == nil || scope.Empty() {
		return agent.PromptBlock{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	manifest, err := s.loadLocked(scope)
	if err != nil {
		return agent.PromptBlock{}, err
	}
	manifest, changed, err := pruneMissingImages(manifest, roots)
	if err != nil {
		return agent.PromptBlock{}, err
	}
	if changed {
		if saveErr := s.saveLocked(manifest); saveErr != nil {
			return agent.PromptBlock{}, saveErr
		}
	}
	if strings.TrimSpace(manifest.ActiveImageID) == "" {
		return agent.PromptBlock{}, nil
	}
	if limit <= 0 {
		limit = 3
	}
	if limit > len(manifest.Images) {
		limit = len(manifest.Images)
	}
	recent := manifest.Images[len(manifest.Images)-limit:]
	var activeImage map[string]any
	recentImages := make([]map[string]any, 0, len(recent))
	for _, rec := range recent {
		active := rec.ID == manifest.ActiveImageID
		item := map[string]any{
			"id":     rec.ID,
			"path":   rec.Path,
			"active": active,
		}
		if active {
			activeImage = map[string]any{
				"id":        rec.ID,
				"path":      rec.Path,
				"mime_type": rec.MIMEType,
				"note":      "Use image_edit with use_active_image=true for follow-up edits.",
			}
		}
		recentImages = append(recentImages, item)
	}
	if activeImage == nil {
		for _, rec := range manifest.Images {
			if rec.ID != manifest.ActiveImageID {
				continue
			}
			activeImage = map[string]any{
				"id":        rec.ID,
				"path":      rec.Path,
				"mime_type": rec.MIMEType,
				"note":      "Use image_edit with use_active_image=true for follow-up edits.",
			}
			break
		}
	}
	payload := map[string]any{
		"active_image":  activeImage,
		"recent_images": recentImages,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return agent.PromptBlock{}, err
	}
	return agent.PromptBlock{Content: "Current image session state:\n" + string(data)}, nil
}

func (s *Store) ProtectedPaths(fileCacheDir string) (map[string]bool, error) {
	out := map[string]bool{}
	if s == nil || strings.TrimSpace(fileCacheDir) == "" {
		return out, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.stateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, err
	}
	roots := pathroots.New("", fileCacheDir, "")
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.stateDir, entry.Name()))
		if err != nil {
			continue
		}
		var manifest Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}
		for _, rec := range manifest.Images {
			path, err := ResolveAliasPath(roots, rec.Path)
			if err == nil {
				out[filepath.Clean(path)] = true
			}
		}
	}
	return out, nil
}

func ResolveAliasPath(roots pathroots.PathRoots, rawPath string) (string, error) {
	roots = pathroots.New(roots.WorkspaceDir, roots.FileCacheDir, roots.FileStateDir)
	rawPath = pathutil.ExpandHomePath(strings.TrimSpace(rawPath))
	alias, rest := pathAlias(rawPath)
	if alias == "" {
		return "", fmt.Errorf("image session path must use root alias")
	}
	if alias != "file_cache_dir" && alias != "workspace_dir" {
		return "", fmt.Errorf("image session path cannot use %s", alias)
	}
	base := strings.TrimSpace(roots.BaseDir(alias))
	if base == "" {
		return "", fmt.Errorf("base dir %s is not configured", alias)
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	rest = strings.TrimLeft(strings.TrimSpace(rest), "/\\")
	if rest == "" {
		return "", fmt.Errorf("image session path requires a file path")
	}
	candidate := filepath.Join(baseAbs, rest)
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	if !pathutil.IsWithinDir(baseAbs, candidateAbs) {
		return "", fmt.Errorf("refusing image session path outside %s", alias)
	}
	return candidateAbs, nil
}

func (s *Store) loadLocked(scope Scope) (Manifest, error) {
	path := s.manifestPath(scope)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Manifest{Version: manifestVersion, Scope: normalizeScope(scope)}, nil
		}
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	manifest.Version = manifestVersion
	manifest.Scope = normalizeScope(scope)
	return manifest, nil
}

func (s *Store) saveLocked(manifest Manifest) error {
	if s == nil || manifest.Scope.Empty() {
		return nil
	}
	if err := os.MkdirAll(s.stateDir, 0o700); err != nil {
		return err
	}
	manifest.Version = manifestVersion
	manifest.Scope = normalizeScope(manifest.Scope)
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	path := s.manifestPath(manifest.Scope)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) manifestPath(scope Scope) string {
	key := normalizeScope(scope).key()
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(s.stateDir, hex.EncodeToString(sum[:])+".json")
}

func pruneMissingImages(manifest Manifest, roots pathroots.PathRoots) (Manifest, bool, error) {
	if len(manifest.Images) == 0 {
		if strings.TrimSpace(manifest.ActiveImageID) == "" {
			return manifest, false, nil
		}
		manifest.ActiveImageID = ""
		return manifest, true, nil
	}
	changed := false
	kept := manifest.Images[:0]
	activeSeen := false
	for _, rec := range manifest.Images {
		path, err := ResolveAliasPath(roots, rec.Path)
		if err != nil {
			kept = append(kept, rec)
			if rec.ID == manifest.ActiveImageID {
				activeSeen = true
			}
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			changed = true
			continue
		}
		kept = append(kept, rec)
		if rec.ID == manifest.ActiveImageID {
			activeSeen = true
		}
	}
	manifest.Images = kept
	if strings.TrimSpace(manifest.ActiveImageID) != "" && !activeSeen {
		manifest.ActiveImageID = ""
		changed = true
	}
	return manifest, changed, nil
}

func normalizeScope(scope Scope) Scope {
	scope.ConversationID = strings.TrimSpace(scope.ConversationID)
	scope.Runtime = strings.TrimSpace(scope.Runtime)
	if scope.Runtime == "" {
		scope.Runtime = runtimeFromConversationID(scope.ConversationID)
	}
	return scope
}

func (s Scope) key() string {
	s = normalizeScope(s)
	return s.Runtime + "\n" + s.ConversationID
}

func runtimeFromConversationID(conversationID string) string {
	conversationID = strings.TrimSpace(conversationID)
	switch {
	case strings.HasPrefix(conversationID, "tg:"):
		return "telegram"
	case strings.HasPrefix(conversationID, "slack:"):
		return "slack"
	case strings.HasPrefix(conversationID, "lark:"):
		return "lark"
	case strings.HasPrefix(conversationID, "line:"):
		return "line"
	case strings.HasPrefix(conversationID, "console:"):
		return "console"
	case strings.HasPrefix(conversationID, "chat:"):
		return "chat"
	default:
		return ""
	}
}

func cleanIDs(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, raw := range in {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func pathAlias(userPath string) (string, string) {
	trimmed := strings.TrimLeft(userPath, "/\\")
	lower := strings.ToLower(trimmed)
	for _, item := range []struct {
		alias  string
		prefix string
	}{
		{"workspace_dir", "workspace_dir/"},
		{"workspace_dir", "workspace_dir\\"},
		{"file_cache_dir", "file_cache_dir/"},
		{"file_cache_dir", "file_cache_dir\\"},
		{"file_state_dir", "file_state_dir/"},
		{"file_state_dir", "file_state_dir\\"},
	} {
		if strings.HasPrefix(lower, item.prefix) {
			return item.alias, strings.TrimLeft(trimmed[len(item.prefix):], "/\\")
		}
	}
	switch lower {
	case "workspace_dir", "file_cache_dir", "file_state_dir":
		return lower, ""
	default:
		return "", userPath
	}
}
