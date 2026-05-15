//go:build wailsdesktop

package main

import (
	"path/filepath"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func TestBuildDesktopWindowOptions_UsesConsoleURL(t *testing.T) {
	consoleURL := "http://127.0.0.1:19080/console/"
	opts := buildDesktopWindowOptions(consoleURL, nil)
	if opts.URL != consoleURL {
		t.Fatalf("buildDesktopWindowOptions() URL = %q, want %q", opts.URL, consoleURL)
	}
	if opts.Title != "MisterMorph" {
		t.Fatalf("buildDesktopWindowOptions() title = %q, want MisterMorph", opts.Title)
	}
	if opts.JS != desktopRuntimeJavaScript {
		t.Fatalf("buildDesktopWindowOptions() JS = %q, want desktop runtime marker", opts.JS)
	}
	if !opts.UseApplicationMenu {
		t.Fatal("buildDesktopWindowOptions() UseApplicationMenu = false, want true")
	}
}

func TestBuildDesktopWindowOptions_UsesSavedWindowState(t *testing.T) {
	state := desktopMainWindowState{
		X:      120,
		Y:      80,
		Width:  1440,
		Height: 920,
	}
	opts := buildDesktopWindowOptions("http://127.0.0.1:19080/console/", &state)
	if opts.Width != state.Width {
		t.Fatalf("width = %d, want %d", opts.Width, state.Width)
	}
	if opts.Height != state.Height {
		t.Fatalf("height = %d, want %d", opts.Height, state.Height)
	}
	if opts.InitialPosition != application.WindowXY {
		t.Fatalf("initial position = %v, want WindowXY", opts.InitialPosition)
	}
	if opts.X != state.X || opts.Y != state.Y {
		t.Fatalf("position = (%d,%d), want (%d,%d)", opts.X, opts.Y, state.X, state.Y)
	}
}

func TestConfigureDesktopMainWindowLifecycle_HidesAndCancelsCloseOnMac(t *testing.T) {
	window := &fakeDesktopLifecycleWindow{}

	configureDesktopMainWindowLifecycleForGOOS(window, "darwin")

	callback := window.callbacks[events.Common.WindowClosing]
	if callback == nil {
		t.Fatal("registered callback = nil, want callback")
	}

	event := application.NewWindowEvent()
	callback(event)

	if !window.hidden {
		t.Fatal("window hidden = false, want true")
	}
	if !event.IsCancelled() {
		t.Fatal("close event cancelled = false, want true")
	}
}

func TestConfigureDesktopMainWindowLifecycle_DoesNotOverrideCloseOffMac(t *testing.T) {
	window := &fakeDesktopLifecycleWindow{}

	configureDesktopMainWindowLifecycleForGOOS(window, "linux")

	if len(window.callbacks) != 0 {
		t.Fatal("registered callback != nil, want no hook outside macOS")
	}
}

func TestConfigureDesktopMainWindowStatePersistence_SavesOnResize(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "main-window.json")
	window := &fakeDesktopLifecycleWindow{
		x:      240,
		y:      120,
		width:  1500,
		height: 960,
	}

	configureDesktopMainWindowStatePersistence(window, statePath)

	callback := window.callbacks[events.Common.WindowDidResize]
	if callback == nil {
		t.Fatal("resize callback = nil, want callback")
	}
	callback(application.NewWindowEvent())

	state, ok := loadDesktopMainWindowState(statePath)
	if !ok {
		t.Fatal("loadDesktopMainWindowState() ok = false, want true")
	}
	if state.X != window.x || state.Y != window.y || state.Width != window.width || state.Height != window.height {
		t.Fatalf("state = %+v, want x=%d y=%d width=%d height=%d", state, window.x, window.y, window.width, window.height)
	}
}

type fakeDesktopLifecycleWindow struct {
	hidden    bool
	x         int
	y         int
	width     int
	height    int
	callbacks map[events.WindowEventType]func(*application.WindowEvent)
}

func (w *fakeDesktopLifecycleWindow) Hide() application.Window {
	w.hidden = true
	return nil
}

func (w *fakeDesktopLifecycleWindow) Position() (int, int) {
	return w.x, w.y
}

func (w *fakeDesktopLifecycleWindow) RegisterHook(eventType events.WindowEventType, callback func(*application.WindowEvent)) func() {
	if w.callbacks == nil {
		w.callbacks = map[events.WindowEventType]func(*application.WindowEvent){}
	}
	w.callbacks[eventType] = callback
	return func() {
		delete(w.callbacks, eventType)
	}
}

func (w *fakeDesktopLifecycleWindow) Size() (int, int) {
	return w.width, w.height
}
