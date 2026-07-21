package imagemime

import "testing"

func TestImageMIMEHelpers(t *testing.T) {
	if got := Normalize(" Image/PNG; charset=binary "); got != "image/png" {
		t.Fatalf("Normalize() = %q, want image/png", got)
	}
	if got := FromPath("photo.JPEG"); got != "image/jpeg" {
		t.Fatalf("FromPath() = %q, want image/jpeg", got)
	}
	if !SupportedUpload("image/webp; charset=binary") {
		t.Fatal("SupportedUpload(image/webp) = false, want true")
	}
	if SupportedUpload("image/gif") {
		t.Fatal("SupportedUpload(image/gif) = true, want false")
	}
	if got := Extension("IMAGE/JPEG"); got != ".jpg" {
		t.Fatalf("Extension() = %q, want .jpg", got)
	}
}
