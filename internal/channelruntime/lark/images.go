package lark

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/channelruntime/imageinput"
	"github.com/quailyquaily/mistermorph/internal/telegramutil"
)

const (
	larkLLMMaxImages     = 3
	larkLLMMaxImageBytes = int64(5 * 1024 * 1024)
)

const larkImageRecognitionDisabledPrompt = "User sent an image, but image recognition is disabled in the current Lark runtime. Reply briefly and ask the user to describe the image in text or enable lark in multimodal.image.sources."

func downloadLarkImageToCache(ctx context.Context, api *larkAPI, cacheDir string, messageID string, imageKey string, maxBytes int64) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if api == nil {
		return "", fmt.Errorf("lark api is not initialized")
	}
	cacheDir = strings.TrimSpace(cacheDir)
	if cacheDir == "" {
		return "", fmt.Errorf("lark image cache dir is required")
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return "", fmt.Errorf("lark message id is required")
	}
	imageKey = strings.TrimSpace(imageKey)
	if imageKey == "" {
		return "", fmt.Errorf("lark image key is required")
	}
	if maxBytes <= 0 {
		return "", fmt.Errorf("lark image max bytes must be positive")
	}
	if err := telegramutil.EnsureSecureCacheDir(cacheDir); err != nil {
		return "", err
	}

	raw, mimeType, err := api.messageResource(ctx, messageID, imageKey, "image", maxBytes)
	if err != nil {
		return "", err
	}
	mimeType = imageinput.NormalizeMIMEType(mimeType)
	if !imageinput.SupportedUploadMIME(mimeType) {
		detected := imageinput.NormalizeMIMEType(http.DetectContentType(raw))
		if imageinput.SupportedUploadMIME(detected) {
			mimeType = detected
		}
	}
	if !imageinput.SupportedUploadMIME(mimeType) {
		return "", fmt.Errorf("lark image format is not supported: %s", mimeType)
	}
	ext := imageinput.ExtensionForMIMEType(mimeType)
	if ext == "" {
		return "", fmt.Errorf("lark image extension is not supported: %s", mimeType)
	}

	pattern := "lark_" + sanitizeLarkFileToken(imageKey) + "_*" + ext
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

func larkImageCacheDir(fileCacheDir string) string {
	fileCacheDir = strings.TrimSpace(fileCacheDir)
	if fileCacheDir == "" {
		return ""
	}
	return filepath.Join(fileCacheDir, "lark")
}

func larkImageFallbackText(text string, imageRecognitionEnabled bool, imageCount int) string {
	text = strings.TrimSpace(text)
	if imageCount <= 0 || imageRecognitionEnabled {
		if text != "" {
			return text
		}
		return "User sent an image."
	}
	if text == "" || text == "User sent an image." {
		return larkImageRecognitionDisabledPrompt
	}
	return text + "\n\n" + larkImageRecognitionDisabledPrompt
}

func appendLarkImageReadFailure(text string) string {
	text = strings.TrimSpace(text)
	note := "Image attachment could not be read."
	if text == "" || text == "User sent an image." {
		return note
	}
	return text + "\n\n" + note
}

func sanitizeLarkFileToken(raw string) string {
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

func larkImageDownloadContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, 4*time.Second)
}
