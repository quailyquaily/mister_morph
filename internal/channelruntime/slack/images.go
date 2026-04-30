package slack

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/channelruntime/imageinput"
	"github.com/quailyquaily/mistermorph/internal/telegramutil"
)

const (
	slackLLMMaxImages     = 3
	slackLLMMaxImageBytes = int64(5 * 1024 * 1024)
)

const slackImageRecognitionDisabledPrompt = "User sent an image, but image recognition is disabled in the current Slack runtime. Reply briefly and ask the user to describe the image in text or enable slack in multimodal.image.sources."

func downloadSlackImageToCache(ctx context.Context, api *slackAPI, cacheDir string, file slackEventFile, maxBytes int64) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if api == nil || api.http == nil {
		return "", fmt.Errorf("slack api is not initialized")
	}
	cacheDir = strings.TrimSpace(cacheDir)
	if cacheDir == "" {
		return "", fmt.Errorf("slack image cache dir is required")
	}
	if maxBytes <= 0 {
		return "", fmt.Errorf("slack image max bytes must be positive")
	}
	if slackFileNeedsInfo(file) {
		resolved, err := api.fileInfo(ctx, file.ID)
		if err != nil {
			return "", err
		}
		file = resolved
	}
	if file.Size > maxBytes {
		return "", fmt.Errorf("slack image too large: %d bytes > %d bytes", file.Size, maxBytes)
	}
	mimeType := imageinput.NormalizeMIMEType(slackFileMIMEType(file))
	if !imageinput.SupportedUploadMIME(mimeType) {
		return "", fmt.Errorf("slack image format is not supported: %s", mimeType)
	}
	ext := imageinput.ExtensionForMIMEType(mimeType)
	if ext == "" {
		return "", fmt.Errorf("slack image extension is not supported: %s", mimeType)
	}
	downloadURL := slackFileDownloadURL(file)
	if downloadURL == "" {
		return "", fmt.Errorf("slack image download url is required")
	}
	if err := telegramutil.EnsureSecureCacheDir(cacheDir); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(api.botToken))
	resp, err := api.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			return "", fmt.Errorf("slack image download http %d", resp.StatusCode)
		}
		return "", fmt.Errorf("slack image download http %d: %s", resp.StatusCode, msg)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(raw)) > maxBytes {
		return "", fmt.Errorf("slack image too large: > %d bytes", maxBytes)
	}

	token := strings.TrimSpace(file.ID)
	if token == "" {
		token = strings.TrimSpace(file.Name)
	}
	pattern := "slack_" + sanitizeSlackFileToken(token) + "_*" + ext
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

func slackImageCacheDir(fileCacheDir string) string {
	fileCacheDir = strings.TrimSpace(fileCacheDir)
	if fileCacheDir == "" {
		return ""
	}
	return filepath.Join(fileCacheDir, "slack")
}

func slackImageFallbackText(text string, imageRecognitionEnabled bool, imageCount int) string {
	text = strings.TrimSpace(text)
	if imageCount <= 0 || imageRecognitionEnabled {
		if text != "" {
			return text
		}
		return "User sent an image."
	}
	if text == "" {
		return slackImageRecognitionDisabledPrompt
	}
	return text + "\n\n" + slackImageRecognitionDisabledPrompt
}

func appendSlackImageReadFailure(text string) string {
	text = strings.TrimSpace(text)
	note := "Image attachment could not be read."
	if text == "" || text == "User sent an image." {
		return note
	}
	return text + "\n\n" + note
}

func sanitizeSlackFileToken(raw string) string {
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

func slackImageDownloadContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, 20*time.Second)
}
