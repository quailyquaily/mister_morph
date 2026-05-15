//go:build wailsdesktop

package main

import "testing"

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
	if opts.URL != "http://127.0.0.1:19080/window/settings" {
		t.Fatalf("url = %q", opts.URL)
	}
	if opts.JS != desktopRuntimeJavaScript {
		t.Fatalf("JS = %q, want desktop runtime marker", opts.JS)
	}
}
