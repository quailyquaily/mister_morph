package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var installIdentityYAMLFenceRE = regexp.MustCompile("(?is)```(?:yaml|yml)\\s*\\n([\\s\\S]*?)\\n```")
var installIdentityYAMLLineRE = regexp.MustCompile(`^\s*(name|creature|vibe|emoji)\s*:\s*(.*)\s*$`)

type installIdentityProfile struct {
	Name     string
	Creature string
	Vibe     string
	Emoji    string
}

type installSoulPresetOption struct {
	Choice      string
	Title       string
	Description string
}

var installSystemEditorOpener = openInstallSystemEditor

func runInstallIdentitySetup(in io.Reader, out io.Writer, path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	reader := bufio.NewReader(in)
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	defaults := parseInstallIdentityProfile(string(raw))
	next := defaults
	if next.Name, err = promptLineWithDefault(reader, out, "Identity name", defaults.Name); err != nil {
		return err
	}
	if next.Creature, err = promptLineWithDefault(reader, out, "Identity creature", defaults.Creature); err != nil {
		return err
	}
	if next.Vibe, err = promptLineWithDefault(reader, out, "Identity vibe", defaults.Vibe); err != nil {
		return err
	}
	if next.Emoji, err = promptLineWithDefault(reader, out, "Identity emoji", defaults.Emoji); err != nil {
		return err
	}
	return writeInstallFilePreserveMode(path, []byte(buildInstallIdentityYAML(next)+"\n"))
}

func runInstallSoulSetup(in io.Reader, out io.Writer, path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	reader := bufio.NewReader(in)
	choice, err := promptInstallSoulPreset(reader, out)
	if err != nil {
		return err
	}
	if choice == "custom" {
		fmt.Fprintf(out, "Opening system editor for %s\n", path)
		return installSystemEditorOpener(path)
	}
	body, err := loadEmbeddedSoulTemplateByID(choice)
	if err != nil {
		return err
	}
	return writeInstallFilePreserveMode(path, []byte(body))
}

func promptInstallSoulPreset(reader *bufio.Reader, out io.Writer) (string, error) {
	options, err := loadInstallSoulPresetOptions()
	if err != nil {
		return "", err
	}
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Select soul.md style:")
	for i, option := range options {
		fmt.Fprintf(out, "  %d. %s\n", i+1, option.Title)
		fmt.Fprintf(out, "     %s\n", option.Description)
	}
	for {
		fmt.Fprint(out, "Choice: ")
		raw, err := readTrimmedLine(reader)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(raw) == "" {
			fmt.Fprintln(out, "Choice is required.")
			continue
		}
		if idx, err := parseInstallSoulPresetIndex(raw); err == nil {
			if idx >= 1 && idx <= len(options) {
				return options[idx-1].Choice, nil
			}
		}
		if choice, ok := matchInstallSoulPresetChoice(options, raw); ok {
			return choice, nil
		}
		fmt.Fprintf(out, "Invalid choice %q. Use a preset number or one of: %s\n", raw, installSoulPresetChoiceList(options))
	}
}

func parseInstallSoulPresetIndex(raw string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(raw))
}

func matchInstallSoulPresetChoice(options []installSoulPresetOption, raw string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return "", false
	}
	for _, option := range options {
		choice := strings.ToLower(strings.TrimSpace(option.Choice))
		title := strings.ToLower(strings.TrimSpace(option.Title))
		if value == choice || value == title {
			return option.Choice, true
		}
		if option.Choice == "custom" && value == "customize" {
			return option.Choice, true
		}
	}
	return "", false
}

func installSoulPresetChoiceList(options []installSoulPresetOption) string {
	choices := make([]string, 0, len(options))
	for _, option := range options {
		choices = append(choices, option.Choice)
	}
	return strings.Join(choices, "/")
}

func writeInstallFilePreserveMode(path string, body []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return os.WriteFile(path, body, mode)
}

func parseInstallIdentityProfile(raw string) installIdentityProfile {
	profile := installIdentityProfile{}
	content := strings.ReplaceAll(raw, "\r\n", "\n")
	match := installIdentityYAMLFenceRE.FindStringSubmatch(content)
	if len(match) >= 2 {
		content = match[1]
	}
	for _, line := range strings.Split(content, "\n") {
		lineMatch := installIdentityYAMLLineRE.FindStringSubmatch(line)
		if len(lineMatch) != 3 {
			continue
		}
		value := parseInstallYAMLScalar(lineMatch[2])
		switch lineMatch[1] {
		case "name":
			profile.Name = value
		case "creature":
			profile.Creature = value
		case "vibe":
			profile.Vibe = value
		case "emoji":
			profile.Emoji = value
		}
	}
	return profile
}

func parseInstallYAMLScalar(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if len(value) >= 2 && ((strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) || (strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`))) {
		value = value[1 : len(value)-1]
	}
	return value
}

func installYAMLString(value string) string {
	return fmt.Sprintf("%q", strings.TrimSpace(value))
}

func buildInstallIdentityYAML(values installIdentityProfile) string {
	return strings.Join([]string{
		"name: " + installYAMLString(values.Name),
		"name_alts: []",
		"creature: " + installYAMLString(values.Creature),
		"vibe: " + installYAMLString(values.Vibe),
		"emoji: " + installYAMLString(values.Emoji),
	}, "\n")
}
