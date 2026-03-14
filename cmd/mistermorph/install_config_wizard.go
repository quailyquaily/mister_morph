package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

type installConfigSetup struct {
	Provider string
	Endpoint string
	Model    string

	APIKey             string
	CloudflareAccount  string
	CloudflareAPIToken string

	TelegramBotToken         string
	TelegramGroupTriggerMode string

	ConfigureSlack    bool
	SlackBotToken     string
	SlackAppToken     string
	SlackGroupTrigger string
}

func findReadableInstallConfig(cmd *cobra.Command, installDir string) (string, bool) {
	candidates := make([]string, 0, 3)

	cfgFlagPath := ""
	if cmd != nil && cmd.Flags() != nil {
		if v, err := cmd.Flags().GetString("config"); err == nil {
			cfgFlagPath = strings.TrimSpace(v)
		}
	}
	if cfgFlagPath == "" {
		cfgFlagPath = strings.TrimSpace(viper.GetString("config"))
	}
	if cfgFlagPath != "" {
		candidates = append(candidates, pathutil.ExpandHomePath(cfgFlagPath))
	}
	candidates = append(candidates, filepath.Join(installDir, "config.yaml"))
	candidates = append(candidates, filepath.Join(pathutil.ExpandHomePath("~/.morph"), "config.yaml"))

	seen := map[string]bool{}
	for _, p := range candidates {
		p = filepath.Clean(strings.TrimSpace(p))
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		if _, err := os.ReadFile(p); err == nil {
			return p, true
		}
	}
	return "", false
}

func maybeCollectInstallConfigSetup(cmd *cobra.Command, skipPrompts bool) (*installConfigSetup, error) {
	if skipPrompts {
		return nil, nil
	}
	if !supportsInteractivePrompts(cmd) {
		fmt.Fprintln(cmd.ErrOrStderr(), "warn: no config.yaml found; non-interactive mode detected, using default config template")
		return nil, nil
	}
	return runInstallConfigSetupWizard(cmd.InOrStdin(), cmd.OutOrStdout())
}

func supportsInteractivePrompts(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	inFile, okIn := cmd.InOrStdin().(*os.File)
	outFile, okOut := cmd.OutOrStdout().(*os.File)
	if !okIn || !okOut {
		return false
	}
	return term.IsTerminal(int(inFile.Fd())) && term.IsTerminal(int(outFile.Fd()))
}

func runInstallConfigSetupWizard(in io.Reader, out io.Writer) (*installConfigSetup, error) {
	reader := bufio.NewReader(in)

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "No readable config.yaml found. Starting interactive config setup.")

	provider, err := promptChoice(reader, out, "Select llm provider", []string{"openai", "gemini", "cloudflare"}, "openai")
	if err != nil {
		return nil, err
	}
	endpointDefault := defaultEndpointForProvider(provider)
	endpoint, err := promptLineWithDefault(reader, out, "LLM endpoint", endpointDefault)
	if err != nil {
		return nil, err
	}

	setup := &installConfigSetup{
		Provider: provider,
		Endpoint: endpoint,
	}

	switch provider {
	case "openai", "gemini":
		setup.APIKey, err = promptRequiredLine(reader, out, "LLM api_key")
		if err != nil {
			return nil, err
		}
	case "cloudflare":
		setup.CloudflareAccount, err = promptRequiredLine(reader, out, "Cloudflare account_id")
		if err != nil {
			return nil, err
		}
		setup.CloudflareAPIToken, err = promptRequiredLine(reader, out, "Cloudflare api_token")
		if err != nil {
			return nil, err
		}
	}

	setup.Model, err = promptRequiredLine(reader, out, "LLM model")
	if err != nil {
		return nil, err
	}

	setup.TelegramBotToken, err = promptRequiredLine(reader, out, "Telegram bot_token")
	if err != nil {
		return nil, err
	}
	setup.TelegramGroupTriggerMode, err = promptChoice(reader, out, "Telegram group trigger mode", []string{"strict", "smart", "talkative"}, "talkative")
	if err != nil {
		return nil, err
	}

	setup.ConfigureSlack, err = promptYesNo(reader, out, "Configure Slack now?", false)
	if err != nil {
		return nil, err
	}
	if setup.ConfigureSlack {
		setup.SlackBotToken, err = promptRequiredLine(reader, out, "Slack bot_token")
		if err != nil {
			return nil, err
		}
		setup.SlackAppToken, err = promptRequiredLine(reader, out, "Slack app_token")
		if err != nil {
			return nil, err
		}
		setup.SlackGroupTrigger, err = promptChoice(reader, out, "Slack group trigger mode", []string{"strict", "smart", "talkative"}, "smart")
		if err != nil {
			return nil, err
		}
	}

	fmt.Fprintln(out, "Interactive config setup captured.")
	return setup, nil
}

func defaultEndpointForProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "gemini":
		return "https://generativelanguage.googleapis.com"
	case "cloudflare":
		return "https://api.cloudflare.com/client/v4"
	default:
		return "https://api.openai.com"
	}
}

func promptRequiredLine(reader *bufio.Reader, out io.Writer, label string) (string, error) {
	for {
		v, err := promptLineWithDefault(reader, out, label, "")
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v), nil
		}
		fmt.Fprintln(out, "Value cannot be empty. Please try again.")
	}
}

