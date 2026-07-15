package agent

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/llm"
)

const maxCompactionImageBytes int64 = 20 * 1024 * 1024

type contextImageReference struct {
	Path     string `json:"path"`
	MIMEType string `json:"mime_type"`
	Bytes    int64  `json:"bytes"`
	SHA256   string `json:"sha256"`
}

type preparedCompactionImages struct {
	Messages               []llm.Message
	ImagePartsByMessage    map[int][]llm.Part
	ReferencesByMessage    map[int][]contextImageReference
	PreparedMessageIndexes map[int]struct{}
	Failures               map[int]error
}

func prepareCompactionImages(ctx context.Context, messages []llm.Message, roots pathroots.PathRoots) (preparedCompactionImages, error) {
	prepared := preparedCompactionImages{
		Messages:               cloneMessagesForCompaction(messages),
		ImagePartsByMessage:    make(map[int][]llm.Part),
		ReferencesByMessage:    make(map[int][]contextImageReference),
		PreparedMessageIndexes: make(map[int]struct{}),
		Failures:               make(map[int]error),
	}
	if ctx == nil {
		ctx = context.Background()
	}

	for messageIndex := range messages {
		message := messages[messageIndex]
		if !messageHasImagePart(message) {
			continue
		}

		parts := append([]llm.Part(nil), prepared.Messages[messageIndex].Parts...)
		messagePrepared := true
		for partIndex, part := range message.Parts {
			if !isImagePart(part) {
				continue
			}
			raw, mimeType, imagePart, err := readCompactionImagePart(part)
			if err != nil {
				prepared.Failures[messageIndex] = err
				messagePrepared = false
				continue
			}
			ref, err := persistCompactionImage(raw, mimeType, roots)
			if err != nil {
				prepared.Failures[messageIndex] = err
				messagePrepared = false
				continue
			}
			parts[partIndex] = llm.Part{
				Type: llm.PartTypeText,
				Text: formatCompactionImageReference(ref),
			}
			prepared.ReferencesByMessage[messageIndex] = append(prepared.ReferencesByMessage[messageIndex], ref)
			prepared.ImagePartsByMessage[messageIndex] = append(prepared.ImagePartsByMessage[messageIndex], imagePart)
		}
		if messagePrepared {
			prepared.Messages[messageIndex].Parts = parts
			prepared.PreparedMessageIndexes[messageIndex] = struct{}{}
		}
	}
	if err := ctx.Err(); err != nil {
		return prepared, err
	}
	return prepared, nil
}

func cloneMessagesForCompaction(messages []llm.Message) []llm.Message {
	out := append([]llm.Message(nil), messages...)
	for i := range out {
		out[i].Parts = append([]llm.Part(nil), messages[i].Parts...)
		out[i].ToolCalls = append([]llm.ToolCall(nil), messages[i].ToolCalls...)
	}
	return out
}

func isImagePart(part llm.Part) bool {
	switch strings.ToLower(strings.TrimSpace(part.Type)) {
	case llm.PartTypeImageBase64, llm.PartTypeImageURL:
		return true
	default:
		return false
	}
}

func readCompactionImagePart(part llm.Part) ([]byte, string, llm.Part, error) {
	switch strings.ToLower(strings.TrimSpace(part.Type)) {
	case llm.PartTypeImageBase64:
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(part.DataBase64))
		if err != nil {
			return nil, "", llm.Part{}, fmt.Errorf("decode context image: %w", err)
		}
		if err := validateCompactionImageSize(raw); err != nil {
			return nil, "", llm.Part{}, err
		}
		mimeType := normalizeImageMIMEType(part.MIMEType, raw)
		if !strings.HasPrefix(mimeType, "image/") {
			return nil, "", llm.Part{}, fmt.Errorf("context image MIME type is invalid: %q", mimeType)
		}
		return raw, mimeType, llm.Part{Type: llm.PartTypeImageBase64, MIMEType: mimeType, DataBase64: base64.StdEncoding.EncodeToString(raw)}, nil

	case llm.PartTypeImageURL:
		return readCompactionImageURL(strings.TrimSpace(part.URL), part.MIMEType)
	default:
		return nil, "", llm.Part{}, fmt.Errorf("unsupported context image part type %q", part.Type)
	}
}

