package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/assets"
)

const (
	setupProviderOpenAICompatible = "openai_compatible"
	setupProviderGemini           = "gemini"
	setupProviderAnthropic        = "anthropic"
	setupProviderCloudflare       = "cloudflare"
)

type setupProviderOption struct {
	Choice          string
	DefaultEndpoint string
}

type embeddedSoulPreset struct {
	ID             string `json:"id"`
	TemplatePath   string `json:"template_path"`
	CLITitle       string `json:"cli_title"`
	CLIDescription string `json:"cli_description"`
	WebTitleKey    string `json:"web_title_key"`
	WebNoteKey     string `json:"web_note_key"`
}

var installSetupProviderOptions = []setupProviderOption{
	{Choice: setupProviderOpenAICompatible, DefaultEndpoint: "https://api.openai.com"},
	{Choice: setupProviderGemini, DefaultEndpoint: "https://generativelanguage.googleapis.com"},
	{Choice: setupProviderAnthropic, DefaultEndpoint: "https://api.anthropic.com"},
	{Choice: setupProviderCloudflare, DefaultEndpoint: "https://api.cloudflare.com/client/v4"},
}

func setupProviderChoices() []string {
	out := make([]string, 0, len(installSetupProviderOptions))
	for _, item := range installSetupProviderOptions {
		out = append(out, item.Choice)
	}
	return out
}

func defaultEndpointForSetupProvider(choice string) string {
	choice = strings.ToLower(strings.TrimSpace(choice))
	for _, item := range installSetupProviderOptions {
		if item.Choice == choice {
			return item.DefaultEndpoint
		}
	}
	return installSetupProviderOptions[0].DefaultEndpoint
}

func normalizeConfigProviderForSetup(choice string) string {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case setupProviderGemini:
		return setupProviderGemini
	case setupProviderAnthropic:
		return setupProviderAnthropic
	case setupProviderCloudflare:
		return setupProviderCloudflare
	default:
		return "openai"
	}
}

func loadEmbeddedSoulPresets() ([]embeddedSoulPreset, error) {
	body, err := assets.ConfigFS.ReadFile("config/souls/presets.json")
	if err != nil {
		return nil, err
	}
	var presets []embeddedSoulPreset
	if err := json.Unmarshal(body, &presets); err != nil {
		return nil, err
	}
	out := make([]embeddedSoulPreset, 0, len(presets))
	for _, preset := range presets {
		preset.ID = strings.TrimSpace(preset.ID)
		preset.TemplatePath = strings.TrimSpace(preset.TemplatePath)
		preset.CLITitle = strings.TrimSpace(preset.CLITitle)
		preset.CLIDescription = strings.TrimSpace(preset.CLIDescription)
		preset.WebTitleKey = strings.TrimSpace(preset.WebTitleKey)
		preset.WebNoteKey = strings.TrimSpace(preset.WebNoteKey)
		if preset.ID == "" || preset.TemplatePath == "" {
			return nil, fmt.Errorf("invalid soul preset manifest entry: missing id or template_path")
		}
		out = append(out, preset)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no soul presets configured")
	}
	return out, nil
}

func loadInstallSoulPresetOptions() ([]installSoulPresetOption, error) {
	presets, err := loadEmbeddedSoulPresets()
	if err != nil {
		return nil, err
	}
	options := make([]installSoulPresetOption, 0, len(presets)+1)
	for _, preset := range presets {
		options = append(options, installSoulPresetOption{
			Choice:      preset.ID,
			Title:       preset.CLITitle,
			Description: preset.CLIDescription,
		})
	}
	options = append(options, installSoulPresetOption{
		Choice:      "custom",
		Title:       "Customize",
		Description: "Open soul.md in your system editor and write your own.",
	})
	return options, nil
}

func loadEmbeddedSoulTemplateByID(id string) (string, error) {
	presets, err := loadEmbeddedSoulPresets()
	if err != nil {
		return "", err
	}
	needle := strings.ToLower(strings.TrimSpace(id))
	for _, preset := range presets {
		if strings.ToLower(strings.TrimSpace(preset.ID)) != needle {
			continue
		}
		body, err := assets.ConfigFS.ReadFile(preset.TemplatePath)
		if err != nil {
			return "", err
		}
		return string(body), nil
	}
	return "", fmt.Errorf("unknown soul preset %q", id)
}

func openInstallSystemEditor(path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return fmt.Errorf("editor path is required")
	}
	editor := strings.TrimSpace(os.Getenv("VISUAL"))
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if editor == "" {
		editor = "vi"
	}
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return fmt.Errorf("invalid editor command")
	}
	args := append(parts[1:], path)
	cmd := exec.Command(parts[0], args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
