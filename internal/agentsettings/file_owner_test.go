package agentsettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/configsettings"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/secref"
	"github.com/spf13/viper"
)

type fakeFileOwnerSecretBackend struct {
	values  map[string]string
	puts    []string
	labels  map[string]string
	deletes []string
	putErr  error
}

func (f *fakeFileOwnerSecretBackend) LookupEnv(string) (string, bool) {
	return "", false
}

func (f *fakeFileOwnerSecretBackend) GetAWSSecretString(context.Context, string) (string, error) {
	return "", secref.ErrAWSSecretNotFound
}

func (f *fakeFileOwnerSecretBackend) GetOSSecretString(_ context.Context, id string) (string, error) {
	value, ok := f.values[id]
	if !ok {
		return "", secref.ErrOSSecretNotFound
	}
	return value, nil
}

func (f *fakeFileOwnerSecretBackend) Get(ctx context.Context, id string) ([]byte, error) {
	value, err := f.GetOSSecretString(ctx, id)
	return []byte(value), err
}

func (f *fakeFileOwnerSecretBackend) Put(_ context.Context, id, configKey string, value []byte) error {
	if f.putErr != nil {
		return f.putErr
	}
	if f.values == nil {
		f.values = map[string]string{}
	}
	f.values[id] = string(value)
	f.puts = append(f.puts, id)
	if f.labels == nil {
		f.labels = map[string]string{}
	}
	f.labels[id] = configKey
	return nil
}

func (f *fakeFileOwnerSecretBackend) Delete(_ context.Context, id string) error {
	if _, ok := f.values[id]; !ok {
		return secref.ErrOSSecretNotFound
	}
	delete(f.values, id)
	f.deletes = append(f.deletes, id)
	return nil
}

