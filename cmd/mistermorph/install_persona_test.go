package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptInstallSoulPresetShowsDescriptionsAndDefaults(t *testing.T) {
	var out bytes.Buffer
	choice, err := promptInstallSoulPreset(bufio.NewReader(strings.NewReader("\n1\n")), &out)
	if err != nil {
		t.Fatalf("promptInstallSoulPreset() error = %v", err)
	}
	if choice != "research_scholar" {
		t.Fatalf("promptInstallSoulPreset() = %q, want research_scholar", choice)
	}
	rendered := out.String()
	for _, want := range []string{
		"Select soul.md style:",
		"Choice is required.",
		"1. Research Scholar",
		"INTJ mind, rigorous logic, first-principles, real intellectual passion.",
		"5. Customize",
		"Open soul.md in your system editor and write your own.",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("prompt output missing %q:\n%s", want, rendered)
		}
	}
}

func TestRunInstallSoulSetupAppliesPresetChoice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SOUL.md")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	var out bytes.Buffer
	if err := runInstallSoulSetup(strings.NewReader("1\n"), &out, path); err != nil {
		t.Fatalf("runInstallSoulSetup() error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read SOUL.md: %v", err)
	}
	want, err := loadEmbeddedSoulTemplateByID("research_scholar")
	if err != nil {
		t.Fatalf("loadEmbeddedSoulTemplateByID(): %v", err)
	}
	if string(got) != want {
		t.Fatalf("SOUL.md not replaced by preset")
	}
}

func TestRunInstallIdentitySetupUsesDefaultsWithoutConfirmation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.yaml")
	body := buildInstallIdentityYAML(installIdentityProfile{
		Name:     "Morph",
		Creature: "Fox",
		Vibe:     "Sharp",
		Emoji:    "🦊",
	}) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	var out bytes.Buffer
	if err := runInstallIdentitySetup(strings.NewReader("\n\n\n\n"), &out, path); err != nil {
		t.Fatalf("runInstallIdentitySetup() error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read identity.yaml: %v", err)
	}
	if string(got) != body {
		t.Fatalf("identity defaults should preserve existing values")
	}
	rendered := out.String()
	for _, want := range []string{"Identity name", "Identity creature", "Identity vibe", "Identity emoji"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("identity prompt missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "Customize IDENTITY.md now?") {
		t.Fatalf("identity prompt should not ask for confirmation:\n%s", rendered)
	}
}

func TestRunInstallSoulSetupCustomUsesSystemEditor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SOUL.md")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	prev := installSystemEditorOpener
	t.Cleanup(func() {
		installSystemEditorOpener = prev
	})
	called := ""
	installSystemEditorOpener = func(nextPath string) error {
		called = nextPath
		return nil
	}
	var out bytes.Buffer
	if err := runInstallSoulSetup(strings.NewReader("customize\n"), &out, path); err != nil {
		t.Fatalf("runInstallSoulSetup() error = %v", err)
	}
	if called != path {
		t.Fatalf("installSystemEditorOpener called with %q, want %q", called, path)
	}
	if !strings.Contains(out.String(), "Opening system editor for "+path) {
		t.Fatalf("expected editor notice in output: %s", out.String())
	}
}
