package chatinfo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	refid "github.com/quailyquaily/mistermorph/internal/entryutil/refid"
	"github.com/quailyquaily/mistermorph/internal/fsstore"
)

const (
	Filename       = "chat_profile.json"
	LegacyFilename = "chat_id_info.json"
	fileVersion    = 1
	successTTL     = 7 * 24 * time.Hour
	successJitter  = 12 * time.Hour
	failureRetry   = 6 * time.Hour
)

type Info struct {
	ChatID    string    `json:"chat_id"`
	Platform  string    `json:"platform"`
	Type      string    `json:"type,omitempty"`
	Name      string    `json:"name,omitempty"`
	FetchedAt time.Time `json:"fetched_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (i *Info) UnmarshalJSON(data []byte) error {
	var raw struct {
		ChatID        string    `json:"chat_id"`
		Platform      string    `json:"platform"`
		Type          string    `json:"type,omitempty"`
		Name          string    `json:"name,omitempty"`
		IgnoredAvatar string    `json:"avatar_ref,omitempty"`
		FetchedAt     time.Time `json:"fetched_at"`
		ExpiresAt     time.Time `json:"expires_at"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return err
	}
	var trailing struct{}
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing data")
		}
		return fmt.Errorf("trailing data: %w", err)
	}
	*i = Info{
		ChatID:    raw.ChatID,
		Platform:  raw.Platform,
		Type:      raw.Type,
		Name:      raw.Name,
		FetchedAt: raw.FetchedAt,
		ExpiresAt: raw.ExpiresAt,
	}
	return nil
}

type File struct {
	Version int    `json:"version"`
	Items   []Info `json:"items"`
}

type Store struct {
	root string
	mu   sync.Mutex
}

type Refresher interface {
	RefreshChatInfo(ctx context.Context, chatID string) (Info, error)
}

type RefreshFunc func(context.Context, string) (Info, error)

func (fn RefreshFunc) RefreshChatInfo(ctx context.Context, chatID string) (Info, error) {
	return fn(ctx, chatID)
}

func NewStore(root string) *Store {
	return &Store{root: strings.TrimSpace(root)}
}

func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return filepath.Join(strings.TrimSpace(s.root), Filename)
}