func TestFileOwnerRotatesOSManagedAPIKey(t *testing.T) {
	const oldID = "b_LsX7HLzAR3OShG7YjRcw"
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("llm:\n  provider: openai\n  model: gpt-test\n  api_key: "+secref.OSSecretRef(oldID)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := &fakeFileOwnerSecretBackend{values: map[string]string{oldID: "old-secret"}}
	reader := readFileOwnerTestConfigWithSource(t, configPath, backend)
	owner := NewFileOwner(FileOwnerOptions{
		ConfigPath:   configPath,
		Reader:       reader,
		SecretSource: backend,
		OSStore:      backend,
	})

	before, err := owner.View(context.Background())
	if err != nil {
		t.Fatalf("View() error = %v", err)
	}
	if before.LLM.APIKey != "" {
		t.Fatalf("View() exposed API key %q", before.LLM.APIKey)
	}
	status := before.SecretFields.LLM["api_key"]
	if !status.Configured || status.Source != "os" || !status.Editable {
		t.Fatalf("api_key secret status = %#v", status)
	}

	replacement := "new-secret"
	after, err := owner.Update(context.Background(), AgentSettingsUpdate{
		LLM: LLMSettingsUpdate{LLMConfigFieldsUpdate: LLMConfigFieldsUpdate{APIKey: &replacement}},
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if after.LLM.APIKey != "" || !after.SecretFields.LLM["api_key"].Configured {
		t.Fatalf("updated view exposed or lost API key status: %#v", after)
	}
	if len(backend.puts) != 1 || len(backend.deletes) != 1 || backend.deletes[0] != oldID {
		t.Fatalf("secret store operations puts=%v deletes=%v", backend.puts, backend.deletes)
	}
	newID := backend.puts[0]
	if backend.labels[newID] != "llm.api_key" {
		t.Fatalf("stored config key = %q, want llm.api_key", backend.labels[newID])
	}
	if backend.values[newID] != replacement {
		t.Fatalf("new keyring value = %q, want replacement", backend.values[newID])
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), replacement) || !strings.Contains(string(raw), secref.OSSecretRef(newID)) {
		t.Fatalf("config did not contain only the new OS ref:\n%s", raw)
	}
}

func TestFileOwnerReportsEnvironmentManagedSecretStatus(t *testing.T) {
	t.Setenv("MISTER_MORPH_LLM_API_KEY", "environment-secret")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("llm:\n  provider: openai\n  model: gpt-test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	owner := NewFileOwner(FileOwnerOptions{
		ConfigPath: configPath,
		Reader:     readFileOwnerTestConfig(t, configPath),
	})

	view, err := owner.View(context.Background())
	if err != nil {
		t.Fatalf("View() error = %v", err)
	}
	status := view.SecretFields.LLM["api_key"]
	if !status.Configured || status.Source != string(secref.RefKindEnv) || status.Editable {
		t.Fatalf("api_key secret status = %#v, want configured non-editable environment secret", status)
	}
}

func TestFileOwnerKeepsOSSecretStillReferencedByProfile(t *testing.T) {
	const sharedID = "b_LsX7HLzAR3OShG7YjRcw"
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	config := "llm:\n  provider: openai\n  model: gpt-main\n  api_key: " + secref.OSSecretRef(sharedID) + "\n  profiles:\n    backup:\n      provider: openai\n      model: gpt-backup\n      api_key: " + secref.OSSecretRef(sharedID) + "\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := &fakeFileOwnerSecretBackend{values: map[string]string{sharedID: "shared-secret"}}
	owner := NewFileOwner(FileOwnerOptions{
		ConfigPath:   configPath,
		Reader:       readFileOwnerTestConfigWithSource(t, configPath, backend),
		SecretSource: backend,
		OSStore:      backend,
	})
	empty := ""
	if _, err := owner.Update(context.Background(), AgentSettingsUpdate{
		LLM: LLMSettingsUpdate{LLMConfigFieldsUpdate: LLMConfigFieldsUpdate{APIKey: &empty}},
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if backend.values[sharedID] != "shared-secret" {
		t.Fatal("shared OS secret was deleted while the profile still referenced it")
	}
}

func TestFileOwnerPreservesOSManagedSecretOnUnrelatedUpdate(t *testing.T) {
	const id = "b_LsX7HLzAR3OShG7YjRcw"
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("llm:\n  provider: openai\n  model: before\n  api_key: "+secref.OSSecretRef(id)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := &fakeFileOwnerSecretBackend{values: map[string]string{id: "stored-secret"}}
	owner := NewFileOwner(FileOwnerOptions{
		ConfigPath:   configPath,
		Reader:       readFileOwnerTestConfigWithSource(t, configPath, backend),
		SecretSource: backend,
		OSStore:      backend,
	})
	model := "after"
	if _, err := owner.Update(context.Background(), AgentSettingsUpdate{
		LLM: LLMSettingsUpdate{LLMConfigFieldsUpdate: LLMConfigFieldsUpdate{Model: &model}},
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if len(backend.puts) != 0 || len(backend.deletes) != 0 {
		t.Fatalf("unrelated update changed secret store: puts=%v deletes=%v", backend.puts, backend.deletes)
	}
	raw, _ := os.ReadFile(configPath)
	if !strings.Contains(string(raw), secref.OSSecretRef(id)) {
		t.Fatalf("unrelated update lost OS ref:\n%s", raw)
	}
}

func TestFileOwnerStoresNewAPIKeyInOSStore(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("llm:\n  provider: openai\n  model: gpt-test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := &fakeFileOwnerSecretBackend{values: map[string]string{}}
	owner := NewFileOwner(FileOwnerOptions{
		ConfigPath:   configPath,
		Reader:       readFileOwnerTestConfigWithSource(t, configPath, backend),
		SecretSource: backend,
		OSStore:      backend,
	})
	apiKey := "new-secret"
	if _, err := owner.Update(context.Background(), AgentSettingsUpdate{
		LLM: LLMSettingsUpdate{LLMConfigFieldsUpdate: LLMConfigFieldsUpdate{APIKey: &apiKey}},
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	raw, _ := os.ReadFile(configPath)
	if strings.Contains(string(raw), apiKey) || len(backend.puts) != 1 || !strings.Contains(string(raw), secref.OSSecretRef(backend.puts[0])) {
		t.Fatalf("new API key was not stored as OS ref: puts=%v\n%s", backend.puts, raw)
	}
}

func TestFileOwnerFallsBackToPlaintextWhenOSStoreWriteFails(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	original := "llm:\n  provider: openai\n  model: gpt-test\n"
	if err := os.WriteFile(configPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := &fakeFileOwnerSecretBackend{putErr: secref.ErrOSStoreUnavailable}
	owner := NewFileOwner(FileOwnerOptions{
		ConfigPath:   configPath,
		Reader:       readFileOwnerTestConfigWithSource(t, configPath, backend),
		SecretSource: backend,
		OSStore:      backend,
	})
	apiKey := "plaintext-fallback"
	view, err := owner.Update(context.Background(), AgentSettingsUpdate{
		LLM: LLMSettingsUpdate{LLMConfigFieldsUpdate: LLMConfigFieldsUpdate{APIKey: &apiKey}},
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	raw, _ := os.ReadFile(configPath)
	if !strings.Contains(string(raw), "api_key: "+apiKey) || strings.Contains(string(raw), "${secret:") {
		t.Fatalf("failed store write did not fall back to plaintext:\n%s", raw)
	}
	if status := view.SecretFields.LLM["api_key"]; !status.Configured || status.Source != "file" {
		t.Fatalf("api_key secret status = %#v, want plaintext file source", status)
	}
}

func TestFileOwnerPreservesAndRotatesOSManagedProfileSecret(t *testing.T) {
	const oldID = "b_LsX7HLzAR3OShG7YjRcw"
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	config := "llm:\n  provider: openai\n  model: gpt-main\n  profiles:\n    backup:\n      provider: openai\n      model: gpt-old\n      api_key: " + secref.OSSecretRef(oldID) + "\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := &fakeFileOwnerSecretBackend{values: map[string]string{oldID: "old-profile-secret"}}
	owner := NewFileOwner(FileOwnerOptions{
		ConfigPath:   configPath,
		Reader:       readFileOwnerTestConfigWithSource(t, configPath, backend),
		SecretSource: backend,
		OSStore:      backend,
	})

	modelOnly := LLMProfileUpdate{
		OriginalName: "backup",
		LLMProfileSettingsPayload: LLMProfileSettingsPayload{
			Name: "backup",
			LLMConfigFieldsPayload: LLMConfigFieldsPayload{
				Provider: "openai",
				Model:    "gpt-new",
			},
		},
	}
	view, err := owner.Update(context.Background(), AgentSettingsUpdate{LLM: LLMSettingsUpdate{Profile: &modelOnly}})
	if err != nil {
		t.Fatalf("model-only profile Update() error = %v", err)
	}
	if len(backend.puts) != 0 || len(backend.deletes) != 0 {
		t.Fatalf("model-only update changed profile secret: puts=%v deletes=%v", backend.puts, backend.deletes)
	}
	status := view.SecretFields.LLMProfiles["backup"]["api_key"]
	if !status.Configured || status.Source != "os" || !status.Editable {
		t.Fatalf("profile api_key status = %#v", status)
	}
	raw, _ := os.ReadFile(configPath)
	if !strings.Contains(string(raw), secref.OSSecretRef(oldID)) {
		t.Fatalf("model-only update lost profile OS ref:\n%s", raw)
	}

	replacement := modelOnly
	replacement.APIKey = "new-profile-secret"
	if _, err := owner.Update(context.Background(), AgentSettingsUpdate{LLM: LLMSettingsUpdate{Profile: &replacement}}); err != nil {
		t.Fatalf("secret profile Update() error = %v", err)
	}
	if len(backend.puts) != 1 || len(backend.deletes) != 1 || backend.deletes[0] != oldID {
		t.Fatalf("profile rotation operations puts=%v deletes=%v", backend.puts, backend.deletes)
	}
	newID := backend.puts[0]
	if backend.labels[newID] != "llm.profiles.backup.api_key" {
		t.Fatalf("stored config key = %q, want llm.profiles.backup.api_key", backend.labels[newID])
	}
	raw, _ = os.ReadFile(configPath)
	if strings.Contains(string(raw), "new-profile-secret") || !strings.Contains(string(raw), secref.OSSecretRef(newID)) {
		t.Fatalf("profile secret was not stored as OS ref:\n%s", raw)
	}
}

func TestFileOwnerReadsAndUpdatesCompleteLLMProfile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	config := `llm:
  provider: openai
  model: gpt-main
  profiles:
    research:
      provider: bedrock
      model: old-model
      supports_image_parts: true
      headers:
        X-Trace: old
      cache_ttl: 5m
      cache_key_prefix: research
      request_timeout: 45s
      temperature: "0.3"
      reasoning_budget_tokens: "2048"
      azure:
        deployment: preserved
      bedrock:
        aws_key: old-key
        aws_secret: old-secret
        aws_session_token: old-token
        aws_profile: old-profile
        region: us-east-1
      future_field: keep-me
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := &fakeFileOwnerSecretBackend{values: map[string]string{}}
	owner := NewFileOwner(FileOwnerOptions{
		ConfigPath:   configPath,
		Reader:       readFileOwnerTestConfigWithSource(t, configPath, backend),
		SecretSource: backend,
		OSStore:      backend,
	})

	before, err := owner.View(context.Background())
	if err != nil {
		t.Fatalf("View() error = %v", err)
	}
	profile := before.LLM.Profiles[0]
	if profile.SupportsImageParts != "true" || profile.Headers["x-trace"] != "old" || profile.CacheTTL != "5m" ||
		profile.CacheKeyPrefix != "research" || profile.RequestTimeout != "45s" || profile.Temperature != "0.3" ||
		profile.ReasoningBudgetTokens != "2048" || profile.AzureDeployment != "preserved" ||
		profile.BedrockAWSProfile != "old-profile" {
		t.Fatalf("incomplete profile view: %#v", profile)
	}
	if profile.BedrockAWSSessionToken != "" {
		t.Fatalf("View() exposed session token %q", profile.BedrockAWSSessionToken)
	}

	profile.Headers = map[string]string{"X-Trace": "new", "X-Mode": "fast"}
	profile.CacheTTL = "10m"
	profile.RequestTimeout = "1m"
	profile.BedrockAWSProfile = ""
	profile.BedrockAWSSessionToken = "new-session-token"
	update := LLMProfileUpdate{OriginalName: "research", LLMProfileSettingsPayload: profile}
	update.providedSecretFields = map[string]bool{"bedrock_aws_session_token": true}
	if _, err := owner.Update(context.Background(), AgentSettingsUpdate{LLM: LLMSettingsUpdate{Profile: &update}}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"cache_ttl: 10m", "request_timeout: 1m", "X-Mode: fast", "future_field: keep-me"} {
		if !strings.Contains(text, want) {
			t.Errorf("updated config missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "new-session-token") || len(backend.puts) != 1 ||
		backend.labels[backend.puts[0]] != "llm.profiles.research.bedrock.aws_session_token" {
		t.Fatalf("session token was not protected: puts=%v labels=%v\n%s", backend.puts, backend.labels, text)
	}
}

func TestFileOwnerSavingSecondProfilePreservesFirstProfileSecretRef(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	config := `llm:
  provider: openai
  model: gpt-main
  profiles:
    first:
      provider: openai
      model: gpt-first
    second:
      provider: openai
      model: gpt-second
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := &fakeFileOwnerSecretBackend{values: map[string]string{}}
	owner := NewFileOwner(FileOwnerOptions{
		ConfigPath:   configPath,
		Reader:       readFileOwnerTestConfigWithSource(t, configPath, backend),
		SecretSource: backend,
		OSStore:      backend,
	})

	updateProfile := func(name, apiKey string) {
		t.Helper()
		var update AgentSettingsUpdate
		body := fmt.Sprintf(
			`{"llm":{"profile":{"original_name":%q,"name":%q,"provider":"openai","model":%q,"api_key":%q}}}`,
			name,
			name,
			"gpt-"+name,
			apiKey,
		)
		if err := json.Unmarshal([]byte(body), &update); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.Update(context.Background(), update); err != nil {
			t.Fatalf("save profile %q: %v", name, err)
		}
	}

	updateProfile("first", "first-secret")
	firstID := backend.puts[0]
	updateProfile("second", "second-secret")

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "first-secret") || strings.Contains(string(raw), "second-secret") {
		t.Fatalf("saving second profile exposed a profile secret:\n%s", raw)
	}
	if !strings.Contains(string(raw), secref.OSSecretRef(firstID)) {
		t.Fatalf("saving second profile replaced the first profile secret ref:\n%s", raw)
	}
	if len(backend.puts) != 2 {
		t.Fatalf("secret store writes = %v, want one write per profile", backend.puts)
	}
	if !strings.Contains(string(raw), secref.OSSecretRef(backend.puts[1])) {
		t.Fatalf("second profile secret ref is missing:\n%s", raw)
	}
}

func TestFileOwnerSingleProfileUpdatesPreserveUnrelatedYAMLAndFallbacks(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	config := `llm:
  provider: openai
  model: gpt-main
  profiles:
    first:
      provider: openai
      model: gpt-first
    untouched:
      provider: openai
      model: gpt-untouched
      extension: keep-me
  routes:
    main_loop:
      fallback_profiles: [first, untouched]
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	owner := NewFileOwner(FileOwnerOptions{
		ConfigPath: configPath,
		Reader:     readFileOwnerTestConfig(t, configPath),
	})

	rename := LLMProfileUpdate{
		OriginalName: "first",
		LLMProfileSettingsPayload: LLMProfileSettingsPayload{
			Name: "renamed",
			LLMConfigFieldsPayload: LLMConfigFieldsPayload{
				Provider: "openai",
				Model:    "gpt-renamed",
			},
		},
	}
	if _, err := owner.Update(context.Background(), AgentSettingsUpdate{
		LLM: LLMSettingsUpdate{Profile: &rename},
	}); err != nil {
		t.Fatalf("rename profile: %v", err)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	fallbacks := readFileOwnerTestConfig(t, configPath).GetStringSlice("llm.routes.main_loop.fallback_profiles")
	if strings.Join(fallbacks, ",") != "renamed,untouched" {
		t.Fatalf("profile rename did not update fallbacks:\n%s", raw)
	}
	if !strings.Contains(text, "extension: keep-me") {
		t.Fatalf("profile rename removed unrelated YAML:\n%s", raw)
	}

	name := "renamed"
	if _, err := owner.Update(context.Background(), AgentSettingsUpdate{
		LLM: LLMSettingsUpdate{DeleteProfile: &name},
	}); err != nil {
		t.Fatalf("delete profile: %v", err)
	}
	raw, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text = string(raw)
	fallbacks = readFileOwnerTestConfig(t, configPath).GetStringSlice("llm.routes.main_loop.fallback_profiles")
	if strings.Join(fallbacks, ",") != "untouched" || strings.Contains(text, "renamed:") {
		t.Fatalf("profile delete did not update profile and fallback:\n%s", raw)
	}
	if !strings.Contains(text, "extension: keep-me") {
		t.Fatalf("profile delete removed unrelated YAML:\n%s", raw)
	}
}

func TestFileOwnerClearsOSManagedProfileSecret(t *testing.T) {
	const id = "b_LsX7HLzAR3OShG7YjRcw"
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	config := "llm:\n  provider: openai\n  model: gpt-main\n  profiles:\n    backup:\n      provider: openai\n      model: gpt-backup\n      api_key: " + secref.OSSecretRef(id) + "\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := &fakeFileOwnerSecretBackend{values: map[string]string{id: "profile-secret"}}
	owner := NewFileOwner(FileOwnerOptions{
		ConfigPath:   configPath,
		Reader:       readFileOwnerTestConfigWithSource(t, configPath, backend),
		SecretSource: backend,
		OSStore:      backend,
	})
	var update AgentSettingsUpdate
	if err := json.Unmarshal([]byte(`{"llm":{"profile":{"original_name":"backup","name":"backup","provider":"openai","model":"gpt-backup","api_key":""}}}`), &update); err != nil {
		t.Fatal(err)
	}

	view, err := owner.Update(context.Background(), update)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if view.SecretFields.LLMProfiles["backup"]["api_key"].Configured {
		t.Fatalf("cleared profile secret is still configured: %#v", view.SecretFields.LLMProfiles["backup"])
	}
	if _, ok := backend.values[id]; ok || len(backend.deletes) != 1 || backend.deletes[0] != id {
		t.Fatalf("cleared profile secret was not deleted: values=%v deletes=%v", backend.values, backend.deletes)
	}
	raw, _ := os.ReadFile(configPath)
	if strings.Contains(string(raw), secref.OSSecretRef(id)) {
		t.Fatalf("cleared profile ref remains in config:\n%s", raw)
	}
}

func TestFileOwnerStoresBulkProfileSecretsInOSStore(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("llm:\n  provider: openai\n  model: gpt-main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := &fakeFileOwnerSecretBackend{values: map[string]string{}}
	owner := NewFileOwner(FileOwnerOptions{
		ConfigPath:   configPath,
		Reader:       readFileOwnerTestConfigWithSource(t, configPath, backend),
		SecretSource: backend,
		OSStore:      backend,
	})
	profiles := []LLMProfileSettingsPayload{{
		Name: "backup",
		LLMConfigFieldsPayload: LLMConfigFieldsPayload{
			Provider: "openai",
			Model:    "gpt-backup",
			APIKey:   "bulk-profile-secret",
		},
	}}

	view, err := owner.Update(context.Background(), AgentSettingsUpdate{LLM: LLMSettingsUpdate{Profiles: &profiles}})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if len(backend.puts) != 1 || backend.values[backend.puts[0]] != "bulk-profile-secret" {
		t.Fatalf("bulk profile secret store writes = %v values=%v", backend.puts, backend.values)
	}
	if !view.SecretFields.LLMProfiles["backup"]["api_key"].Configured {
		t.Fatalf("bulk profile secret status = %#v", view.SecretFields.LLMProfiles["backup"])
	}
	raw, _ := os.ReadFile(configPath)
	if strings.Contains(string(raw), "bulk-profile-secret") || !strings.Contains(string(raw), secref.OSSecretRef(backend.puts[0])) {
		t.Fatalf("bulk profile secret was not replaced with an OS ref:\n%s", raw)
	}
}

func TestFileOwnerDeletesUnreferencedOSSecretWithProfile(t *testing.T) {
	const id = "b_LsX7HLzAR3OShG7YjRcw"
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	config := "llm:\n  provider: openai\n  model: gpt-main\n  profiles:\n    backup:\n      provider: openai\n      model: gpt-backup\n      api_key: " + secref.OSSecretRef(id) + "\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := &fakeFileOwnerSecretBackend{values: map[string]string{id: "profile-secret"}}
	owner := NewFileOwner(FileOwnerOptions{
		ConfigPath:   configPath,
		Reader:       readFileOwnerTestConfigWithSource(t, configPath, backend),
		SecretSource: backend,
		OSStore:      backend,
	})
	name := "backup"

	if _, err := owner.Update(context.Background(), AgentSettingsUpdate{LLM: LLMSettingsUpdate{DeleteProfile: &name}}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if _, ok := backend.values[id]; ok || len(backend.deletes) != 1 || backend.deletes[0] != id {
		t.Fatalf("deleted profile left an OS secret: values=%v deletes=%v", backend.values, backend.deletes)
	}
}

func TestFileOwnerUpdatesItsConfigAndPreservesUnrelatedKeys(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
llm:
  provider: openai
  model: before
telegram:
  bot_token: keep-me
`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reader := readFileOwnerTestConfig(t, configPath)
	owner := NewFileOwner(FileOwnerOptions{ConfigPath: configPath, Reader: reader})

	before, err := owner.View(context.Background())
	if err != nil {
		t.Fatalf("View() error = %v", err)
	}
	if before.ReadOnly || !before.ConfigExists || before.LLM.Model != "before" {
		t.Fatalf("unexpected initial view: %#v", before)
	}

	afterModel := "after"
	after, err := owner.Update(context.Background(), AgentSettingsUpdate{
		LLM: LLMSettingsUpdate{LLMConfigFieldsUpdate: LLMConfigFieldsUpdate{Model: &afterModel}},
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if after.LLM.Model != "after" {
		t.Fatalf("updated model = %q, want after", after.LLM.Model)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read updated config: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "model: after") || !strings.Contains(text, "bot_token: keep-me") {
		t.Fatalf("updated config did not preserve expected values:\n%s", text)
	}
}

func TestFileOwnerChangingToCodexPreservesCustomEndpoint(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
llm:
  inference_provider: openai_response_compatible
  provider: openai_resp
  endpoint: https://codex.example.test/api
  model: gpt-5.5
  api_key: test-key
`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	owner := NewFileOwner(FileOwnerOptions{ConfigPath: configPath, Reader: readFileOwnerTestConfig(t, configPath)})
	provider := "openai_codex"
	if _, err := owner.Update(context.Background(), AgentSettingsUpdate{
		LLM: LLMSettingsUpdate{LLMConfigFieldsUpdate: LLMConfigFieldsUpdate{InferenceProvider: &provider}},
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read updated config: %v", err)
	}
	if !strings.Contains(string(raw), "endpoint: https://codex.example.test/api") {
		t.Fatalf("Codex custom endpoint was removed:\n%s", raw)
	}
	if !strings.Contains(string(raw), "api_key: test-key") {
		t.Fatalf("Codex API key was removed:\n%s", raw)
	}
}

func TestFileOwnerCreatesMissingConfigAtResolvedPath(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "nested", "config.yaml")
	reader := viper.New()
	configdefaults.Apply(reader)
	reader.Set("config", configPath)
	owner := NewFileOwner(FileOwnerOptions{Reader: reader})

	model := "created"
	view, err := owner.Update(context.Background(), AgentSettingsUpdate{
		LLM: LLMSettingsUpdate{LLMConfigFieldsUpdate: LLMConfigFieldsUpdate{Model: &model}},
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if view.ConfigPath != configPath || !view.ConfigExists {
		t.Fatalf("updated config metadata = %#v", view)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("created config stat: %v", err)
	}
}

func TestFileOwnerRejectsChangingManagedReference(t *testing.T) {
	t.Setenv("OWNER_TEST_API_KEY", "resolved-secret")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
llm:
  provider: openai
  model: gpt-test
  api_key: ${OWNER_TEST_API_KEY}
`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	owner := NewFileOwner(FileOwnerOptions{ConfigPath: configPath, Reader: readFileOwnerTestConfig(t, configPath)})

	replacement := "literal-secret"
	_, err := owner.Update(context.Background(), AgentSettingsUpdate{
		LLM: LLMSettingsUpdate{LLMConfigFieldsUpdate: LLMConfigFieldsUpdate{APIKey: &replacement}},
	})
	if err == nil {
		t.Fatal("Update() error = nil, want managed-field conflict")
	}
	var statusErr interface{ HTTPStatus() int }
	if !errors.As(err, &statusErr) || statusErr.HTTPStatus() != http.StatusConflict {
		t.Fatalf("Update() error = %v, want HTTP 409", err)
	}
	if !strings.Contains(err.Error(), "api_key") || strings.Contains(err.Error(), "resolved-secret") {
		t.Fatalf("Update() error = %q, want field name without secret", err.Error())
	}

	rawReference := "${OWNER_TEST_API_KEY}"
	if _, err := owner.Update(context.Background(), AgentSettingsUpdate{
		LLM: LLMSettingsUpdate{LLMConfigFieldsUpdate: LLMConfigFieldsUpdate{APIKey: &rawReference}},
	}); err != nil {
		t.Fatalf("Update() preserving raw reference error = %v", err)
	}
}

func TestFileOwnerProtectsManagedProfileReference(t *testing.T) {
	t.Setenv("PROFILE_API_KEY", "profile-secret")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(`llm:
  provider: openai
  model: gpt-main
  profiles:
    backup:
      provider: openai
      model: gpt-backup
      api_key: ${PROFILE_API_KEY}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	owner := NewFileOwner(FileOwnerOptions{ConfigPath: configPath, Reader: readFileOwnerTestConfig(t, configPath)})

	replacement := LLMProfileUpdate{
		OriginalName: "backup",
		LLMProfileSettingsPayload: LLMProfileSettingsPayload{
			Name: "backup",
			LLMConfigFieldsPayload: LLMConfigFieldsPayload{
				Provider: "openai",
				Model:    "gpt-next",
				APIKey:   "replacement-secret",
			},
		},
	}
	_, err := owner.Update(context.Background(), AgentSettingsUpdate{LLM: LLMSettingsUpdate{Profile: &replacement}})
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != http.StatusConflict {
		t.Fatalf("Update() error = %v, want 409 conflict", err)
	}
	if strings.Contains(err.Error(), "profile-secret") || strings.Contains(err.Error(), "replacement-secret") {
		t.Fatalf("conflict error leaked a secret: %v", err)
	}

	replacement.APIKey = "${PROFILE_API_KEY}"
	view, err := owner.Update(context.Background(), AgentSettingsUpdate{LLM: LLMSettingsUpdate{Profile: &replacement}})
	if err != nil {
		t.Fatalf("Update() preserving raw reference error = %v", err)
	}
	if len(view.LLM.Profiles) != 1 || view.LLM.Profiles[0].Model != "gpt-next" {
		t.Fatalf("updated profiles = %+v", view.LLM.Profiles)
	}
	written, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "api_key: ${PROFILE_API_KEY}") {
		t.Fatalf("managed profile reference was not preserved:\n%s", written)
	}
}

func TestFileOwnerRejectsDeletingProfileWithManagedReference(t *testing.T) {
	t.Setenv("PROFILE_API_KEY", "profile-secret")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(`llm:
  provider: openai
  model: gpt-main
  profiles:
    backup:
      provider: openai
      model: gpt-backup
      api_key: ${PROFILE_API_KEY}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	owner := NewFileOwner(FileOwnerOptions{ConfigPath: configPath, Reader: readFileOwnerTestConfig(t, configPath)})
	profile := "backup"

	_, err := owner.Update(context.Background(), AgentSettingsUpdate{LLM: LLMSettingsUpdate{DeleteProfile: &profile}})
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != http.StatusConflict {
		t.Fatalf("Update() error = %v, want 409 conflict", err)
	}
}

func TestFileOwnerUsesReplacedReaderForCurrentView(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("llm:\n  provider: openai\n  model: file-model\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	initial := readFileOwnerTestConfig(t, configPath)
	owner := NewFileOwner(FileOwnerOptions{ConfigPath: configPath, Reader: initial})
	next := readFileOwnerTestConfig(t, configPath)
	next.Set("llm.model", "runtime-model")
	owner.ReplaceReader(next)

	if got := owner.CurrentReader().GetString("llm.model"); got != "runtime-model" {
		t.Fatalf("CurrentReader() model = %q, want runtime-model", got)
	}
}

func TestFileOwnerLoadsCandidateWithoutChangingCurrentReader(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("llm:\n  provider: openai\n  model: old-model\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	owner := NewFileOwner(FileOwnerOptions{ConfigPath: configPath, Reader: readFileOwnerTestConfig(t, configPath)})
	if err := os.WriteFile(configPath, []byte("llm:\n  provider: openai\n  model: new-model\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	candidate, err := owner.LoadCandidate()
	if err != nil {
		t.Fatalf("LoadCandidate() error = %v", err)
	}
	if got := candidate.GetString("llm.model"); got != "new-model" {
		t.Fatalf("candidate model = %q, want new-model", got)
	}
	if got := owner.CurrentReader().GetString("llm.model"); got != "old-model" {
		t.Fatalf("current model changed before ReplaceReader = %q", got)
	}
	if got := owner.ConfigPath(); got != configPath {
		t.Fatalf("ConfigPath() = %q, want %q", got, configPath)
	}
}

func TestFileOwnerViewPreservesRawMCPReferences(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(`llm:
  provider: openai
  model: gpt-test
mcp:
  servers:
    - name: remote
      enable: true
      type: http
      url: https://mcp.example.com/mcp
      headers:
        Authorization: Bearer ${MCP_REMOTE_TOKEN}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	owner := NewFileOwner(FileOwnerOptions{ConfigPath: configPath, Reader: readFileOwnerTestConfig(t, configPath)})

	view, err := owner.View(context.Background())
	if err != nil {
		t.Fatalf("View() error = %v", err)
	}
	if len(view.MCP.Servers) != 1 {
		t.Fatalf("MCP servers = %#v, want one server", view.MCP.Servers)
	}
	server := view.MCP.Servers[0]
	if server.Name != "remote" || server.Type != "http" || !server.Enable {
		t.Fatalf("MCP server = %#v", server)
	}
	if got := server.Headers["Authorization"]; got != "Bearer ${MCP_REMOTE_TOKEN}" {
		t.Fatalf("Authorization header = %q, want raw environment reference", got)
	}
}

func TestFileOwnerUpdateWritesMCPServersAndPreservesOtherConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("custom:\n  keep: true\nllm:\n  provider: openai\n  model: gpt-test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	owner := NewFileOwner(FileOwnerOptions{ConfigPath: configPath, Reader: readFileOwnerTestConfig(t, configPath)})
	servers := []MCPServerSettings{
		{
			Name:         "local",
			Enable:       true,
			Type:         "stdio",
			Command:      "node",
			Args:         []string{"server.js", "--quiet"},
			Env:          map[string]string{"NODE_ENV": "production"},
			AllowedTools: []string{"search", "lookup"},
		},
		{
			Name:    "remote",
			Enable:  false,
			Type:    "http",
			URL:     "https://mcp.example.com/mcp",
			Headers: map[string]string{"Authorization": "Bearer ${MCP_REMOTE_TOKEN}"},
		},
	}

	view, err := owner.Update(context.Background(), AgentSettingsUpdate{
		MCP: &MCPSettingsUpdate{Servers: &servers},
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if len(view.MCP.Servers) != 2 || view.MCP.Servers[1].Enable {
		t.Fatalf("updated MCP view = %#v", view.MCP.Servers)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "keep: true") || !strings.Contains(string(raw), "allowed_tools:") {
		t.Fatalf("updated config lost existing data or MCP fields:\n%s", raw)
	}
	parsed := viper.New()
	parsed.SetConfigType("yaml")
	if err := parsed.ReadConfig(strings.NewReader(string(raw))); err != nil {
		t.Fatal(err)
	}
	if got := parsed.GetString("mcp.servers.0.command"); got != "node" {
		t.Fatalf("mcp.servers.0.command = %q, want node", got)
	}
	if got := parsed.GetString("mcp.servers.1.headers.Authorization"); got != "Bearer ${MCP_REMOTE_TOKEN}" {
		t.Fatalf("remote Authorization header = %q", got)
	}
}

func TestFileOwnerUpdateRejectsInvalidMCPServer(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	original := "llm:\n  provider: openai\n  model: gpt-test\n"
	if err := os.WriteFile(configPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	owner := NewFileOwner(FileOwnerOptions{ConfigPath: configPath, Reader: readFileOwnerTestConfig(t, configPath)})
	servers := []MCPServerSettings{{Name: "broken", Enable: true, Type: "http"}}

	_, err := owner.Update(context.Background(), AgentSettingsUpdate{
		MCP: &MCPSettingsUpdate{Servers: &servers},
	})
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != http.StatusBadRequest {
		t.Fatalf("Update() error = %v, want 400 status error", err)
	}
	raw, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(raw) != original {
		t.Fatalf("invalid update changed config:\n%s", raw)
	}
}

func TestFileOwnerCandidateOnlyReloadsAgentSettings(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	initialConfig := `
file_state_dir: boot-state
max_steps: 11
telegram:
  bot_token: boot-token
mcp:
  servers:
    - name: boot-mcp
      command: boot-command
guard:
  enabled: false
llm:
  provider: openai
  model: old-model
skills:
  enabled: true
  load: [old-skill]
tools:
  bash:
    enabled: false
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	owner := NewFileOwner(FileOwnerOptions{ConfigPath: configPath, Reader: readFileOwnerTestConfig(t, configPath)})

	nextConfig := `
file_state_dir: changed-state
max_steps: 99
telegram:
  bot_token: changed-token
mcp:
  servers:
    - name: changed-mcp
      command: changed-command
guard:
  enabled: true
llm:
  provider: openai
  model: new-model
skills:
  enabled: false
  load: [new-skill]
tools:
  bash:
    enabled: true
`
	if err := os.WriteFile(configPath, []byte(nextConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	candidate, err := owner.LoadCandidate()
	if err != nil {
		t.Fatalf("LoadCandidate() error = %v", err)
	}
	if got := candidate.GetString("llm.model"); got != "new-model" {
		t.Fatalf("candidate llm.model = %q, want new-model", got)
	}
	if got := candidate.GetBool("skills.enabled"); got {
		t.Fatal("candidate skills.enabled = true, want false")
	}
	if got := candidate.GetStringSlice("skills.load"); len(got) != 1 || got[0] != "new-skill" {
		t.Fatalf("candidate skills.load = %#v, want [new-skill]", got)
	}
	if got := candidate.GetBool("tools.bash.enabled"); !got {
		t.Fatal("candidate tools.bash.enabled = false, want true")
	}
	if got := candidate.GetString("file_state_dir"); got != "boot-state" {
		t.Fatalf("candidate file_state_dir = %q, want boot-state", got)
	}
	if got := candidate.GetInt("max_steps"); got != 11 {
		t.Fatalf("candidate max_steps = %d, want 11", got)
	}
	if got := candidate.GetString("telegram.bot_token"); got != "boot-token" {
		t.Fatalf("candidate telegram.bot_token = %q, want boot-token", got)
	}
	if got := candidate.GetString("mcp.servers.0.name"); got != "changed-mcp" {
		t.Fatalf("candidate mcp server = %q, want changed-mcp", got)
	}
	if got := candidate.GetBool("guard.enabled"); got {
		t.Fatal("candidate guard.enabled = true, want false")
	}
}

func TestFileOwnerPersistsSettingsAcrossOwnerRestart(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("llm:\n  provider: openai\n  model: before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	owner := NewFileOwner(FileOwnerOptions{ConfigPath: configPath, Reader: readFileOwnerTestConfig(t, configPath)})
	model := "after-restart"
	if _, err := owner.Update(context.Background(), AgentSettingsUpdate{
		LLM: LLMSettingsUpdate{LLMConfigFieldsUpdate: LLMConfigFieldsUpdate{Model: &model}},
	}); err != nil {
		t.Fatal(err)
	}

	restarted := NewFileOwner(FileOwnerOptions{ConfigPath: configPath, Reader: readFileOwnerTestConfig(t, configPath)})
	view, err := restarted.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.LLM.Model != "after-restart" {
		t.Fatalf("model after owner restart = %q", view.LLM.Model)
	}
}

func TestFileOwnerRejectsStaleConfigRevision(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	initial := "# keep this comment\nllm:\n  provider: openai\n  model: before\n"
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	owner := NewFileOwner(FileOwnerOptions{ConfigPath: configPath, Reader: readFileOwnerTestConfig(t, configPath)})
	view, err := owner.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.ConfigRevision == "" {
		t.Fatal("config revision is empty")
	}

	external := initial + "user_agent: external-edit\n"
	if err := os.WriteFile(configPath, []byte(external), 0o600); err != nil {
		t.Fatal(err)
	}
	model := "after"
	_, err = owner.Update(context.Background(), AgentSettingsUpdate{
		ConfigRevision: view.ConfigRevision,
		LLM:            LLMSettingsUpdate{LLMConfigFieldsUpdate: LLMConfigFieldsUpdate{Model: &model}},
	})
	if err == nil {
		t.Fatal("Update() error = nil, want revision conflict")
	}
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.HTTPStatus() != http.StatusConflict {
		t.Fatalf("Update() error = %v, want HTTP 409", err)
	}
	raw, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(raw) != external {
		t.Fatalf("stale update changed config:\n%s", raw)
	}
}

func TestFileOwnerReadsAndUpdatesAdditionalAgentConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	initial := "# keep\nllm:\n  provider: openai\n  model: test\nmax_steps: 8\ncontext_compaction:\n  enabled: false\nunknown: value\n"
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	owner := NewFileOwner(FileOwnerOptions{ConfigPath: configPath, Reader: readFileOwnerTestConfig(t, configPath)})
	view, err := owner.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.ConfigValues["max_steps"] != 8 || view.FieldStates["context_compaction.enabled"].Explicit != true {
		t.Fatalf("additional config view = %#v states=%#v", view.ConfigValues, view.FieldStates)
	}

	view, err = owner.Update(context.Background(), AgentSettingsUpdate{
		ConfigRevision: view.ConfigRevision,
		ConfigChanges: map[string]json.RawMessage{
			"max_steps": json.RawMessage("12"),
		},
		Reset: []string{"context_compaction.enabled"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.ConfigValues["max_steps"] != 12 || view.FieldStates["context_compaction.enabled"].Explicit {
		t.Fatalf("updated view = %#v states=%#v", view.ConfigValues, view.FieldStates)
	}
	if view.ApplyMode != configsettings.ApplyNextGeneration || view.ApplyStatus != "pending" {
		t.Fatalf("apply result = mode %q status %q", view.ApplyMode, view.ApplyStatus)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	if !strings.Contains(out, "# keep") || !strings.Contains(out, "unknown: value") || strings.Contains(out, "context_compaction:") {
		t.Fatalf("updated config did not preserve unrelated YAML:\n%s", out)
	}
}

func TestFileOwnerProtectsAdditionalAgentSecret(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("llm:\n  provider: openai\n  model: test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := &fakeFileOwnerSecretBackend{}
	owner := NewFileOwner(FileOwnerOptions{
		ConfigPath:   configPath,
		Reader:       readFileOwnerTestConfigWithSource(t, configPath, backend),
		SecretSource: backend,
		OSStore:      backend,
	})
	view, err := owner.Update(context.Background(), AgentSettingsUpdate{
		ConfigChanges: map[string]json.RawMessage{
			"llm.image.api_key": json.RawMessage(`"image-secret"`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(backend.puts) != 1 || backend.labels[backend.puts[0]] != "llm.image.api_key" {
		t.Fatalf("stored secret puts=%v labels=%v", backend.puts, backend.labels)
	}
	if state := view.FieldStates["llm.image.api_key"]; !state.Configured || state.Source != "config_os_ref" {
		t.Fatalf("secret field state = %#v", state)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "image-secret") || !strings.Contains(string(raw), secref.OSSecretRef(backend.puts[0])) {
		t.Fatalf("secret was not protected:\n%s", raw)
	}
}

func TestFileOwnerRejectsInvalidACPAgentConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	initial := "llm:\n  provider: openai\n  model: test\n"
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	owner := NewFileOwner(FileOwnerOptions{ConfigPath: configPath, Reader: readFileOwnerTestConfig(t, configPath)})
	_, err := owner.Update(context.Background(), AgentSettingsUpdate{
		ConfigChanges: map[string]json.RawMessage{
			"acp.agents": json.RawMessage(`[{"name":"codex"}]`),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "command is required") {
		t.Fatalf("Update() error = %v, want invalid ACP command", err)
	}
	raw, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(raw) != initial {
		t.Fatalf("invalid update changed config:\n%s", raw)
	}
}

func TestFileOwnerReturnsConfigPathIOError(t *testing.T) {
	root := t.TempDir()
	blockedParent := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blockedParent, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(blockedParent, "config.yaml")
	reader := viper.New()
	configdefaults.Apply(reader)
	reader.Set("config", configPath)
	owner := NewFileOwner(FileOwnerOptions{ConfigPath: configPath, Reader: reader})
	model := "unwritable"
	_, err := owner.Update(context.Background(), AgentSettingsUpdate{
		LLM: LLMSettingsUpdate{LLMConfigFieldsUpdate: LLMConfigFieldsUpdate{Model: &model}},
	})
	if err == nil {
		t.Fatal("Update() error = nil, want config path I/O error")
	}
	if !strings.Contains(err.Error(), blockedParent) {
		t.Fatalf("Update() error = %q, want failing config path", err)
	}
}

func readFileOwnerTestConfig(t *testing.T, configPath string) *viper.Viper {
	t.Helper()
	reader := viper.New()
	configdefaults.Apply(reader)
	if err := configutil.ReadExpandedConfig(reader, configPath, nil); err != nil {
		t.Fatalf("read expanded config: %v", err)
	}
	reader.Set("config", configPath)
	return reader
}

func readFileOwnerTestConfigWithSource(t *testing.T, configPath string, source secref.Source) *viper.Viper {
	t.Helper()
	reader := viper.New()
	configdefaults.Apply(reader)
	if err := configutil.ReadExpandedConfigWithSource(reader, configPath, source, nil); err != nil {
		t.Fatalf("read expanded config with source: %v", err)
	}
	reader.Set("config", configPath)
	return reader
}
