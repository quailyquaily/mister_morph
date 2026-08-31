package agentsettings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/configdefaults"
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
	if got := candidate.GetString("mcp.servers.0.name"); got != "boot-mcp" {
		t.Fatalf("candidate mcp server = %q, want boot-mcp", got)
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
