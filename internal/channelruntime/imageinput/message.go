package imageinput

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/llm"
)

type TranscodeFunc func(raw []byte, mimeType string) ([]byte, string, error)

type MessageOptions struct {
	MaxImages int
	MaxBytes  int64
	Logger    *slog.Logger
	LogPrefix string
	Transcode TranscodeFunc
}

func BuildUserMessage(content string, model string, imagePaths []string, opts MessageOptions) (llm.Message, error) {
	msg := llm.Message{Role: "user", Content: content}
	if !llm.ModelSupportsImageParts(model) || len(imagePaths) == 0 || opts.MaxImages <= 0 || opts.MaxBytes <= 0 {
		return msg, nil
	}

	parts := make([]llm.Part, 0, 1+minInt(len(imagePaths), opts.MaxImages))
	if strings.TrimSpace(content) != "" {
		parts = append(parts, llm.Part{Type: llm.PartTypeText, Text: content})
	}

	seen := make(map[string]bool, len(imagePaths))
	imageCount := 0
	for _, rawPath := range imagePaths {
		if imageCount >= opts.MaxImages {
			break
		}
		path := strings.TrimSpace(rawPath)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true

		info, err := os.Stat(path)
		if err != nil {
			logWarn(opts, "image_part_skip", "path", path, "error", err.Error())
			continue
		}
		if info.Size() <= 0 {
			continue
		}
		if info.Size() > opts.MaxBytes {
			return llm.Message{}, fmt.Errorf("图片太大: %s (%d bytes > %d bytes)", filepath.Base(path), info.Size(), opts.MaxBytes)
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			logWarn(opts, "image_part_read_error", "path", path, "error", err.Error())
			continue
		}
		mimeType := MIMETypeFromPath(path)
		if !SupportedUploadMIME(mimeType) {
			logWarn(opts, "image_part_skip_unsupported_format", "path", path, "mime_type", mimeType)
			continue
		}
		if opts.Transcode != nil {
			transcodedRaw, transcodedMIME, transcodeErr := opts.Transcode(raw, mimeType)
			if transcodeErr != nil {
				return llm.Message{}, fmt.Errorf("图片转换失败: %s: %w", filepath.Base(path), transcodeErr)
			}
			raw = transcodedRaw
			mimeType = strings.TrimSpace(strings.ToLower(transcodedMIME))
			if !SupportedUploadMIME(mimeType) {
				return llm.Message{}, fmt.Errorf("图片转换后格式不支持: %s (%s)", filepath.Base(path), mimeType)
			}
		}

		parts = append(parts, llm.Part{
			Type:       llm.PartTypeImageBase64,
			MIMEType:   mimeType,
			DataBase64: base64.StdEncoding.EncodeToString(raw),
		})
		imageCount++
	}
	if imageCount == 0 {
		return msg, nil
	}
	msg.Parts = parts
	return msg, nil
}

func MIMETypeFromPath(path string) string {
	switch strings.ToLower(strings.TrimSpace(filepath.Ext(path))) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".bmp":
		return "image/bmp"
	case ".heic":
		return "image/heic"
	case ".heif":
		return "image/heif"
	default:
		return ""
	}
}

func NormalizeMIMEType(mimeType string) string {
	mimeType = strings.TrimSpace(strings.ToLower(mimeType))
	if idx := strings.Index(mimeType, ";"); idx >= 0 {
		mimeType = strings.TrimSpace(mimeType[:idx])
	}
	return mimeType
}

func SupportedUploadMIME(mimeType string) bool {
	switch NormalizeMIMEType(mimeType) {
	case "image/jpeg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

func ExtensionForMIMEType(mimeType string) string {
	switch NormalizeMIMEType(mimeType) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}

func logWarn(opts MessageOptions, suffix string, args ...any) {
	if opts.Logger == nil {
		return
	}
	prefix := strings.TrimSpace(opts.LogPrefix)
	if prefix == "" {
		prefix = "image"
	}
	opts.Logger.Warn(prefix+"_"+suffix, args...)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
