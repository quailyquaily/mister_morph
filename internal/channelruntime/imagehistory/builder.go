package imagehistory

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/imagemime"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/telegramutil"
)

type Input struct {
	SourceMessageID    string
	SourceAttachmentID string
	LocalPath          string
	MIMEType           string
	Description        string
	DescriptionSource  string
}

func Build(inputs []Input, roots pathroots.PathRoots) []chathistory.ChatHistoryImage {
	if len(inputs) == 0 {
		return nil
	}
	roots = pathroots.New(roots.WorkspaceDir, roots.FileCacheDir, roots.FileStateDir)
	out := make([]chathistory.ChatHistoryImage, 0, len(inputs))
	for _, input := range inputs {
		localPath := strings.TrimSpace(input.LocalPath)
		if localPath == "" {
			continue
		}
		contentSHA256 := fileSHA256(localPath)
		if len(contentSHA256) < 16 {
			continue
		}
		width, height := imageDimensions(localPath)
		out = append(out, chathistory.ChatHistoryImage{
			ID:                 "img_" + contentSHA256[:16],
			Path:               AliasPath(localPath, roots),
			MIMEType:           imageMIMEType(localPath, input.MIMEType),
			Width:              width,
			Height:             height,
			Bytes:              fileSize(localPath),
			ContentSHA256:      contentSHA256,
			SourceMessageID:    strings.TrimSpace(input.SourceMessageID),
			SourceAttachmentID: strings.TrimSpace(input.SourceAttachmentID),
			Description:        strings.TrimSpace(input.Description),
			DescriptionSource:  strings.TrimSpace(input.DescriptionSource),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func BuildFromAttachments(attachments []busruntime.ImageAttachment, roots pathroots.PathRoots) []chathistory.ChatHistoryImage {
	inputs := make([]Input, 0, len(attachments))
	for _, attachment := range attachments {
		inputs = append(inputs, Input{
			SourceMessageID:    attachment.SourceMessageID,
			SourceAttachmentID: attachment.SourceAttachmentID,
			LocalPath:          attachment.Path,
			MIMEType:           attachment.MIMEType,
		})
	}
	return Build(inputs, roots)
}

func WithDescription(images []chathistory.ChatHistoryImage, description string, source string) []chathistory.ChatHistoryImage {
	if len(images) == 0 {
		return nil
	}
	out := append([]chathistory.ChatHistoryImage(nil), images...)
	description = strings.TrimSpace(description)
	source = strings.TrimSpace(source)
	if description == "" {
		return out
	}
	if source == "" {
		source = "agent_final"
	}
	for i := range out {
		out[i].Description = description
		out[i].DescriptionSource = source
	}
	return out
}

func DownloadDir(fileCacheDir string, workspaceDir string, channel string) (string, error) {
	channel = strings.Trim(strings.ToLower(strings.TrimSpace(channel)), `/\`)
	if channel == "" || strings.Contains(channel, "/") || strings.Contains(channel, `\`) {
		return "", fmt.Errorf("channel is invalid")
	}
	if workspaceDir = strings.TrimSpace(workspaceDir); workspaceDir != "" {
		base := pathutil.ExpandHomePath(workspaceDir)
		dir := filepath.Join(base, ".mistermorph", "images", channel)
		if err := ensureChildDir(base, dir); err != nil {
			return "", err
		}
		return dir, nil
	}
	fileCacheDir = strings.TrimSpace(fileCacheDir)
	if fileCacheDir == "" {
		return "", fmt.Errorf("file cache dir is required")
	}
	base := pathutil.ExpandHomePath(fileCacheDir)
	dir := filepath.Join(base, channel)
	if err := ensureChildDir(base, dir); err != nil {
		return "", err
	}
	return dir, nil
}

func ensureChildDir(parentDir string, childDir string) error {
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
	if !pathutil.IsWithinDir(parentAbs, childAbs) {
		return fmt.Errorf("child dir is not under parent dir: %s", childAbs)
	}
	return telegramutil.EnsureSecureCacheDir(childAbs)
}

func AliasPath(localPath string, roots pathroots.PathRoots) string {
	pathAbs, err := filepath.Abs(pathutil.ExpandHomePath(localPath))
	if err != nil {
		return ""
	}
	for _, candidate := range []struct {
		alias string
		base  string
	}{
		{alias: "workspace_dir", base: roots.WorkspaceDir},
		{alias: "file_cache_dir", base: roots.FileCacheDir},
	} {
		base := strings.TrimSpace(candidate.base)
		if base == "" {
			continue
		}
		baseAbs, err := filepath.Abs(pathutil.ExpandHomePath(base))
		if err != nil || !pathutil.IsWithinDir(baseAbs, pathAbs) {
			continue
		}
		rel, err := filepath.Rel(baseAbs, pathAbs)
		if err != nil || strings.TrimSpace(rel) == "" || strings.HasPrefix(rel, "..") {
			continue
		}
		return filepath.ToSlash(filepath.Join(candidate.alias, rel))
	}
	return ""
}

func imageMIMEType(path string, hint string) string {
	if mimeType := imagemime.Normalize(hint); strings.HasPrefix(mimeType, "image/") {
		return mimeType
	}
	if mimeType := detectImageMIMEType(path); mimeType != "" {
		return mimeType
	}
	return imagemime.FromPath(path)
}

func detectImageMIMEType(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	var header [512]byte
	n, err := f.Read(header[:])
	if err != nil && err != io.EOF {
		return ""
	}
	mimeType := imagemime.Normalize(http.DetectContentType(header[:n]))
	if strings.HasPrefix(mimeType, "image/") {
		return mimeType
	}
	return ""
}

func imageDimensions(path string) (int, int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return 0
	}
	return info.Size()
}

func fileSHA256(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return ""
	}
	return hex.EncodeToString(hash.Sum(nil))
}
