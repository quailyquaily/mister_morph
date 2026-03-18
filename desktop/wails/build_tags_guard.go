//go:build wailsdesktop && !dev && !production && !bindings

package main

// Intentionally undefined: building the desktop app without one of the
// Wails app tags should fail at compile time instead of producing a binary
// that only errors at runtime.
var _ BuildDesktopWithTagsWailsdesktopAndProductionOrDev
