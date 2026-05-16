//go:build wailsdesktop

package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestNormalizeExternalBrowserURL(t *testing.T) {
	cases := []struct {
		name    string
		rawURL  string
		want    string
		wantErr bool
	}{
		{
			name:   "https",
			rawURL: " https://example.com/path?q=a~b(1)! ",
			want:   "https://example.com/path?q=a~b(1)!",
		},
		{
			name:   "http",
			rawURL: "http://example.com",
			want:   "http://example.com",
		},
		{
			name:    "missing host",
			rawURL:  "https:///path",
			wantErr: true,
		},
		{
			name:    "unsupported scheme",
			rawURL:  "file:///tmp/example",
			wantErr: true,
		},
		{
			name:    "control character",
			rawURL:  "https://example.com/\npath",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeExternalBrowserURL(tc.rawURL)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("normalizeExternalBrowserURL() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeExternalBrowserURL() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("normalizeExternalBrowserURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveDesktopWindowURL(t *testing.T) {
	cases := []struct {
		name       string
		consoleURL string
		rawPath    string
		want       string
		wantErr    bool
	}{
		{
			name:       "root base",
			consoleURL: "http://127.0.0.1:19080/",
			rawPath:    "/window/settings?section=agent",
			want:       "http://127.0.0.1:19080/window/settings?section=agent",
		},
		{
			name:       "nested base",
			consoleURL: "http://127.0.0.1:19080/console/",
			rawPath:    "/window/settings",
			want:       "http://127.0.0.1:19080/console/window/settings",
		},
		{
			name:       "base without trailing slash",
			consoleURL: "http://127.0.0.1:19080/console",
			rawPath:    "/window/logs",
			want:       "http://127.0.0.1:19080/console/window/logs",
		},
		{
			name:       "reject absolute URL",
			consoleURL: "http://127.0.0.1:19080/",
			rawPath:    "https://example.com/window",
			wantErr:    true,
		},
		{
			name:       "reject protocol relative URL",
			consoleURL: "http://127.0.0.1:19080/",
			rawPath:    "//example.com/window",
			wantErr:    true,
		},
		{
			name:       "reject relative path",
			consoleURL: "http://127.0.0.1:19080/",
			rawPath:    "window/logs",
			wantErr:    true,
		},
		{
			name:       "reject non window route",
			consoleURL: "http://127.0.0.1:19080/",
			rawPath:    "/settings",
			wantErr:    true,
		},
		{
			name:       "reject dot segment",
			consoleURL: "http://127.0.0.1:19080/",
			rawPath:    "/window/../settings",
			wantErr:    true,
		},
		{
			name:       "reject control character",
			consoleURL: "http://127.0.0.1:19080/",
			rawPath:    "/window\n/settings",
			wantErr:    true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveDesktopWindowURL(tc.consoleURL, tc.rawPath)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveDesktopWindowURL() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveDesktopWindowURL() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("resolveDesktopWindowURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDesktopChildWindowName(t *testing.T) {
	cases := []struct {
		name    string
		rawPath string
		want    string
	}{
		{
			name:    "raw json",
			rawPath: "/window/raw-json?payload_id=abc",
			want:    "mistermorph-window-raw-json",
		},
		{
			name:    "nested route uses first segment",
			rawPath: "/window/settings/agent",
			want:    "mistermorph-window-settings",
		},
		{
			name:    "window root",
			rawPath: "/window",
			want:    "mistermorph-window",
		},
		{
			name:    "non window route",
			rawPath: "/settings",
			want:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := desktopChildWindowName(tc.rawPath); got != tc.want {
				t.Fatalf("desktopChildWindowName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDesktopWindowIDFromName(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{name: "mistermorph-window-setup-picker", want: "setup-picker"},
		{name: "mistermorph-window", want: ""},
		{name: "main", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := desktopWindowIDFromName(tc.name); got != tc.want {
				t.Fatalf("desktopWindowIDFromName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildDesktopChildWindowOptions(t *testing.T) {
	opts, err := buildDesktopChildWindowOptions("http://127.0.0.1:19080/window/settings", DesktopWindowRequest{
		Title:     " Settings ",
		Width:     10,
		Height:    5000,
		MinWidth:  0,
		MinHeight: 0,
	})
	if err != nil {
		t.Fatalf("buildDesktopChildWindowOptions() error = %v", err)
	}
	if opts.Title != "Settings" {
		t.Fatalf("title = %q, want Settings", opts.Title)
	}
	if opts.Width != defaultDesktopChildWindowMinWidth {
		t.Fatalf("width = %d, want %d", opts.Width, defaultDesktopChildWindowMinWidth)
	}
	if opts.Height != maxDesktopChildWindowHeight {
		t.Fatalf("height = %d, want %d", opts.Height, maxDesktopChildWindowHeight)
	}
	if opts.MinWidth != defaultDesktopChildWindowMinWidth {
		t.Fatalf("min width = %d, want %d", opts.MinWidth, defaultDesktopChildWindowMinWidth)
	}
	if opts.MinHeight != defaultDesktopChildWindowMinHeight {
		t.Fatalf("min height = %d, want %d", opts.MinHeight, defaultDesktopChildWindowMinHeight)
	}
	if opts.InitialPosition != application.WindowCentered {
		t.Fatalf("initial position = %d, want centered", opts.InitialPosition)
	}
	if opts.URL != "http://127.0.0.1:19080/window/settings" {
		t.Fatalf("url = %q", opts.URL)
	}
	if opts.JS != desktopRuntimeJavaScript {
		t.Fatalf("JS = %q, want desktop runtime marker", opts.JS)
	}
	if opts.UseApplicationMenu {
		t.Fatal("UseApplicationMenu = true, want false")
	}
	if !opts.DisableResize {
		t.Fatal("DisableResize = false, want true")
	}

	manual, err := buildDesktopChildWindowOptions("http://127.0.0.1:19080/window/settings", DesktopWindowRequest{
		Position: "manual",
		X:        -80,
		Y:        120,
	})
	if err != nil {
		t.Fatalf("buildDesktopChildWindowOptions() manual position error = %v", err)
	}
	if manual.Width != defaultDesktopChildWindowWidth {
		t.Fatalf("manual width = %d, want default %d", manual.Width, defaultDesktopChildWindowWidth)
	}
	if manual.Height != defaultDesktopChildWindowHeight {
		t.Fatalf("manual height = %d, want default %d", manual.Height, defaultDesktopChildWindowHeight)
	}
	if manual.InitialPosition != application.WindowXY || manual.X != -80 || manual.Y != 120 {
		t.Fatalf("manual position = (%d,%d,%d), want WindowXY -80 120", manual.InitialPosition, manual.X, manual.Y)
	}

	if _, err := buildDesktopChildWindowOptions("http://127.0.0.1:19080/window/settings", DesktopWindowRequest{Position: "corner"}); err == nil {
		t.Fatal("buildDesktopChildWindowOptions() invalid position error = nil, want error")
	}
}

func TestParseDesktopOpenWindowMessage(t *testing.T) {
	req, err := parseDesktopOpenWindowMessage(desktopOpenWindowPrefix + `{"path":"/window/raw-json","title":"RAW JSON","width":980}`)
	if err != nil {
		t.Fatalf("parseDesktopOpenWindowMessage() error = %v", err)
	}
	if req.Path != "/window/raw-json" || req.Title != "RAW JSON" || req.Width != 980 {
		t.Fatalf("request = %#v", req)
	}

	if _, err := parseDesktopOpenWindowMessage(desktopOpenWindowPrefix); err == nil {
		t.Fatal("parseDesktopOpenWindowMessage() empty error = nil, want error")
	}
}

func TestParseDesktopWindowMessage(t *testing.T) {
	msg, err := parseDesktopWindowMessage(desktopWindowMessagePrefix + `{"target":" parent ","type":" runtime:poke-submitted ","request_id":" abc ","source":"ignored","_delivery_id":" delivery-1 ","payload":{"poked_at":"2026-05-15T00:00:00Z"}}`)
	if err != nil {
		t.Fatalf("parseDesktopWindowMessage() error = %v", err)
	}
	if msg.Target != "parent" || msg.Type != "runtime:poke-submitted" || msg.RequestID != "abc" || msg.DeliveryID != "delivery-1" || msg.Source != "" {
		t.Fatalf("message = %#v", msg)
	}
	if string(msg.Payload) != `{"poked_at":"2026-05-15T00:00:00Z"}` {
		t.Fatalf("payload = %s", msg.Payload)
	}

	if _, err := parseDesktopWindowMessage(desktopWindowMessagePrefix + `{"target":"parent"}`); err == nil {
		t.Fatal("parseDesktopWindowMessage() missing type error = nil, want error")
	}
	if _, err := parseDesktopWindowMessage(desktopWindowMessagePrefix + `{"type":"runtime:poke-submitted"}`); err == nil {
		t.Fatal("parseDesktopWindowMessage() missing target error = nil, want error")
	}
}

func TestResolveDesktopWindowMessageTarget(t *testing.T) {
	app := NewApp("http://127.0.0.1:19080/", "", time.Now(), nil)
	app.rememberDesktopWindowParent("mistermorph-window-poke", "window-1")

	target, err := app.resolveDesktopWindowMessageTarget("mistermorph-window-poke", DesktopWindowMessage{Target: "parent", Type: "x"})
	if err != nil {
		t.Fatalf("resolve parent error = %v", err)
	}
	if target != "window-1" {
		t.Fatalf("parent target = %q, want window-1", target)
	}

	target, err = app.resolveDesktopWindowMessageTarget("window-1", DesktopWindowMessage{WindowID: "raw-json", Type: "x"})
	if err != nil {
		t.Fatalf("resolve window id error = %v", err)
	}
	if target != "mistermorph-window-raw-json" {
		t.Fatalf("window id target = %q, want mistermorph-window-raw-json", target)
	}

	target, err = app.resolveDesktopWindowMessageTarget("window-1", DesktopWindowMessage{Target: "self", Type: "x"})
	if err != nil {
		t.Fatalf("resolve self error = %v", err)
	}
	if target != "window-1" {
		t.Fatalf("self target = %q, want window-1", target)
	}
}

func TestReportFrontendReadyWritesOnce(t *testing.T) {
	var out bytes.Buffer
	app := NewApp("http://127.0.0.1:19080/", "", time.Now().Add(-time.Second), &out)

	app.ReportFrontendReady()
	app.ReportFrontendReady()

	got := out.String()
	if count := strings.Count(got, "desktop_startup_frontend_ready"); count != 1 {
		t.Fatalf("frontend ready metric count = %d, want 1 in %q", count, got)
	}
	for _, want := range []string{
		"duration_ms=",
		"desktop_go_alloc_bytes=",
		"desktop_go_sys_bytes=",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("frontend ready metric missing %q in %q", want, got)
		}
	}
}
