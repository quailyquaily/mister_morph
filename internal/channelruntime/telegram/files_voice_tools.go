package telegram

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/imagehistory"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/telegramutil"
)

func messageTextOrCaption(msg *telegramMessage) string {
	if msg == nil {
		return ""
	}
	if strings.TrimSpace(msg.Text) != "" {
		return msg.Text
	}
	return msg.Caption
}

func telegramMessageSentAt(msg *telegramMessage) time.Time {
	if msg != nil && msg.Date > 0 {
		return time.Unix(msg.Date, 0).UTC()
	}
	return time.Now().UTC()
}

func messageHasDownloadableFile(msg *telegramMessage) bool {
	if msg == nil {
		return false
	}
	if msg.Document != nil && strings.TrimSpace(msg.Document.FileID) != "" {
		return true
	}
	if len(msg.Photo) > 0 {
		for _, p := range msg.Photo {
			if strings.TrimSpace(p.FileID) != "" {
				return true
			}
		}
	}
	return false
}

func ensureSecureChildDir(parentDir, childDir string) error {
	parentDir = strings.TrimSpace(parentDir)
	childDir = strings.TrimSpace(childDir)
	if parentDir == "" || childDir == "" {
		return fmt.Errorf("missing parent/child dir")
	}
	parentAbs, err := filepath.Abs(parentDir)
	if err != nil {
		return err
	}
	childAbs, err := filepath.Abs(childDir)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(parentAbs, childAbs)
	if err != nil {
		return err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("child dir is not under parent dir: %s", childAbs)
	}
	return telegramutil.EnsureSecureCacheDir(childAbs)
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "file"
	}
	name = filepath.Base(name)
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '_' || r == '-' || r == '+':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "._- ")
	if out == "" {
		return "file"
	}
	const max = 120
	if len(out) > max {
		out = out[:max]
	}
	return out
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:6])
}

func capUniqueStrings(in []string, max int) []string {
	if len(in) == 0 || max == 0 {
		return nil
	}
	if max < 0 {
		max = 0
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		k := strings.ToLower(v)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, v)
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out
}

