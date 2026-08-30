package contacts

import (
	"context"
	"encoding/base64"
	"os"
	"testing"
	"time"
)

const testContactAvatarPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

func testContactAvatarPNG(t *testing.T) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(testContactAvatarPNGBase64)
	if err != nil {
		t.Fatalf("decode test png: %v", err)
	}
	return raw
}

func TestFileStoreContactAvatarLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewFileStore(t.TempDir())
	contactID := "tg:../../1234"
	raw := testContactAvatarPNG(t)

	if err := store.PutContactAvatar(ctx, contactID, raw); err != nil {
		t.Fatalf("PutContactAvatar() error = %v", err)
	}

	avatar, ok, err := store.ReadContactAvatar(ctx, contactID)
	if err != nil {
		t.Fatalf("ReadContactAvatar() error = %v", err)
	}
	if !ok {
		t.Fatal("ReadContactAvatar() found = false, want true")
	}
	if avatar.ContentType != "image/png" {
		t.Fatalf("content type = %q, want image/png", avatar.ContentType)
	}
	if string(avatar.Data) != string(raw) {
		t.Fatal("avatar data mismatch")
	}
	if avatar.ModTime.IsZero() {
		t.Fatal("avatar mod time is zero")
	}

	revisions, err := store.ContactAvatarRevisions(ctx, []string{contactID, "tg:missing"})
	if err != nil {
		t.Fatalf("ContactAvatarRevisions() error = %v", err)
	}
	if _, ok := revisions[contactID]; !ok {
		t.Fatalf("revisions missing %q: %#v", contactID, revisions)
	}
	if _, ok := revisions["tg:missing"]; ok {
		t.Fatalf("revisions unexpectedly contains missing contact: %#v", revisions)
	}

	old := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(store.contactAvatarPath(contactID), old, old); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}
	fresh, err := store.ContactAvatarFresh(ctx, contactID, old.Add(7*24*time.Hour), 7*24*time.Hour)
	if err != nil {
		t.Fatalf("ContactAvatarFresh() boundary error = %v", err)
	}
	if !fresh {
		t.Fatal("avatar at refresh boundary should be fresh")
	}
	fresh, err = store.ContactAvatarFresh(ctx, contactID, old.Add(7*24*time.Hour+time.Nanosecond), 7*24*time.Hour)
	if err != nil {
		t.Fatalf("ContactAvatarFresh() stale error = %v", err)
	}
	if fresh {
		t.Fatal("avatar past refresh boundary should be stale")
	}

	if err := store.DeleteContactAvatar(ctx, contactID); err != nil {
		t.Fatalf("DeleteContactAvatar() error = %v", err)
	}
	if _, ok, err := store.ReadContactAvatar(ctx, contactID); err != nil || ok {
		t.Fatalf("avatar after delete = (found=%v, err=%v), want false, nil", ok, err)
	}
	if err := store.DeleteContactAvatar(ctx, contactID); err != nil {
		t.Fatalf("DeleteContactAvatar() missing error = %v", err)
	}
}

func TestFileStoreContactAvatarValidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewFileStore(t.TempDir())

	tests := []struct {
		name      string
		contactID string
		data      []byte
	}{
		{name: "empty contact id", data: testContactAvatarPNG(t)},
		{name: "svg", contactID: "tg:1", data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)},
		{name: "plain text", contactID: "tg:1", data: []byte("not an image")},
		{name: "too large", contactID: "tg:1", data: make([]byte, ContactAvatarMaxBytes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := store.PutContactAvatar(ctx, test.contactID, test.data); err == nil {
				t.Fatal("PutContactAvatar() error = nil, want validation error")
			}
		})
	}
}

func TestFileStoreContactAvatarHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := NewFileStore(t.TempDir())
	if err := store.PutContactAvatar(ctx, "tg:1", testContactAvatarPNG(t)); err == nil {
		t.Fatal("PutContactAvatar() error = nil, want context error")
	}
}
