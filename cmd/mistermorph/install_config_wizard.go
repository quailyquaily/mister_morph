package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

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

	ConfigureConsole            bool
	ConsoleListen               string
	ConsoleBasePath             string
	ConsolePassword             string
	ConsoleEndpointName         string
	ConsoleEndpointURL          string
	ConsoleEndpointAuthTokenEnv string
	ServerAuthTokenEnv          string
	GeneratedServerAuthToken    string
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

	provider, err := promptChoice(
		reader,
		out,
		"Select llm provider",
		setupProviderChoices(),
		setupProviderOpenAICompatible,
	)
	if err != nil {
		return nil, err
	}
	endpointDefault := defaultEndpointForSetupProvider(provider)
	endpoint, err := promptLineWithDefault(reader, out, "LLM endpoint", endpointDefault)
	if err != nil {
		return nil, err
	}

	setup := &installConfigSetup{
		Provider: provider,
		Endpoint: endpoint,
	}

	switch provider {
	case setupProviderOpenAICompatible, setupProviderGemini, setupProviderAnthropic:
		setup.APIKey, err = promptRequiredLine(reader, out, "LLM api_key")
		if err != nil {
			return nil, err
		}
	case setupProviderCloudflare:
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

	fmt.Fprintln(out, "Interactive config setup captured.")
	return setup, nil
}

var envVarNamePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

func isValidEnvVarName(raw string) bool {
	return envVarNamePattern.MatchString(strings.TrimSpace(raw))
}

func normalizeConsoleBasePath(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		v = "/"
	}
	if !strings.HasPrefix(v, "/") {
		v = "/" + v
	}
	v = path.Clean(v)
	if v == "." || v == "" || v == "/" {
		return "/", nil
	}
	return strings.TrimRight(v, "/"), nil
}

func normalizeConsoleEndpointURL(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", fmt.Errorf("url is required")
	}
	parsed, err := neturl.Parse(v)
	if err != nil {
		return "", err
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("only http/https are supported")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return "", fmt.Errorf("missing host")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func isLikelyLocalEndpointURL(raw string) bool {
	normalized, err := normalizeConsoleEndpointURL(raw)
	if err != nil {
		return false
	}
	parsed, err := neturl.Parse(normalized)
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func generateInstallAuthToken() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func printConsoleSetupSummary(out io.Writer, setup *installConfigSetup) {
	if setup == nil || !setup.ConfigureConsole {
		return
	}
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Console setup summary:")
	fmt.Fprintln(out, renderConsoleConfigSnippet(setup))
	fmt.Fprintln(out, "Suggested env vars:")
	for _, line := range setupSuggestedEnvVarLines(setup) {
		fmt.Fprintf(out, "  - %s\n", line)
	}
	ok, detail := probeConsoleEndpointHealth(setup.ConsoleEndpointURL)
	if ok {
		fmt.Fprintf(out, "Endpoint health check: ok (%s)\n", detail)
		return
	}
	fmt.Fprintf(out, "Endpoint health check: failed (%s)\n", detail)
}

func setupSuggestedEnvVarLines(setup *installConfigSetup) []string {
	out := []string{
		"MISTER_MORPH_CONSOLE_PASSWORD",
		"MISTER_MORPH_CONSOLE_PASSWORD_HASH",
	}
	if setup != nil {
		if envName := strings.TrimSpace(setup.ConsoleEndpointAuthTokenEnv); envName != "" {
			out = append(out, envName)
		}
		if envName := strings.TrimSpace(setup.ServerAuthTokenEnv); envName != "" {
			if token := strings.TrimSpace(setup.GeneratedServerAuthToken); token != "" {
				out = append(out, fmt.Sprintf(`export %s=%q`, envName, token))
			}
		}
	}
	seen := map[string]bool{}
	uniq := make([]string, 0, len(out))
	for _, item := range out {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		uniq = append(uniq, item)
	}
	return uniq
}

func probeConsoleEndpointHealth(endpointURL string) (bool, string) {
	target := strings.TrimRight(strings.TrimSpace(endpointURL), "/") + "/health"
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return false, err.Error()
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, resp.Status
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	msg := strings.TrimSpace(string(raw))
	if msg == "" {
		msg = resp.Status
	}
	return false, fmt.Sprintf("status=%d %s", resp.StatusCode, msg)
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

func renderConsoleConfigSnippet(setup *installConfigSetup) string {
	listen := strings.TrimSpace(setup.ConsoleListen)
	if listen == "" {
		listen = "127.0.0.1:9080"
	}
	basePath, err := normalizeConsoleBasePath(setup.ConsoleBasePath)
	if err != nil {
		basePath = "/"
	}
	password := strings.TrimSpace(setup.ConsolePassword)
	endpointName := strings.TrimSpace(setup.ConsoleEndpointName)
	if endpointName == "" {
		endpointName = "Main Runtime"
	}
	endpointURL, err := normalizeConsoleEndpointURL(setup.ConsoleEndpointURL)
	if err != nil {
		endpointURL = "http://127.0.0.1:8787"
	}
	endpointTokenEnv := strings.TrimSpace(setup.ConsoleEndpointAuthTokenEnv)
	if !isValidEnvVarName(endpointTokenEnv) {
		if shared := strings.TrimSpace(setup.ServerAuthTokenEnv); isValidEnvVarName(shared) {
			endpointTokenEnv = shared
		}
	}
	if !isValidEnvVarName(endpointTokenEnv) {
		endpointTokenEnv = "MISTER_MORPH_SERVER_AUTH_TOKEN"
	}
	endpointTokenRef := "${" + endpointTokenEnv + "}"

	lines := []string{
		"console:",
		"  # Bind address for console API + SPA.",
		"  listen: " + yamlQuotedScalar(listen),
		"  # Base path for console routes and static files.",
		"  base_path: " + yamlQuotedScalar(basePath),
		"  # Optional static directory for SPA build artifacts.",
		"  # Can be overridden by --console-static-dir.",
		`  static_dir: ""`,
		"  # Prefer password_hash in production; set via env when possible.",
		"  password: " + yamlQuotedScalar(password) + " # or set via MISTER_MORPH_CONSOLE_PASSWORD",
		"  # Bcrypt hash string, e.g. \"$2a$...\"",
		`  password_hash: "" # or set via MISTER_MORPH_CONSOLE_PASSWORD_HASH`,
		"  # Session TTL for console bearer token.",
		`  session_ttl: "12h"`,
		"  # Runtime endpoints shown in Console endpoint selector.",
		"  # Use ${ENV_VAR} syntax in auth_token to reference environment variables.",
		"  endpoints:",
		"    - name: " + yamlQuotedScalar(endpointName),
		"      url: " + yamlQuotedScalar(endpointURL),
		"      auth_token: " + yamlQuotedScalar(endpointTokenRef),
	}
	return strings.Join(lines, "\n")
}

func yamlQuotedScalar(v string) string {
	v = strings.ReplaceAll(v, "\\", "\\\\")
	v = strings.ReplaceAll(v, "\"", "\\\"")
	v = strings.ReplaceAll(v, "\n", "\\n")
	return `"` + v + `"`
}