func appendDownloadedFilesToTask(task string, files []telegramDownloadedFile, roots pathroots.PathRoots) string {
	task = strings.TrimSpace(task)
	var b strings.Builder
	b.WriteString(task)
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	b.WriteString("Downloaded Telegram file(s) (use bash tool to process these paths):\n")
	for _, f := range files {
		name := strings.TrimSpace(f.OriginalName)
		if name == "" {
			name = "file"
		}
		b.WriteString("- ")
		if strings.TrimSpace(f.Kind) != "" {
			b.WriteString(strings.TrimSpace(f.Kind))
			b.WriteString(": ")
		}
		b.WriteString(name)
		displayPath := imagehistory.AliasPath(f.Path, roots)
		if strings.TrimSpace(displayPath) != "" {
			b.WriteString(" -> ")
			b.WriteString(strings.TrimSpace(displayPath))
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func collectDownloadedImageAttachments(files []telegramDownloadedFile, max int) []busruntime.ImageAttachment {
	if len(files) == 0 || max == 0 {
		return nil
	}
	if max < 0 {
		max = 0
	}
	out := make([]busruntime.ImageAttachment, 0, len(files))
	seen := make(map[string]bool, len(files))
	for _, f := range files {
		if !isDownloadedImageFile(f) {
			continue
		}
		path := strings.TrimSpace(f.Path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, busruntime.ImageAttachment{
			Path:               path,
			SourceMessageID:    strings.TrimSpace(f.SourceMessageID),
			SourceAttachmentID: strings.TrimSpace(f.SourceAttachmentID),
			MIMEType:           strings.TrimSpace(f.MimeType),
		})
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out
}

func isDownloadedImageFile(file telegramDownloadedFile) bool {
	if strings.EqualFold(strings.TrimSpace(file.Kind), "photo") {
		return true
	}
	mimeType := strings.ToLower(strings.TrimSpace(file.MimeType))
	if strings.HasPrefix(mimeType, "image/") {
		return true
	}
	name := strings.TrimSpace(file.Path)
	if name == "" {
		name = strings.TrimSpace(file.OriginalName)
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp", ".heic", ".heif":
		return true
	default:
		return false
	}
}

func downloadTelegramMessageFiles(ctx context.Context, api *telegramAPI, cacheDir string, maxBytes int64, msg *telegramMessage, chatID int64) ([]telegramDownloadedFile, error) {
	if api == nil {
		return nil, fmt.Errorf("telegram api not available")
	}
	if msg == nil {
		return nil, nil
	}
	cacheDir = strings.TrimSpace(cacheDir)
	if cacheDir == "" {
		return nil, fmt.Errorf("missing cache dir")
	}
	if maxBytes <= 0 {
		maxBytes = 20 * 1024 * 1024
	}
	if err := telegramutil.EnsureSecureCacheDir(cacheDir); err != nil {
		return nil, err
	}
	chatDir := filepath.Join(cacheDir, fmt.Sprintf("chat_%d", chatID))
	if err := ensureSecureChildDir(cacheDir, chatDir); err != nil {
		return nil, err
	}

	var out []telegramDownloadedFile
	seen := make(map[string]bool)
	handleMessage := func(m *telegramMessage) error {
		if m == nil {
			return nil
		}

		// document
		if m.Document != nil {
			fileID := strings.TrimSpace(m.Document.FileID)
			if fileID != "" {
				key := "doc:" + fileID
				if !seen[key] {
					seen[key] = true
					f, err := api.getFile(ctx, fileID)
					if err != nil {
						return err
					}
					sourceMessageID := fmt.Sprintf("%d", m.MessageID)
					sourceAttachmentID := firstNonEmptyString(f.FileUniqueID, fileID)
					orig := strings.TrimSpace(m.Document.FileName)
					if orig == "" {
						orig = "document" + filepath.Ext(f.FilePath)
					}
					name := sanitizeFilename(orig)
					base := fmt.Sprintf("tg_%d_%s_%s", m.MessageID, shortHash(fileID), name)
					dst := filepath.Join(chatDir, base)
					if _, err := os.Stat(dst); err == nil {
						out = append(out, telegramDownloadedFile{
							Kind:               "document",
							OriginalName:       orig,
							MimeType:           m.Document.MimeType,
							SizeBytes:          m.Document.FileSize,
							Path:               dst,
							SourceMessageID:    sourceMessageID,
							SourceAttachmentID: sourceAttachmentID,
						})
					} else {
						tmp, err := os.CreateTemp(chatDir, base+".tmp-*")
						if err != nil {
							return err
						}
						tmpPath := tmp.Name()
						_ = tmp.Close()
						_, _, dlErr := api.downloadFileTo(ctx, f.FilePath, tmpPath, maxBytes)
						if dlErr != nil {
							_ = os.Remove(tmpPath)
							return dlErr
						}
						if err := os.Chmod(tmpPath, 0o600); err != nil {
							_ = os.Remove(tmpPath)
							return err
						}
						if err := os.Rename(tmpPath, dst); err != nil {
							_ = os.Remove(tmpPath)
							return err
						}
						_ = os.Chmod(dst, 0o600)
						out = append(out, telegramDownloadedFile{
							Kind:               "document",
							OriginalName:       orig,
							MimeType:           m.Document.MimeType,
							SizeBytes:          m.Document.FileSize,
							Path:               dst,
							SourceMessageID:    sourceMessageID,
							SourceAttachmentID: sourceAttachmentID,
						})
					}
				}
			}
		}

		// photo (download the largest size).
		if len(m.Photo) > 0 {
			var best telegramPhotoSize
			for i := range m.Photo {
				if strings.TrimSpace(m.Photo[i].FileID) == "" {
					continue
				}
				best = m.Photo[i]
			}
			if strings.TrimSpace(best.FileID) != "" {
				key := "photo:" + best.FileID
				if !seen[key] {
					seen[key] = true
					f, err := api.getFile(ctx, best.FileID)
					if err != nil {
						return err
					}
					sourceMessageID := fmt.Sprintf("%d", m.MessageID)
					sourceAttachmentID := firstNonEmptyString(f.FileUniqueID, best.FileID)
					ext := filepath.Ext(f.FilePath)
					orig := "photo" + ext
					name := sanitizeFilename(orig)
					base := fmt.Sprintf("tg_%d_%s_%s", m.MessageID, shortHash(best.FileID), name)
					dst := filepath.Join(chatDir, base)
					if _, err := os.Stat(dst); err == nil {
						out = append(out, telegramDownloadedFile{
							Kind:               "photo",
							OriginalName:       orig,
							SizeBytes:          best.FileSize,
							Path:               dst,
							SourceMessageID:    sourceMessageID,
							SourceAttachmentID: sourceAttachmentID,
						})
					} else {
						tmp, err := os.CreateTemp(chatDir, base+".tmp-*")
						if err != nil {
							return err
						}
						tmpPath := tmp.Name()
						_ = tmp.Close()
						_, _, dlErr := api.downloadFileTo(ctx, f.FilePath, tmpPath, maxBytes)
						if dlErr != nil {
							_ = os.Remove(tmpPath)
							return dlErr
						}
						if err := os.Chmod(tmpPath, 0o600); err != nil {
							_ = os.Remove(tmpPath)
							return err
						}
						if err := os.Rename(tmpPath, dst); err != nil {
							_ = os.Remove(tmpPath)
							return err
						}
						_ = os.Chmod(dst, 0o600)
						out = append(out, telegramDownloadedFile{
							Kind:               "photo",
							OriginalName:       orig,
							SizeBytes:          best.FileSize,
							Path:               dst,
							SourceMessageID:    sourceMessageID,
							SourceAttachmentID: sourceAttachmentID,
						})
					}
				}
			}
		}
		return nil
	}

	if err := handleMessage(msg); err != nil {
		return nil, err
	}
	if msg.ReplyTo != nil {
		if err := handleMessage(msg.ReplyTo); err != nil {
			return nil, err
		}
	}

	return out, nil
}
