package builtin

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mister_morph/secrets"
)

func TestURLFetchTool_AuthProfileInjectsHeader(t *testing.T) {
	t.Setenv("TEST_API_KEY", "shh_secret")

	type got struct {
		Auth string
	}
	ch := make(chan got, 1)
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		ch <- got{Auth: r.Header.Get("Authorization")}
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    r,
		}, nil
	})

	profile := testProfileForURL(t, "p1", "https://example.test/", secrets.ToolBinding{
		Inject: secrets.Inject{
			Location: "header",
			Name:     "Authorization",
			Format:   "bearer",
		},
		AllowUserHeaders: false,
	})

	tool := NewURLFetchToolWithAuth(true, 2*time.Second, 1024, "test-agent", t.TempDir(), &URLFetchAuth{
		Enabled:       true,
		AllowProfiles: map[string]bool{"p1": true},
		Profiles:      secrets.NewProfileStore(map[string]secrets.AuthProfile{"p1": profile}),
		Resolver:      &secrets.EnvResolver{Aliases: map[string]string{"TEST_API_KEY": "TEST_API_KEY"}},
	})
	tool.HTTPClient = &http.Client{Transport: rt}

	out, err := tool.Execute(context.Background(), map[string]any{
		"url":          "https://example.test/",
		"auth_profile": "p1",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v (out=%q)", err, out)
	}

	req := <-ch
	if req.Auth != "Bearer shh_secret" {
		t.Fatalf("expected Authorization %q, got %q", "Bearer shh_secret", req.Auth)
	}
	if strings.Contains(out, "shh_secret") {
		t.Fatalf("output must not contain secret (out=%q)", out)
	}
}

func TestURLFetchTool_AuthProfileNotAllowlisted(t *testing.T) {
	t.Setenv("TEST_API_KEY", "shh_secret")
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    r,
		}, nil
	})

	profile := testProfileForURL(t, "p1", "https://example.test/", secrets.ToolBinding{
		Inject: secrets.Inject{
			Location: "header",
			Name:     "Authorization",
			Format:   "bearer",
		},
	})

	tool := NewURLFetchToolWithAuth(true, 2*time.Second, 1024, "test-agent", t.TempDir(), &URLFetchAuth{
		Enabled:       true,
		AllowProfiles: map[string]bool{}, // fail-closed
		Profiles:      secrets.NewProfileStore(map[string]secrets.AuthProfile{"p1": profile}),
		Resolver:      &secrets.EnvResolver{},
	})
	tool.HTTPClient = &http.Client{Transport: rt}

	out, err := tool.Execute(context.Background(), map[string]any{
		"url":          "https://example.test/",
		"auth_profile": "p1",
	})
	if err == nil {
		t.Fatalf("expected error, got nil (out=%q)", out)
	}
}

func TestURLFetchTool_AuthProfileRequiresSkillDeclaration_WhenEnabled(t *testing.T) {
	t.Setenv("TEST_API_KEY", "shh_secret")

	profile := testProfileForURL(t, "p1", "https://example.test/", secrets.ToolBinding{
		Inject: secrets.Inject{
			Location: "header",
			Name:     "Authorization",
			Format:   "bearer",
		},
	})

	tool := NewURLFetchToolWithAuth(true, 2*time.Second, 1024, "test-agent", t.TempDir(), &URLFetchAuth{
		Enabled:       true,
		AllowProfiles: map[string]bool{"p1": true},
		Profiles:      secrets.NewProfileStore(map[string]secrets.AuthProfile{"p1": profile}),
		Resolver:      &secrets.EnvResolver{},
	})
	tool.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    r,
		}, nil
	})}

	ctx := secrets.WithSkillAuthProfilePolicy(context.Background(), []string{"other"}, true)
	out, err := tool.Execute(ctx, map[string]any{
		"url":          "https://example.test/",
		"auth_profile": "p1",
	})
	if err == nil {
		t.Fatalf("expected error, got nil (out=%q)", out)
	}
}

func TestURLFetchTool_DeniesSensitiveHeaders(t *testing.T) {
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    r,
		}, nil
	})

	tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent", t.TempDir())
	tool.HTTPClient = &http.Client{Transport: rt}
	out, err := tool.Execute(context.Background(), map[string]any{
		"url": "https://example.test/",
		"headers": map[string]any{
			"X-API-Key": "x",
		},
	})
	if err == nil {
		t.Fatalf("expected error, got nil (out=%q)", out)
	}
}

