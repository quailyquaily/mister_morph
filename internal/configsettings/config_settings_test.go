package configsettings

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/secref"
)

var testFields = []Field{
	{Path: "max_steps", Kind: KindInt, ApplyMode: ApplyNextGeneration, Min: number(1)},
	{Path: "timeout", Kind: KindDuration, ApplyMode: ApplyNextGeneration},
	{Path: "live.enabled", Kind: KindBool, ApplyMode: ApplyImmediate},
	{Path: "logging.level", Kind: KindString, ApplyMode: ApplyProcessRestart, Enum: []string{"debug", "info", "warn", "error"}},
	{Path: "logging.redact_keys", Kind: KindStringList, ApplyMode: ApplyProcessRestart},
	{Path: "server.auth_token", Kind: KindString, Sensitive: true, ApplyMode: ApplyRuntimeRestart},
}

func TestViewReportsDefaultsExplicitValuesAndSecretState(t *testing.T) {
	raw := []byte("timeout: 45s\nlive:\n  enabled: true\nserver:\n  auth_token: ${TOKEN}\n")
	view, err := View(raw, testFields)
	if err != nil {
		t.Fatal(err)
	}
	if got := view.Values["max_steps"]; got != 1024 {
		t.Fatalf("default max_steps = %#v, want 1024", got)
	}
	if got := view.Values["timeout"]; got != "45s" {
		t.Fatalf("timeout = %#v, want 45s", got)
	}
	if got := view.Values["live.enabled"]; got != true {
		t.Fatalf("live.enabled = %#v, want true", got)
	}
	if got := view.Values["server.auth_token"]; got != "" {
		t.Fatalf("secret value leaked: %#v", got)
	}
	if state := view.FieldStates["max_steps"]; state.Source != SourceDefault || state.Explicit {
		t.Fatalf("max_steps state = %#v", state)
	}
	if state := view.FieldStates["server.auth_token"]; state.Source != SourceConfigEnvRef || state.EnvName != "TOKEN" || !state.Configured || !state.Sensitive {
		t.Fatalf("auth token state = %#v", state)
	}
}

func TestViewReportsEnvironmentOverride(t *testing.T) {
	t.Setenv("MISTER_MORPH_MAX_STEPS", "12")
	view, err := View([]byte("max_steps: 8\n"), testFields)
	if err != nil {
		t.Fatal(err)
	}
	if got := view.Values["max_steps"]; got != 12 {
		t.Fatalf("effective max_steps = %#v, want 12", got)
	}
	state := view.FieldStates["max_steps"]
	if state.Source != SourceEnvironmentOverride || state.Editable || state.EnvName != "MISTER_MORPH_MAX_STEPS" || !state.Explicit {
		t.Fatalf("max_steps state = %#v", state)
	}
}

