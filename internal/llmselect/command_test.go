package llmselect

import (
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/llmutil"
)

func TestExecuteCommandText_SetAndReset(t *testing.T) {
	values := llmutil.RuntimeValues{
		Provider: "openai",
		Model:    "gpt-5.2",
		Profiles: map[string]llmutil.ProfileConfig{
			"cheap": {Model: "gpt-4.1-mini"},
		},
		Routes: llmutil.RoutesConfig{
			PurposeRoutes: llmutil.PurposeRoutes{
				MainLoop: llmutil.RoutePolicyConfig{
					Candidates: []llmutil.RouteCandidateConfig{
						{Profile: llmutil.RouteProfileDefault, Weight: 80},
						{Profile: "cheap", Weight: 20},
					},
				},
			},
		},
	}
	store := NewStore()

	currentText, handled, err := ExecuteCommandText(values, store, "/models")
	if err != nil {
		t.Fatalf("ExecuteCommandText(/models) error = %v", err)
	}
	if !handled {
		t.Fatal("expected /models to be handled")
	}
	if !strings.Contains(currentText, "Current LLM selection: auto") {
		t.Fatalf("current text = %q, want auto selection", currentText)
	}
	if !strings.Contains(currentText, "weighted candidates") {
		t.Fatalf("current text = %q, want weighted candidates", currentText)
	}

	setText, handled, err := ExecuteCommandText(values, store, "/models set cheap")
	if err != nil {
		t.Fatalf("ExecuteCommandText(/models set) error = %v", err)
	}
	if !handled {
		t.Fatal("expected /models set to be handled")
	}
	if !strings.Contains(setText, "cheap") {
		t.Fatalf("set text = %q, want cheap profile", setText)
	}
	if got := store.Get(); got.Mode != ModeManual || got.ManualProfile != "cheap" {
		t.Fatalf("store.Get() = %#v, want manual cheap", got)
	}

	resetText, handled, err := ExecuteCommandText(values, store, "/models reset")
	if err != nil {
		t.Fatalf("ExecuteCommandText(/models reset) error = %v", err)
	}
	if !handled {
		t.Fatal("expected /models reset to be handled")
	}
	if !strings.Contains(resetText, "Current LLM selection: auto") {
		t.Fatalf("reset text = %q, want auto selection", resetText)
	}
	if got := store.Get(); got.Mode != ModeAuto {
		t.Fatalf("store.Get().Mode = %q, want auto", got.Mode)
	}
}

func TestExecuteCommandText_InvalidUsageHandled(t *testing.T) {
	values := llmutil.RuntimeValues{Provider: "openai", Model: "gpt-5.2"}
	_, handled, err := ExecuteCommandText(values, NewStore(), "/models set")
	if !handled {
		t.Fatal("expected invalid /models command to be handled")
	}
	if err == nil {
		t.Fatal("expected usage error")
	}
	if !strings.Contains(err.Error(), "/models set <profile_name>") {
		t.Fatalf("error = %q, want usage text", err.Error())
	}
}

func TestExecuteCommandText_SingularModelCommandIsNotHandled(t *testing.T) {
	values := llmutil.RuntimeValues{Provider: "openai", Model: "gpt-5.2"}
	_, handled, err := ExecuteCommandText(values, NewStore(), "/model")
	if err != nil {
		t.Fatalf("ExecuteCommandText(/model) error = %v", err)
	}
	if handled {
		t.Fatal("expected /model to be ignored")
	}
}

func TestParseCommandNormalizesChatSyntax(t *testing.T) {
	cmd, handled, err := ParseCommand("  <@U123>   /models@MissMorphBot   set   cheap  ")
	if err != nil {
		t.Fatalf("ParseCommand() error = %v", err)
	}
	if !handled {
		t.Fatal("ParseCommand() handled = false")
	}
	if cmd.Action != CommandSet || cmd.ProfileName != "cheap" {
		t.Fatalf("ParseCommand() = %#v, want set cheap", cmd)
	}
}
