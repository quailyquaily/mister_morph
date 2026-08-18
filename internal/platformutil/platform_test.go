package platformutil

import (
	"runtime"
	"testing"
)

func TestCurrentPlatform(t *testing.T) {
	if got := Current(); got != runtime.GOOS {
		t.Fatalf("Current() = %q, want %q", got, runtime.GOOS)
	}
	if got := IsWindows(); got != (runtime.GOOS == Windows) {
		t.Fatalf("IsWindows() = %t, want %t", got, runtime.GOOS == Windows)
	}
}