func TestURLFetchTool_AuthProfileRedirectSameOrigin307(t *testing.T) {
	t.Setenv("TEST_API_KEY", "shh_secret")

	type got struct {
		Path string
		Auth string
		Meth string
		Body string
	}
	ch := make(chan got, 2)
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		ch <- got{
			Path: r.URL.Path,
			Auth: r.Header.Get("Authorization"),
			Meth: r.Method,
			Body: string(body),
		}
		if r.URL.Path == "/start" {
			h := make(http.Header)
			h.Set("Location", "/next")
			return &http.Response{
				StatusCode: http.StatusTemporaryRedirect, // 307
				Header:     h,
				Body:       io.NopCloser(strings.NewReader("redirect")),
				Request:    r,
			}, nil
		}
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    r,
		}, nil
	})

	profile := testProfileForURL(t, "p1", "https://example.test/", secrets.ToolBinding{
		Inject: secrets.Inject{
			Location: "header",
			Name:     "Authorization",
			Format:   "bearer",
		},
		AllowUserHeaders: false,
	})
	profile.Allow.FollowRedirects = true
	profile.Allow.URLPrefixes = []string{"https://example.test/"}
	profile.Allow.Methods = []string{"POST"}

	tool := NewURLFetchToolWithAuth(true, 2*time.Second, 1024, "test-agent", t.TempDir(), &URLFetchAuth{
		Enabled:       true,
		AllowProfiles: map[string]bool{"p1": true},
		Profiles:      secrets.NewProfileStore(map[string]secrets.AuthProfile{"p1": profile}),
		Resolver:      &secrets.EnvResolver{},
	})
	tool.HTTPClient = &http.Client{Transport: rt}

	out, err := tool.Execute(context.Background(), map[string]any{
		"url":          "https://example.test/start",
		"method":       "POST",
		"body":         "hello",
		"auth_profile": "p1",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v (out=%q)", err, out)
	}

	a := <-ch // /start
	b := <-ch // /next

	if a.Path != "/start" || b.Path != "/next" {
		t.Fatalf("expected /start then /next, got %q then %q", a.Path, b.Path)
	}
	if b.Auth != "Bearer shh_secret" {
		t.Fatalf("expected redirected request to include auth header, got %q", b.Auth)
	}
	if b.Meth != http.MethodPost {
		t.Fatalf("expected redirected method %q, got %q", http.MethodPost, b.Meth)
	}
	if b.Body != "hello" {
		t.Fatalf("expected redirected body %q, got %q", "hello", b.Body)
	}
}

func TestURLFetchTool_ResponseBodyRedaction(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("token=" + jwt)),
			Request:    r,
		}, nil
	})

	tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent", t.TempDir())
	tool.HTTPClient = &http.Client{Transport: rt}
	out, err := tool.Execute(context.Background(), map[string]any{
		"url": "https://example.test/",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v (out=%q)", err, out)
	}
	if strings.Contains(out, jwt) {
		t.Fatalf("expected jwt to be redacted (out=%q)", out)
	}
	if !strings.Contains(out, "[redacted]") && !strings.Contains(out, "[redacted_jwt]") {
		t.Fatalf("expected redaction marker in output (out=%q)", out)
	}
}

func testProfileForURL(t *testing.T, id string, rawURL string, binding secrets.ToolBinding) secrets.AuthProfile {
	t.Helper()

	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("bad server URL: %v", err)
	}
	port := 0
	if p := u.Port(); p != "" {
		port, _ = strconv.Atoi(p)
	}
	if port == 0 {
		switch u.Scheme {
		case "http":
			port = 80
		case "https":
			port = 443
		default:
			t.Fatalf("expected server url to include port or use http/https: %q", rawURL)
		}
	}

	return secrets.AuthProfile{
		ID: id,
		Credential: secrets.Credential{
			Kind:      "api_key",
			SecretRef: "TEST_API_KEY",
		},
		Allow: secrets.Allow{
			URLPrefixes:     []string{u.Scheme + "://" + u.Host + "/"},
			Methods:         []string{"GET", "POST", "PUT", "DELETE"},
			FollowRedirects: false,
			AllowProxy:      false,
		},
		Bindings: map[string]secrets.ToolBinding{
			"url_fetch": binding,
		},
	}
}
