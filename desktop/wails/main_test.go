//go:build wailsdesktop

package main

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func TestBuildDesktopWindowOptions_UsesConsoleURL(t *testing.T) {
	consoleURL := "http://127.0.0.1:19080/console/"
	opts := buildDesktopWindowOptions(consoleURL)
	if opts.URL != consoleURL {
		t.Fatalf("buildDesktopWindowOptions() URL = %q, want %q", opts.URL, consoleURL)
	}
	if opts.Title != "MisterMorph" {
		t.Fatalf("buildDesktopWindowOptions() title = %q, want MisterMorph", opts.Title)
	}
	if opts.JS != desktopRuntimeJavaScript {
		t.Fatalf("buildDesktopWindowOptions() JS = %q, want desktop runtime marker", opts.JS)
	}
}

func TestConfigureDesktopMainWindowLifecycle_HidesAndCancelsCloseOnMac(t *testing.T) {
	window := &fakeDesktopLifecycleWindow{}

	configureDesktopMainWindowLifecycleForGOOS(window, "darwin")

	if window.eventType != events.Common.WindowClosing {
		t.Fatalf("registered event = %v, want %v", window.eventType, events.Common.WindowClosing)
	}
	if window.callback == nil {
		t.Fatal("registered callback = nil, want callback")
	}

	event := application.NewWindowEvent()
	window.callback(event)

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

	if window.callback != nil {
		t.Fatal("registered callback != nil, want no hook outside macOS")
	}
}

type fakeDesktopLifecycleWindow struct {
	hidden    bool
	eventType events.WindowEventType
	callback  func(*application.WindowEvent)
}

func (w *fakeDesktopLifecycleWindow) Hide() application.Window {
	w.hidden = true
	return nil
}

func (w *fakeDesktopLifecycleWindow) RegisterHook(eventType events.WindowEventType, callback func(*application.WindowEvent)) func() {
	w.eventType = eventType
	w.callback = callback
	return func() {
		w.callback = nil
	}
}
