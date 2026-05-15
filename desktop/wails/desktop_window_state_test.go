//go:build wailsdesktop

package main

import (
	"path/filepath"
	"testing"
)

func TestDesktopMainWindowStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "main-window.json")
	want := desktopMainWindowState{
		X:      -20,
		Y:      40,
		Width:  1600,
		Height: 980,
	}

	if err := saveDesktopMainWindowState(path, want); err != nil {
		t.Fatalf("saveDesktopMainWindowState() error = %v", err)
	}

	got, ok := loadDesktopMainWindowState(path)
	if !ok {
		t.Fatal("loadDesktopMainWindowState() ok = false, want true")
	}
	if got != want {
		t.Fatalf("loadDesktopMainWindowState() = %+v, want %+v", got, want)
	}
}

func TestNormalizeDesktopMainWindowStateRejectsMissingSize(t *testing.T) {
	_, ok := normalizeDesktopMainWindowState(desktopMainWindowState{
		X:      20,
		Y:      20,
		Width:  0,
		Height: 860,
	})
	if ok {
		t.Fatal("normalizeDesktopMainWindowState() ok = true, want false")
	}
}

func TestNormalizeDesktopMainWindowStateClampsSize(t *testing.T) {
	got, ok := normalizeDesktopMainWindowState(desktopMainWindowState{
		X:      20,
		Y:      20,
		Width:  10,
		Height: 9999,
	})
	if !ok {
		t.Fatal("normalizeDesktopMainWindowState() ok = false, want true")
	}
	if got.Width != defaultDesktopMainWindowMinWidth {
		t.Fatalf("width = %d, want %d", got.Width, defaultDesktopMainWindowMinWidth)
	}
	if got.Height != maxDesktopMainWindowHeight {
		t.Fatalf("height = %d, want %d", got.Height, maxDesktopMainWindowHeight)
	}
}