func (s *Store) Read(ctx context.Context) ([]Info, bool, error) {
	if err := ensureContext(ctx); err != nil {
		return nil, false, err
	}
	if s == nil || strings.TrimSpace(s.root) == "" {
		return nil, false, fmt.Errorf("chat profile store root is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked()
}

func (s *Store) Write(ctx context.Context, items []Info) error {
	if err := ensureContext(ctx); err != nil {
		return err
	}
	if s == nil || strings.TrimSpace(s.root) == "" {
		return fmt.Errorf("chat profile store root is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeLocked(items)
}

func (s *Store) Get(ctx context.Context, now time.Time, chatID string, refresher Refresher) (Info, bool, error) {
	if err := ensureContext(ctx); err != nil {
		return Info{}, false, err
	}
	chatID, err := NormalizeChatID(chatID)
	if err != nil {
		return Info{}, false, err
	}
	if s == nil || strings.TrimSpace(s.root) == "" {
		return Info{}, false, fmt.Errorf("chat profile store root is required")
	}
	now = normalizeNow(now)

	s.mu.Lock()
	defer s.mu.Unlock()

	items, _, err := s.readLocked()
	if err != nil {
		return Info{}, false, err
	}
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.ChatID), chatID) && item.ExpiresAt.After(now) {
			return normalizeInfoForStore(item, now), true, nil
		}
	}
	if refresher == nil {
		return Info{}, false, nil
	}
	next, err := refresher.RefreshChatInfo(ctx, chatID)
	if err != nil {
		return Info{}, false, err
	}
	next = normalizeFetchedInfo(chatID, next, now)
	items = upsertInfo(items, next)
	if err := s.writeLocked(items); err != nil {
		return Info{}, false, err
	}
	return next, true, nil
}

func (s *Store) RefreshExpired(ctx context.Context, now time.Time, refresher Refresher) error {
	if err := ensureContext(ctx); err != nil {
		return err
	}
	if s == nil || strings.TrimSpace(s.root) == "" {
		return fmt.Errorf("chat profile store root is required")
	}
	if refresher == nil {
		return nil
	}
	now = normalizeNow(now)

	s.mu.Lock()
	defer s.mu.Unlock()

	items, exists, err := s.readLocked()
	if err != nil || !exists {
		return err
	}
	changed := false
	for i := range items {
		item := normalizeInfoForStore(items[i], now)
		if strings.TrimSpace(item.ChatID) == "" || item.ExpiresAt.After(now) {
			items[i] = item
			continue
		}
		next, refreshErr := refresher.RefreshChatInfo(ctx, item.ChatID)
		if refreshErr != nil {
			item.ExpiresAt = now.Add(failureRetry)
			items[i] = item
			changed = true
			continue
		}
		items[i] = normalizeFetchedInfo(item.ChatID, next, now)
		changed = true
	}
	if !changed {
		return nil
	}
	return s.writeLocked(items)
}

func NormalizeChatID(raw string) (string, error) {
	protocol, id, ok := refid.Parse(raw)
	if !ok {
		return "", fmt.Errorf("invalid chat_id: %s", strings.TrimSpace(raw))
	}
	value := protocol + ":" + id
	switch protocol {
	case "tg":
		if _, _, err := refid.ParseTelegramChatIDHint(value); err != nil {
			return "", err
		}
	case "slack":
		if _, _, _, err := refid.ParseSlackChatIDHint(value); err != nil {
			return "", err
		}
	case "line":
		if _, _, err := refid.ParseLineChatIDHint(value); err != nil {
			return "", err
		}
	case "lark":
		if _, _, err := refid.ParseLarkChatIDHint(value); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("invalid chat_id: %s", strings.TrimSpace(raw))
	}
	return value, nil
}

func PlatformFromChatID(chatID string) string {
	protocol, _, ok := refid.Parse(chatID)
	if !ok {
		return ""
	}
	switch protocol {
	case "tg":
		return "telegram"
	case "slack", "line", "lark":
		return protocol
	default:
		return ""
	}
}

func (s *Store) readLocked() ([]Info, bool, error) {
	var file File
	exists, err := fsstore.ReadJSONStrict(s.Path(), &file)
	if err != nil {
		return nil, exists, err
	}
	if !exists {
		legacyPath := filepath.Join(strings.TrimSpace(s.root), LegacyFilename)
		legacyExists, legacyErr := fsstore.ReadJSONStrict(legacyPath, &file)
		if legacyErr != nil || !legacyExists {
			return nil, legacyExists, legacyErr
		}
		items, _, err := normalizeFileItems(file)
		if err != nil {
			return nil, true, err
		}
		if err := s.writeLocked(items); err != nil {
			return nil, true, err
		}
		return items, true, nil
	}
	items, _, err := normalizeFileItems(file)
	if err != nil {
		return nil, true, err
	}
	if fileContainsLegacyAvatarRef(s.Path()) {
		if err := s.writeLocked(items); err != nil {
			return nil, true, err
		}
	}
	return items, true, nil
}

func fileContainsLegacyAvatarRef(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return bytes.Contains(raw, []byte(`"avatar_ref"`))
}

func normalizeFileItems(file File) ([]Info, bool, error) {
	if file.Version != fileVersion {
		return nil, true, fmt.Errorf("unsupported chat_profile version: %d", file.Version)
	}
	items := normalizeItems(file.Items, time.Now().UTC())
	return items, true, nil
}

func (s *Store) writeLocked(items []Info) error {
	items = normalizeItems(items, time.Now().UTC())
	return fsstore.WriteJSONAtomic(s.Path(), File{
		Version: fileVersion,
		Items:   items,
	}, fsstore.FileOptions{DirPerm: 0o700, FilePerm: 0o600})
}

func normalizeItems(items []Info, now time.Time) []Info {
	byID := map[string]Info{}
	for _, item := range items {
		item = normalizeInfoForStore(item, now)
		if strings.TrimSpace(item.ChatID) == "" {
			continue
		}
		byID[item.ChatID] = item
	}
	out := make([]Info, 0, len(byID))
	for _, item := range byID {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.TrimSpace(out[i].ChatID) < strings.TrimSpace(out[j].ChatID)
	})
	return out
}

func normalizeInfoForStore(item Info, now time.Time) Info {
	if normalized, err := NormalizeChatID(item.ChatID); err == nil {
		item.ChatID = normalized
	} else {
		item.ChatID = strings.TrimSpace(item.ChatID)
	}
	item.Platform = strings.TrimSpace(item.Platform)
	if item.Platform == "" {
		item.Platform = PlatformFromChatID(item.ChatID)
	}
	item.Type = strings.TrimSpace(item.Type)
	item.Name = strings.TrimSpace(item.Name)
	if item.FetchedAt.IsZero() {
		item.FetchedAt = now.UTC()
	} else {
		item.FetchedAt = item.FetchedAt.UTC()
	}
	if item.ExpiresAt.IsZero() {
		item.ExpiresAt = expiresAtFor(item.ChatID, now)
	} else {
		item.ExpiresAt = item.ExpiresAt.UTC()
	}
	return item
}

func normalizeFetchedInfo(chatID string, item Info, now time.Time) Info {
	item.ChatID = chatID
	item = normalizeInfoForStore(item, now)
	item.FetchedAt = now.UTC()
	item.ExpiresAt = expiresAtFor(chatID, now)
	return item
}

func upsertInfo(items []Info, next Info) []Info {
	for i := range items {
		if strings.EqualFold(strings.TrimSpace(items[i].ChatID), strings.TrimSpace(next.ChatID)) {
			items[i] = next
			return items
		}
	}
	return append(items, next)
}

func expiresAtFor(chatID string, now time.Time) time.Time {
	now = normalizeNow(now)
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.TrimSpace(chatID)))
	span := int64(successJitter * 2)
	offset := time.Duration(int64(h.Sum32())%span) - successJitter
	return now.Add(successTTL + offset).UTC()
}

func normalizeNow(now time.Time) time.Time {
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}

func ensureContext(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