func promptLineWithDefault(reader *bufio.Reader, out io.Writer, label string, defaultValue string) (string, error) {
	prompt := label + ": "
	if strings.TrimSpace(defaultValue) != "" {
		prompt = fmt.Sprintf("%s [%s]: ", label, defaultValue)
	}
	fmt.Fprint(out, prompt)
	line, err := readTrimmedLine(reader)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(line) == "" {
		return strings.TrimSpace(defaultValue), nil
	}
	return strings.TrimSpace(line), nil
}

func promptChoice(reader *bufio.Reader, out io.Writer, label string, options []string, defaultValue string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no options for %s", label)
	}
	joined := strings.Join(options, "/")
	for {
		fmt.Fprintf(out, "%s (%s) [%s]: ", label, joined, defaultValue)
		raw, err := readTrimmedLine(reader)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(raw) == "" {
			return strings.TrimSpace(defaultValue), nil
		}

		if idx, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
			if idx >= 1 && idx <= len(options) {
				return options[idx-1], nil
			}
		}

		lower := strings.ToLower(strings.TrimSpace(raw))
		for _, opt := range options {
			if lower == strings.ToLower(strings.TrimSpace(opt)) {
				return strings.TrimSpace(opt), nil
			}
		}
		fmt.Fprintf(out, "Invalid choice %q. Use one of: %s\n", raw, joined)
	}
}

func promptYesNo(reader *bufio.Reader, out io.Writer, label string, defaultYes bool) (bool, error) {
	defaultLabel := "y/N"
	if defaultYes {
		defaultLabel = "Y/n"
	}
	for {
		fmt.Fprintf(out, "%s [%s]: ", label, defaultLabel)
		raw, err := readTrimmedLine(reader)
		if err != nil {
			return false, err
		}
		if strings.TrimSpace(raw) == "" {
			return defaultYes, nil
		}
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(out, "Invalid choice. Please enter y or n.")
		}
	}
}

func readTrimmedLine(reader *bufio.Reader) (string, error) {
	if reader == nil {
		return "", fmt.Errorf("nil input reader")
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		if err == io.EOF {
			line = strings.TrimSpace(line)
			if line != "" {
				return line, nil
			}
			return "", fmt.Errorf("input closed")
		}
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func applyInstallConfigSetupOverrides(cfg string, setup *installConfigSetup) string {
	if setup == nil || strings.TrimSpace(cfg) == "" {
		return cfg
	}

	cfg = replaceConfigLine(cfg, "  provider: openai", "  provider: "+strings.ToLower(strings.TrimSpace(setup.Provider)))
	cfg = replaceConfigLine(cfg, `  endpoint: "https://api.openai.com"`, `  endpoint: `+yamlQuotedScalar(setup.Endpoint))
	cfg = replaceConfigLinePrefix(cfg, "  model: ", `  model: `+yamlQuotedScalar(setup.Model))

	apiKeyComment := " # or set via MISTER_MORPH_LLM_API_KEY"
	switch strings.ToLower(strings.TrimSpace(setup.Provider)) {
	case "cloudflare":
		cfg = replaceConfigLinePrefix(cfg, "  api_key: ", `  api_key: ""`+apiKeyComment)
		cfg = replaceConfigLine(cfg, `    account_id: ""`, `    account_id: `+yamlQuotedScalar(setup.CloudflareAccount))
		cfg = replaceConfigLinePrefix(cfg, "    api_token: ", `    api_token: `+yamlQuotedScalar(setup.CloudflareAPIToken))
	default:
		cfg = replaceConfigLinePrefix(cfg, "  api_key: ", `  api_key: `+yamlQuotedScalar(setup.APIKey)+apiKeyComment)
		cfg = replaceConfigLine(cfg, `    account_id: ""`, `    account_id: ""`)
		cfg = replaceConfigLinePrefix(cfg, "    api_token: ", `    api_token: ""`)
	}

	cfg = replaceConfigLine(cfg, `  bot_token: ""`, `  bot_token: `+yamlQuotedScalar(setup.TelegramBotToken))
	cfg = replaceConfigLine(cfg, `  group_trigger_mode: "talkative"`, `  group_trigger_mode: `+yamlQuotedScalar(setup.TelegramGroupTriggerMode))

	if setup.ConfigureSlack {
		// The first bot_token replacement above targets telegram; the second targets slack.
		cfg = replaceConfigLine(cfg, `  bot_token: ""`, `  bot_token: `+yamlQuotedScalar(setup.SlackBotToken))
		cfg = replaceConfigLine(cfg, `  app_token: ""`, `  app_token: `+yamlQuotedScalar(setup.SlackAppToken))
		cfg = replaceConfigLine(cfg, `  group_trigger_mode: "smart"`, `  group_trigger_mode: `+yamlQuotedScalar(setup.SlackGroupTrigger))
	}

	return cfg
}

func replaceConfigLine(cfg string, from string, to string) string {
	if strings.TrimSpace(cfg) == "" || strings.TrimSpace(from) == "" {
		return cfg
	}
	return strings.Replace(cfg, from, to, 1)
}

func replaceConfigLinePrefix(cfg string, prefix string, to string) string {
	if strings.TrimSpace(cfg) == "" || strings.TrimSpace(prefix) == "" {
		return cfg
	}
	lines := strings.Split(cfg, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, prefix) {
			lines[i] = to
			return strings.Join(lines, "\n")
		}
	}
	return cfg
}

func yamlQuotedScalar(v string) string {
	v = strings.ReplaceAll(v, "\\", "\\\\")
	v = strings.ReplaceAll(v, "\"", "\\\"")
	v = strings.ReplaceAll(v, "\n", "\\n")
	return `"` + v + `"`
}
