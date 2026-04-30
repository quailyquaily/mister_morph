package line

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/channelruntime/imageinput"
	"github.com/quailyquaily/mistermorph/internal/telegramutil"
	"github.com/quailyquaily/mistermorph/llm"
)

const (
	lineLLMMaxImages     = 3
	lineLLMMaxImageBytes = int64(5 * 1024 * 1024)
)

func buildLineCurrentMessage(content string, model string, imagePaths []string, logger *slog.Logger) (llm.Message, error) {
	return imageinput.BuildUserMessage(content, model, imagePaths, imageinput.MessageOptions{
		MaxImages: lineLLMMaxImages,
		MaxBytes:  lineLLMMaxImageBytes,
		Logger:    logger,
		LogPrefix: "line",
	})
}

func downloadLineImageToCache(ctx context.Context, api *lineAPI, cacheDir string, messageID string, maxBytes int64) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if api == nil {
		return "", fmt.Errorf("line api is not initialized")
	}
	cacheDir = strings.TrimSpace(cacheDir)
	if cacheDir == "" {
		return "", fmt.Errorf("line image cache dir is required")
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return "", fmt.Errorf("line message id is required")
	}
	if maxBytes <= 0 {
		return "", fmt.Errorf("line image max bytes must be positive")
	}
	if err := telegramutil.EnsureSecureCacheDir(cacheDir); err != nil {
		return "", err
	}

	raw, mimeType, err := api.messageContent(ctx, messageID, maxBytes)
	if err != nil {
		return "", err
	}
	mimeType = imageinput.NormalizeMIMEType(mimeType)
	if !imageinput.SupportedUploadMIME(mimeType) {
		return "", fmt.Errorf("line image format is not supported: %s", mimeType)
	}
	ext := imageinput.ExtensionForMIMEType(mimeType)
	if ext == "" {
		return "", fmt.Errorf("line image extension is not supported: %s", mimeType)
	}

	pattern := "line_" + sanitizeLineFileToken(messageID) + "_*" + ext
	tmp, err := os.CreateTemp(cacheDir, pattern)
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	return tmpPath, nil
}

func ensureLineSecureChildDir(parentDir, childDir string) error {
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

func sanitizeLineFileToken(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "img"
	}
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "img"
	}
	return out
}
