package mixinapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUploadAttachmentUsesSignedStorageHeadersWithoutMixinAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s", r.Method)
		}
		if r.Header.Get("Authorization") != "storage-signature" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Error("mixin jwt leaked to attachment storage")
		}
		if r.ContentLength != 5 || r.Header.Get("Content-Type") != "text/plain" {
			t.Errorf("content length/type = %d %q", r.ContentLength, r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "hello" {
			t.Errorf("body = %q", body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client, err := NewClient(testCredentials(), ClientOptions{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	err = client.UploadAttachment(context.Background(), Attachment{
		UploadURL: server.URL,
		Headers:   map[string]string{"Authorization": "storage-signature"},
	}, "text/plain", 5, strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("UploadAttachment() error = %v", err)
	}
}

func TestDownloadAttachmentEnforcesDeclaredAndActualSize(t *testing.T) {
	tests := []struct {
		name          string
		contentLength string
		body          string
	}{
		{name: "declared too large", contentLength: "6", body: "123456"},
		{name: "stream too large", body: "123456"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.contentLength != "" {
					w.Header().Set("Content-Length", tt.contentLength)
				}
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()
			client, err := NewClient(testCredentials(), ClientOptions{HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := client.DownloadAttachment(context.Background(), Attachment{ViewURL: server.URL}, 5); !errors.Is(err, ErrRequestTooLarge) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestDownloadAttachmentReturnsBodyAndContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, "png")
	}))
	defer server.Close()
	client, err := NewClient(testCredentials(), ClientOptions{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	body, contentType, err := client.DownloadAttachment(context.Background(), Attachment{ViewURL: server.URL}, 10)
	if err != nil {
		t.Fatalf("DownloadAttachment() error = %v", err)
	}
	if string(body) != "png" || contentType != "image/png" {
		t.Fatalf("body/type = %q %q", body, contentType)
	}
}