func TestApplyPreservesUntouchedYAMLAndSupportsReset(t *testing.T) {
	raw := []byte("# heading\nmax_steps: 8 # keep\ntimeout: 30s\nunknown:\n  nested: value\n")
	next, err := Apply(raw, Update{
		Changes: map[string]json.RawMessage{"max_steps": json.RawMessage("12")},
		Reset:   []string{"timeout"},
	}, testFields)
	if err != nil {
		t.Fatal(err)
	}
	out := string(next)
	for _, want := range []string{"# heading", "max_steps: 12 # keep", "unknown:", "nested: value"} {
		if !strings.Contains(out, want) {
			t.Fatalf("updated YAML missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "timeout:") {
		t.Fatalf("reset field remains in YAML:\n%s", out)
	}
}

func TestApplyRejectsUnknownAndInvalidFields(t *testing.T) {
	tests := []struct {
		name   string
		update Update
	}{
		{name: "unknown", update: Update{Changes: map[string]json.RawMessage{"not_allowed": json.RawMessage("true")}}},
		{name: "wrong type", update: Update{Changes: map[string]json.RawMessage{"max_steps": json.RawMessage(`"many"`)}}},
		{name: "below minimum", update: Update{Changes: map[string]json.RawMessage{"max_steps": json.RawMessage("0")}}},
		{name: "invalid duration", update: Update{Changes: map[string]json.RawMessage{"timeout": json.RawMessage(`"later"`)}}},
		{name: "invalid enum", update: Update{Changes: map[string]json.RawMessage{"logging.level": json.RawMessage(`"verbose"`)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Apply(nil, tt.update, testFields); err == nil {
				t.Fatal("Apply() error = nil")
			}
		})
	}
}

func TestApplyUsesEnvironmentOverrideAsReadOnly(t *testing.T) {
	t.Setenv("MISTER_MORPH_MAX_STEPS", "12")
	_, err := Apply([]byte("max_steps: 8\n"), Update{
		Changes: map[string]json.RawMessage{"max_steps": json.RawMessage("10")},
	}, testFields)
	if err == nil || !strings.Contains(err.Error(), "MISTER_MORPH_MAX_STEPS") {
		t.Fatalf("Apply() error = %v, want environment override conflict", err)
	}
}

func TestReadFileReturnsRevisionForView(t *testing.T) {
	path := t.TempDir() + "/config.yaml"
	if err := os.WriteFile(path, []byte("max_steps: 8\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	view, err := ReadFile(path, testFields)
	if err != nil {
		t.Fatal(err)
	}
	if view.ConfigRevision == "" || view.Values["max_steps"] != 8 {
		t.Fatalf("view = %#v", view)
	}
}

type configSettingsSecretStore struct {
	values   map[string]string
	labels   map[string]string
	deletes  []string
	putCount int
	failAt   int
}

func (s *configSettingsSecretStore) Get(context.Context, string) ([]byte, error) {
	return nil, secref.ErrOSSecretNotFound
}

func (s *configSettingsSecretStore) Put(_ context.Context, id, label string, value []byte) error {
	s.putCount++
	if s.putCount == s.failAt {
		return secref.ErrOSStoreUnavailable
	}
	if s.values == nil {
		s.values = map[string]string{}
		s.labels = map[string]string{}
	}
	s.values[id] = string(value)
	s.labels[id] = label
	return nil
}

func (s *configSettingsSecretStore) Delete(_ context.Context, id string) error {
	delete(s.values, id)
	s.deletes = append(s.deletes, id)
	return nil
}

func TestProtectSecretsStoresScalarSecretChanges(t *testing.T) {
	store := &configSettingsSecretStore{}
	update := Update{Changes: map[string]json.RawMessage{
		"server.auth_token": json.RawMessage(`"server-token"`),
	}}
	ids, err := ProtectSecrets(context.Background(), &update, testFields, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || store.values[ids[0]] != "server-token" || store.labels[ids[0]] != "server.auth_token" {
		t.Fatalf("stored secrets ids=%v values=%v labels=%v", ids, store.values, store.labels)
	}
	var stored string
	if err := json.Unmarshal(update.Changes["server.auth_token"], &stored); err != nil {
		t.Fatal(err)
	}
	if stored != secref.OSSecretRef(ids[0]) {
		t.Fatalf("protected value = %q", stored)
	}
}

func TestProtectSecretsRollsBackWithoutMutatingChanges(t *testing.T) {
	fields := append(append([]Field(nil), testFields...), Field{
		Path: "telegram.bot_token", Kind: KindString, Sensitive: true, ApplyMode: ApplyRuntimeRestart,
	})
	store := &configSettingsSecretStore{failAt: 2}
	update := Update{Changes: map[string]json.RawMessage{
		"server.auth_token":  json.RawMessage(`"server-token"`),
		"telegram.bot_token": json.RawMessage(`"telegram-token"`),
	}}
	_, err := ProtectSecrets(context.Background(), &update, fields, store)
	if !errors.Is(err, secref.ErrOSStoreUnavailable) {
		t.Fatalf("ProtectSecrets() error = %v", err)
	}
	if len(store.values) != 0 || len(store.deletes) != 1 {
		t.Fatalf("rollback values=%v deletes=%v", store.values, store.deletes)
	}
	if string(update.Changes["server.auth_token"]) != `"server-token"` || string(update.Changes["telegram.bot_token"]) != `"telegram-token"` {
		t.Fatalf("failed protection mutated update: %#v", update.Changes)
	}
}

func TestApplyResultUsesStrongestChangedField(t *testing.T) {
	result := ResultForUpdate(Update{
		Changes: map[string]json.RawMessage{
			"live.enabled":      json.RawMessage(`true`),
			"server.auth_token": json.RawMessage(`"token"`),
		},
		Reset: []string{"timeout"},
	}, testFields, []string{"telegram"})

	if result.ApplyMode != ApplyRuntimeRestart {
		t.Fatalf("apply mode = %q, want %q", result.ApplyMode, ApplyRuntimeRestart)
	}
	if result.ApplyStatus != "pending" {
		t.Fatalf("apply status = %q, want pending", result.ApplyStatus)
	}
	if !reflect.DeepEqual(result.RestartTargets, []string{"telegram"}) {
		t.Fatalf("restart targets = %#v", result.RestartTargets)
	}
}

func TestApplyResultAllowsAdditionalMode(t *testing.T) {
	result := ResultForUpdate(Update{}, testFields, nil, ApplyNextGeneration)
	if result.ApplyMode != ApplyNextGeneration || result.ApplyStatus != "pending" {
		t.Fatalf("result = %#v", result)
	}

	immediate := ResultForUpdate(Update{
		Changes: map[string]json.RawMessage{"live.enabled": json.RawMessage(`true`)},
	}, testFields, nil)
	if immediate.ApplyMode != ApplyImmediate || immediate.ApplyStatus != "applied" {
		t.Fatalf("immediate result = %#v", immediate)
	}
}

func TestApplyRuntimeOverridesMarksFieldsReadOnly(t *testing.T) {
	view, err := View([]byte("logging:\n  level: info\n"), testFields)
	if err != nil {
		t.Fatal(err)
	}
	ApplyRuntimeOverrides(&view, map[string]any{"logging.level": "debug", "unknown": "ignored"})

	if view.Values["logging.level"] != "debug" {
		t.Fatalf("override value = %#v", view.Values["logging.level"])
	}
	state := view.FieldStates["logging.level"]
	if state.Source != SourceRuntimeOverride || state.Editable {
		t.Fatalf("override state = %#v", state)
	}
}

func TestRejectRuntimeOverrideUpdate(t *testing.T) {
	err := RejectRuntimeOverrideUpdate(Update{
		Changes: map[string]json.RawMessage{"logging.level": json.RawMessage(`"warn"`)},
	}, map[string]any{"logging.level": "debug"})
	if err == nil || !strings.Contains(err.Error(), "command-line flag") {
		t.Fatalf("error = %v, want runtime override conflict", err)
	}
}

func number(value float64) *float64 { return &value }
