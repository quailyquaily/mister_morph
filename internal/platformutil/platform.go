// Package platformutil provides platform detection utilities.
package platformutil

import "runtime"

const Windows = "windows"

// IsWindows returns true if the current platform is Windows.
func IsWindows() bool {
	return runtime.GOOS == Windows
}

// Current returns the current platform name.
func Current() string {
	return runtime.GOOS
}
