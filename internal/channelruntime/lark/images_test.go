package lark

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	larkbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/lark"
)

func TestDownloadLarkImageToCache(t *testing.T) {
	t.Parallel()

	raw := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/v3/tenant_access_token/internal":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"tenant-token","expire":7200}`))
		case "/im/v1/messages/om_1001/resources/img_123":
			if r.URL.Query().Get("type") != "image" {
				t.Fatalf("type query = %q, want image", r.URL.Query().Get("type"))
			}
			if r.Header.Get("Authorization") != "Bearer tenant-token" {
				t.Fatalf("authorization = %q, want tenant token", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(raw)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	tokenClient := NewTenantTokenClient(srv.Client(), srv.URL, "app_id", "app_secret")
	api := newLarkAPI(srv.Client(), srv.URL, tokenClient)
	path, err := downloadLarkImageToCache(context.Background(), api, t.TempDir(), "om_1001", "img_123", 1024*1024)
	if err != nil {
		t.Fatalf("downloadLarkImageToCache() error = %v", err)
	}
	if filepath.Ext(path) != ".png" {
		t.Fatalf("extension = %q, want .png", filepath.Ext(path))
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != string(raw) {
		t.Fatalf("downloaded content mismatch")
	}
}

func TestDownloadLarkInboundImages(t *testing.T) {
	t.Parallel()

	raw := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/v3/tenant_access_token/internal":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"tenant-token","expire":7200}`))
		case "/im/v1/messages/om_1001/resources/img_123":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(raw)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	tokenClient := NewTenantTokenClient(srv.Client(), srv.URL, "app_id", "app_secret")
	api := newLarkAPI(srv.Client(), srv.URL, tokenClient)
	got := downloadLarkInboundImages(context.Background(), api, t.TempDir(), larkbus.InboundMessage{
		ChatID:    "oc_group123",
		MessageID: "om_1001",
		Text:      "User sent an image.",
		ImageKeys: []string{"img_123"},
	}, nil)
	if len(got.ImagePaths) != 1 {
		t.Fatalf("image_paths len = %d, want 1", len(got.ImagePaths))
	}
	if filepath.Ext(got.ImagePaths[0]) != ".png" {
		t.Fatalf("extension = %q, want .png", filepath.Ext(got.ImagePaths[0]))
	}
	if got.Text != "User sent an image." {
		t.Fatalf("text = %q, want original text", got.Text)
	}
}

func TestDownloadLarkImageToCacheRejectsUnknownType(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/v3/tenant_access_token/internal":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"tenant-token","expire":7200}`))
		case "/im/v1/messages/om_1001/resources/img_123":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("unknown"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	tokenClient := NewTenantTokenClient(srv.Client(), srv.URL, "app_id", "app_secret")
	api := newLarkAPI(srv.Client(), srv.URL, tokenClient)
	_, err := downloadLarkImageToCache(context.Background(), api, t.TempDir(), "om_1001", "img_123", 1024*1024)
	if err == nil {
		t.Fatalf("downloadLarkImageToCache() expected error")
	}
}