func readCompactionImageURL(rawURL string, mimeHint string) ([]byte, string, llm.Part, error) {
	if strings.HasPrefix(strings.ToLower(rawURL), "data:") {
		raw, mimeType, err := decodeImageDataURL(rawURL)
		if err != nil {
			return nil, "", llm.Part{}, err
		}
		if err := validateCompactionImageSize(raw); err != nil {
			return nil, "", llm.Part{}, err
		}
		mimeType = normalizeImageMIMEType(firstNonEmptyString(mimeHint, mimeType), raw)
		if !strings.HasPrefix(mimeType, "image/") {
			return nil, "", llm.Part{}, fmt.Errorf("context image MIME type is invalid: %q", mimeType)
		}
		return raw, mimeType, llm.Part{Type: llm.PartTypeImageBase64, MIMEType: mimeType, DataBase64: base64.StdEncoding.EncodeToString(raw)}, nil
	}

	if rawURL == "" {
		return nil, "", llm.Part{}, fmt.Errorf("context image URL is invalid")
	}
	return nil, "", llm.Part{}, fmt.Errorf("remote context image URL must be materialized before compaction")
}

func decodeImageDataURL(value string) ([]byte, string, error) {
	comma := strings.IndexByte(value, ',')
	if comma <= len("data:") {
		return nil, "", fmt.Errorf("context image data URL is invalid")
	}
	header := value[len("data:"):comma]
	if !strings.HasSuffix(strings.ToLower(header), ";base64") {
		return nil, "", fmt.Errorf("context image data URL must use base64")
	}
	mimeType := header[:len(header)-len(";base64")]
	raw, err := base64.StdEncoding.DecodeString(value[comma+1:])
	if err != nil {
		return nil, "", fmt.Errorf("decode context image data URL: %w", err)
	}
	return raw, mimeType, nil
}

func validateCompactionImageSize(raw []byte) error {
	if len(raw) == 0 {
		return fmt.Errorf("context image is empty")
	}
	if int64(len(raw)) > maxCompactionImageBytes {
		return fmt.Errorf("context image is too large: %d bytes", len(raw))
	}
	return nil
}

func normalizeImageMIMEType(value string, raw []byte) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if mediaType, _, err := mime.ParseMediaType(value); err == nil {
		value = strings.ToLower(strings.TrimSpace(mediaType))
	}
	if strings.HasPrefix(value, "image/") {
		return value
	}
	detected := strings.ToLower(strings.TrimSpace(http.DetectContentType(raw)))
	if mediaType, _, err := mime.ParseMediaType(detected); err == nil {
		detected = strings.ToLower(strings.TrimSpace(mediaType))
	}
	return detected
}

func persistCompactionImage(raw []byte, mimeType string, roots pathroots.PathRoots) (contextImageReference, error) {
	roots = pathroots.New(roots.WorkspaceDir, roots.FileCacheDir, roots.FileStateDir)
	baseDir := ""
	alias := ""
	relDir := ""
	if strings.TrimSpace(roots.WorkspaceDir) != "" {
		baseDir = roots.WorkspaceDir
		alias = "workspace_dir"
		relDir = filepath.Join(".mistermorph", "context-images")
	} else if strings.TrimSpace(roots.FileCacheDir) != "" {
		baseDir = roots.FileCacheDir
		alias = "file_cache_dir"
		relDir = "context-images"
	} else {
		return contextImageReference{}, fmt.Errorf("context image storage requires workspace_dir or file_cache_dir")
	}

	hash := sha256.Sum256(raw)
	hashText := hex.EncodeToString(hash[:])
	filename := hashText + extensionForImageMIME(mimeType)
	dir := filepath.Join(baseDir, relDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return contextImageReference{}, fmt.Errorf("create context image directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return contextImageReference{}, fmt.Errorf("secure context image directory: %w", err)
	}
	path := filepath.Join(dir, filename)
	if err := writeFileAtomicIfMissing(path, raw, 0o600); err != nil {
		return contextImageReference{}, fmt.Errorf("persist context image: %w", err)
	}
	return contextImageReference{
		Path:     filepath.ToSlash(filepath.Join(alias, relDir, filename)),
		MIMEType: mimeType,
		Bytes:    int64(len(raw)),
		SHA256:   hashText,
	}, nil
}

func writeFileAtomicIfMissing(path string, raw []byte, mode os.FileMode) error {
	if info, err := os.Stat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("target is not a regular file")
		}
		return os.Chmod(path, mode)
	} else if !os.IsNotExist(err) {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".context-image-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	keep := false
	defer func() {
		_ = tmp.Close()
		if !keep {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	keep = true
	return nil
}

func extensionForImageMIME(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/bmp":
		return ".bmp"
	case "image/heic":
		return ".heic"
	case "image/heif":
		return ".heif"
	default:
		return ".img"
	}
}

func formatCompactionImageReference(ref contextImageReference) string {
	return fmt.Sprintf("[[ Context Image Reference ]]\npath: %s\nmime_type: %s\nbytes: %d\nsha256: %s", ref.Path, ref.MIMEType, ref.Bytes, ref.SHA256)
}
