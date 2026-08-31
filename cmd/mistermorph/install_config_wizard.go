package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/secref"
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
	secretIDs          []string
}

func protectInstallSetupSecrets(ctx context.Context, setup *installConfigSetup, store secref.OSStore) error {
	if setup == nil || store == nil {
		return nil
	}
	value := &setup.APIKey
	if normalizeInferenceProviderForSetup(setup.Provider) == setupProviderCloudflare {
		value = &setup.CloudflareAPIToken
	}
	secret := strings.TrimSpace(*value)
	if secret == "" {
		return nil
	}
	if _, ok := secref.ParseSingleRef(secret); ok {
		return nil
	}
	id, err := secref.NewOSSecretID()
	if err != nil {
		return fmt.Errorf("create system secret reference: %w", err)
	}
	if err := store.Put(ctx, id, []byte(secret)); err != nil {
		return fmt.Errorf("store install credential: %w", secref.ErrOSStoreUnavailable)
	}
	*value = secref.OSSecretRef(id)
	setup.secretIDs = append(setup.secretIDs, id)
	return nil
}

func discardInstallSetupSecrets(ctx context.Context, setup *installConfigSetup, store secref.OSStore) {
	if setup == nil || store == nil {
		return
	}
	for _, id := range setup.secretIDs {
		_ = store.Delete(ctx, id)
	}
	setup.secretIDs = nil
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
	candidates = append(candidates, pathutil.DefaultConfigPath())

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
