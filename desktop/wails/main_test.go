//go:build wailsdesktop

package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func TestBuildDesktopWindowOptions_UsesConsoleURL(t *testing.T) {
	consoleURL := "http://127.0.0.1:19080/console/"
	opts := buildDesktopWindowOptions(consoleURL, nil, nil)
	if opts.URL != consoleURL {
		t.Fatalf("buildDesktopWindowOptions() URL = %q, want %q", opts.URL, consoleURL)
	}
	if opts.Title != "MisterMorph" {
		t.Fatalf("buildDesktopWindowOptions() title = %q, want MisterMorph", opts.Title)
	}
	if opts.JS != desktopRuntimeJavaScript {
		t.Fatalf("buildDesktopWindowOptions() JS = %q, want desktop runtime marker", opts.JS)
	}
	if opts.UseApplicationMenu {
		t.Fatal("buildDesktopWindowOptions() UseApplicationMenu = true, want false")
	}
}

func TestDesktopRuntimeJavaScriptIncludesBindingNames(t *testing.T) {
	if desktopAppBindingPrefix != "main.App." {
		t.Fatalf("desktopAppBindingPrefix = %q, want main.App.", desktopAppBindingPrefix)
	}
	required := []string{
		"__MISTERMORPH_DESKTOP_VERSION__",
		desktopAppBindingPrefix + "CheckUpdate",
		desktopAppBindingPrefix + "OpenDesktopLog",
		desktopAppBindingPrefix + "OpenWindow",
		desktopAppBindingPrefix + "QuitApp",
		desktopAppBindingPrefix + "ReportFrontendReady",
		desktopAppBindingPrefix + "RestartApp",
	}
	for _, item := range required {
		if !strings.Contains(desktopRuntimeJavaScript, item) {
			t.Fatalf("desktopRuntimeJavaScript missing %q", item)
		}
	}
}

func TestBuildDesktopWindowOptions_UsesSavedWindowState(t *testing.T) {
	state := desktopMainWindowState{
		X:      120,
		Y:      80,
		Width:  1440,
		Height: 920,
	}
	opts := buildDesktopWindowOptions("http://127.0.0.1:19080/console/", &state, nil)
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

func TestBuildDesktopWindowOptions_ConstrainedSavedWindowState(t *testing.T) {
	state := desktopMainWindowState{
		X:      3600,
		Y:      1800,
		Width:  2400,
		Height: 1400,
	}
	area := desktopWindowArea{
		X:      0,
		Y:      0,
		Width:  1280,
		Height: 720,
	}

	opts := buildDesktopWindowOptions("http://127.0.0.1:19080/console/", &state, &area)

	if opts.Width != area.Width {
		t.Fatalf("width = %d, want %d", opts.Width, area.Width)
	}
	if opts.Height != area.Height {
		t.Fatalf("height = %d, want %d", opts.Height, area.Height)
	}
	if opts.X != area.X || opts.Y != area.Y {
		t.Fatalf("position = (%d,%d), want (%d,%d)", opts.X, opts.Y, area.X, area.Y)
	}
}

func TestBuildDesktopWindowOptions_ConstrainedBelowDefaultMinimum(t *testing.T) {
	state := desktopMainWindowState{
		X:      100,
		Y:      100,
		Width:  1600,
		Height: 1000,
	}
	area := desktopWindowArea{
		X:      0,
		Y:      0,
		Width:  800,
		Height: 500,
	}

	opts := buildDesktopWindowOptions("http://127.0.0.1:19080/console/", &state, &area)

	if opts.Width != area.Width {
		t.Fatalf("width = %d, want %d", opts.Width, area.Width)
	}
	if opts.Height != area.Height {
		t.Fatalf("height = %d, want %d", opts.Height, area.Height)
	}
	if opts.MinWidth != area.Width {
		t.Fatalf("min width = %d, want %d", opts.MinWidth, area.Width)
	}
	if opts.MinHeight != area.Height {
		t.Fatalf("min height = %d, want %d", opts.MinHeight, area.Height)
	}
	if opts.X != area.X || opts.Y != area.Y {
		t.Fatalf("position = (%d,%d), want (%d,%d)", opts.X, opts.Y, area.X, area.Y)
	}
}

func TestBuildDesktopWindowOptions_ConstrainedPositionInOffsetArea(t *testing.T) {
	state := desktopMainWindowState{
		X:      1100,
		Y:      700,
		Width:  1000,
		Height: 680,
	}
	area := desktopWindowArea{
		X:      100,
		Y:      50,
		Width:  1200,
		Height: 800,
	}

	opts := buildDesktopWindowOptions("http://127.0.0.1:19080/console/", &state, &area)

	if opts.Width != state.Width {
		t.Fatalf("width = %d, want %d", opts.Width, state.Width)
	}
	if opts.Height != state.Height {
		t.Fatalf("height = %d, want %d", opts.Height, state.Height)
	}
	if opts.X != 300 || opts.Y != 170 {
		t.Fatalf("position = (%d,%d), want (300,170)", opts.X, opts.Y)
	}
}

func TestDesktopWindowAreaFromScreenUsesWorkArea(t *testing.T) {
	screen := &application.Screen{
		Bounds: application.Rect{
			X:      0,
			Y:      0,
			Width:  1440,
			Height: 900,
		},
		WorkArea: application.Rect{
			X:      10,
			Y:      20,
			Width:  1400,
			Height: 820,
		},
	}

	got, ok := desktopWindowAreaFromScreen(screen)
	if !ok {
		t.Fatal("desktopWindowAreaFromScreen() ok = false, want true")
	}
	want := desktopWindowArea{
		X:      10,
		Y:      20,
		Width:  1400,
		Height: 820,
	}
	if got != want {
		t.Fatalf("desktopWindowAreaFromScreen() = %+v, want %+v", got, want)
	}
}

func TestConfigureDesktopMainWindowLifecycle_HidesAndCancelsCloseOnMac(t *testing.T) {
	window := &fakeDesktopLifecycleWindow{}

	configureDesktopMainWindowLifecycleForGOOS(window, "darwin", nil)

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

func TestConfigureDesktopMainWindowLifecycle_QuitsOffMac(t *testing.T) {
	window := &fakeDesktopLifecycleWindow{}
	quitCalled := false

	configureDesktopMainWindowLifecycleForGOOS(window, "linux", func() {
		quitCalled = true
	})

	callback := window.callbacks[events.Common.WindowClosing]
	if callback == nil {
		t.Fatal("registered callback = nil, want callback")
	}

	event := application.NewWindowEvent()
	callback(event)

	if !quitCalled {
		t.Fatal("quit called = false, want true")
	}
	if event.IsCancelled() {
		t.Fatal("close event cancelled = true, want false")
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
