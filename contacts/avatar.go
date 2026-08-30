package contacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/fsstore"
)

const ContactAvatarMaxBytes = 5 << 20

const contactAvatarsDirName = "avatars"

type ContactAvatar struct {
	Data        []byte
	ContentType string
	ModTime     time.Time
}

func (s *FileStore) PutContactAvatar(ctx context.Context, contactID string, raw []byte) error {
	if err := ensureNotCanceled(ctx); err != nil {
		return err
	}
	contactID = strings.TrimSpace(contactID)
	if contactID == "" {
		return fmt.Errorf("contact_id is required")
	}
	if len(raw) == 0 {
		return fmt.Errorf("contact avatar is empty")
	}
	if len(raw) > ContactAvatarMaxBytes {
		return fmt.Errorf("contact avatar exceeds %d bytes", ContactAvatarMaxBytes)
	}
	if _, err := contactAvatarContentType(raw); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return fsstore.WriteBytesAtomic(s.contactAvatarPath(contactID), raw, fsstore.FileOptions{
		DirPerm:  0o700,
		FilePerm: 0o600,
	})
}

func (s *FileStore) ReadContactAvatar(ctx context.Context, contactID string) (ContactAvatar, bool, error) {
	if err := ensureNotCanceled(ctx); err != nil {
		return ContactAvatar{}, false, err
	}
	contactID = strings.TrimSpace(contactID)
	if contactID == "" {
		return ContactAvatar{}, false, fmt.Errorf("contact_id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.contactAvatarPath(contactID)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ContactAvatar{}, false, nil
		}
		return ContactAvatar{}, false, fmt.Errorf("stat contact avatar: %w", err)
	}
	if info.Size() <= 0 || info.Size() > ContactAvatarMaxBytes {
		return ContactAvatar{}, false, fmt.Errorf("contact avatar has invalid size %d", info.Size())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ContactAvatar{}, false, fmt.Errorf("read contact avatar: %w", err)
	}
	contentType, err := contactAvatarContentType(raw)
	if err != nil {
		return ContactAvatar{}, false, err
	}
	return ContactAvatar{
		Data:        raw,
		ContentType: contentType,
		ModTime:     info.ModTime().UTC(),
	}, true, nil
}

func (s *FileStore) DeleteContactAvatar(ctx context.Context, contactID string) error {
	if err := ensureNotCanceled(ctx); err != nil {
		return err
	}
	contactID = strings.TrimSpace(contactID)
	if contactID == "" {
		return fmt.Errorf("contact_id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.contactAvatarPath(contactID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete contact avatar: %w", err)
	}
	return nil
}

func (s *FileStore) ContactAvatarFresh(ctx context.Context, contactID string, now time.Time, ttl time.Duration) (bool, error) {
	if err := ensureNotCanceled(ctx); err != nil {
		return false, err
	}
	contactID = strings.TrimSpace(contactID)
	if contactID == "" {
		return false, fmt.Errorf("contact_id is required")
	}
	if ttl <= 0 {
		return false, fmt.Errorf("contact avatar ttl must be positive")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	info, err := os.Stat(s.contactAvatarPath(contactID))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat contact avatar: %w", err)
	}
	return !now.After(info.ModTime().UTC().Add(ttl)), nil
}

func (s *FileStore) ContactAvatarRevisions(ctx context.Context, contactIDs []string) (map[string]time.Time, error) {
	if err := ensureNotCanceled(ctx); err != nil {
		return nil, err
	}
	out := make(map[string]time.Time)
	if len(contactIDs) == 0 {
		return out, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.contactAvatarsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, fmt.Errorf("read contact avatars: %w", err)
	}
	byName := make(map[string]time.Time, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil, fmt.Errorf("stat contact avatar %s: %w", entry.Name(), infoErr)
		}
		byName[entry.Name()] = info.ModTime().UTC()
	}
	for _, rawID := range contactIDs {
		contactID := strings.TrimSpace(rawID)
		if contactID == "" {
			continue
		}
		if modTime, ok := byName[contactAvatarFilename(contactID)]; ok {
			out[contactID] = modTime
		}
	}
	return out, nil
}

func (s *FileStore) contactAvatarsPath() string {
	return filepath.Join(s.rootPath(), contactAvatarsDirName)
}

func (s *FileStore) contactAvatarPath(contactID string) string {
	return filepath.Join(s.contactAvatarsPath(), contactAvatarFilename(contactID))
}

func contactAvatarFilename(contactID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(contactID)))
	return hex.EncodeToString(sum[:]) + ".image"
}

func contactAvatarContentType(raw []byte) (string, error) {
	contentType := strings.ToLower(strings.TrimSpace(http.DetectContentType(raw)))
	switch contentType {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
		return contentType, nil
	default:
		return "", fmt.Errorf("unsupported contact avatar content type %q", contentType)
	}
}
