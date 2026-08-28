package mixinapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

func (c *Client) UploadAttachment(ctx context.Context, attachment Attachment, contentType string, size int64, source io.Reader) error {
	if c == nil {
		return fmt.Errorf("mixin api client is not initialized")
	}
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if source == nil {
		return fmt.Errorf("attachment source is required")
	}
	if size < 0 {
		return fmt.Errorf("attachment size must not be negative")
	}
	uploadURL, err := validateAttachmentURL(attachment.UploadURL)
	if err != nil {
		return fmt.Errorf("invalid attachment upload url: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, io.LimitReader(source, size))
	if err != nil {
		return err
	}
	req.ContentLength = size
	for name, value := range attachment.Headers {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("attachment upload header name is required")
		}
		req.Header.Set(name, strings.TrimSpace(value))
	}
	if contentType = strings.TrimSpace(contentType); contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload mixin attachment: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{HTTPStatus: resp.StatusCode, Description: "attachment upload failed"}
	}
	return nil
}

func (c *Client) DownloadAttachment(ctx context.Context, attachment Attachment, maxBytes int64) ([]byte, string, error) {
	if c == nil {
		return nil, "", fmt.Errorf("mixin api client is not initialized")
	}
	if ctx == nil {
		return nil, "", fmt.Errorf("context is required")
	}
	if maxBytes <= 0 {
		return nil, "", fmt.Errorf("attachment max bytes must be positive")
	}
	viewURL, err := validateAttachmentURL(attachment.ViewURL)
	if err != nil {
		return nil, "", fmt.Errorf("invalid attachment view url: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, viewURL, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("download mixin attachment: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil, "", &APIError{HTTPStatus: resp.StatusCode, Description: "attachment download failed"}
	}
	if resp.ContentLength > maxBytes {
		return nil, "", fmt.Errorf("%w: attachment content length %d exceeds %d", ErrRequestTooLarge, resp.ContentLength, maxBytes)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read mixin attachment: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, "", fmt.Errorf("%w: attachment exceeds %d bytes", ErrRequestTooLarge, maxBytes)
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if mediaType, _, parseErr := mime.ParseMediaType(contentType); parseErr == nil {
		contentType = mediaType
	}
	return body, contentType, nil
}

func validateAttachmentURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("url must use http or https")
	}
	if parsed.User != nil {
		return "", errors.New("url must not contain user info")
	}
	return parsed.String(), nil
}
