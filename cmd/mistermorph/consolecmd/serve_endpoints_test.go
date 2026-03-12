package consolecmd

import (
	"os"
	"testing"
)

func TestResolveRuntimeEndpoints(t *testing.T) {
	os.Setenv("AUTH_TOKEN_A", "alpha")
	os.Setenv("AUTH_TOKEN_B", "beta")

	t.Run("missing_endpoints", func(t *testing.T) {
		_, err := resolveRuntimeEndpoints(nil)
		if err == nil {
			t.Fatalf("expected error for missing endpoints")
		}
	})

	t.Run("missing_required_fields", func(t *testing.T) {
		_, err := resolveRuntimeEndpoints([]runtimeEndpointConfigRaw{
			{Name: "a", URL: "http://127.0.0.1:8787"},
		})
		if err == nil {
			t.Fatalf("expected error for missing auth_token")
		}
	})

	t.Run("missing_token_env", func(t *testing.T) {
		_, err := resolveRuntimeEndpoints([]runtimeEndpointConfigRaw{
			{Name: "a", URL: "http://127.0.0.1:8787", AuthTokenEnvRef: "MISSING"},
		})
		if err == nil {
			t.Fatalf("expected error for missing token env")
		}
	})

	t.Run("use raw auth token", func(t *testing.T) {
		out, err := resolveRuntimeEndpoints([]runtimeEndpointConfigRaw{
			{Name: "a", URL: "http://127.0.0.1:8787", AuthToken: "alpha"},
		})
		if err != nil {
			t.Fatalf("resolveRuntimeEndpoints failed: %v", err)
		}

		if out[0].AuthToken != "alpha" {
			t.Fatalf("out[0].AuthToken = %q", out[0].AuthToken)
		}
	})

	t.Run("fallback_to_env_ref", func(t *testing.T) {
		_, err := resolveRuntimeEndpoints([]runtimeEndpointConfigRaw{
			{Name: "a", URL: "http://127.0.0.1:8787", AuthTokenEnvRef: "AUTH_TOKEN_A"},
		})
		if err != nil {
			t.Fatalf("resolveRuntimeEndpoints failed: %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		out, err := resolveRuntimeEndpoints([]runtimeEndpointConfigRaw{
			{Name: " Telegram ", URL: "http://127.0.0.1:8787/", AuthToken: "${AUTH_TOKEN_A}"},
			{Name: "Slack", URL: "http://127.0.0.1:8788", AuthToken: "${AUTH_TOKEN_B}"},
		})
		if err != nil {
			t.Fatalf("resolveRuntimeEndpoints failed: %v", err)
		}
		if len(out) != 2 {
			t.Fatalf("len(out) = %d, want 2", len(out))
		}
		if out[0].Name != "Telegram" {
			t.Fatalf("out[0].Name = %q", out[0].Name)
		}
		if out[0].URL != "http://127.0.0.1:8787" {
			t.Fatalf("out[0].URL = %q", out[0].URL)
		}
		if out[0].AuthToken != "alpha" {
			t.Fatalf("out[0].AuthToken = %q", out[0].AuthToken)
		}
		if out[0].Ref == "" || out[1].Ref == "" {
			t.Fatalf("endpoint ref is empty: %#v", out)
		}
		if out[0].Ref == out[1].Ref {
			t.Fatalf("endpoint refs should be unique: %q", out[0].Ref)
		}
	})

	t.Run("duplicate_endpoints", func(t *testing.T) {
		_, err := resolveRuntimeEndpoints([]runtimeEndpointConfigRaw{
			{Name: "Telegram", URL: "http://127.0.0.1:8787", AuthTokenEnvRef: "AUTH_TOKEN_A"},
			{Name: "Telegram", URL: "http://127.0.0.1:8787", AuthTokenEnvRef: "AUTH_TOKEN_A"},
		})
		if err == nil {
			t.Fatalf("expected duplicate endpoint error")
		}
	})
}
