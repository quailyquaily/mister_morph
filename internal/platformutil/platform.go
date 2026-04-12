// Package platformutil provides platform detection utilities.
package platformutil

import "runtime"

// Platform constants
const (
	Windows = "windows"
	Linux   = "linux"
	Darwin  = "darwin"
)

// IsWindows returns true if the current platform is Windows.
func IsWindows() bool {
	return runtime.GOOS == Windows
}

// IsLinux returns true if the current platform is Linux.
func IsLinux() bool {
	return runtime.GOOS == Linux
}

// IsDarwin returns true if the current platform is macOS.
func IsDarwin() bool {
	return runtime.GOOS == Darwin
}

// IsUnixLike returns true if the current platform is Unix-like (Linux or macOS).
func IsUnixLike() bool {
	return IsLinux() || IsDarwin()
}

// Current returns the current platform name.
func Current() string {
	return runtime.GOOS
}

// ShellToolName returns the appropriate shell tool name for the current platform.
// Returns "powershell" for Windows, "bash" for Unix-like systems.
func ShellToolName() string {
	if IsWindows() {
		return "powershell"
	}
	return "bash"
}

// ShellToolDescription returns a description of the available shell tool for the current platform.
func ShellToolDescription() string {
	if IsWindows() {
		return "PowerShell is available for command execution on Windows."
	}
	return "Bash is available for command execution on Unix-like systems."
}
