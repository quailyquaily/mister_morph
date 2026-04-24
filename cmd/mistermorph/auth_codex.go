package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/codexauth"
	"github.com/quailyquaily/mistermorph/internal/configbootstrap"
	"github.com/quailyquaily/mistermorph/internal/fsstore"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

type codexLoginOptions struct {
	SetDefault bool
}

type codexLoginRuntimeConfig struct {
	Provider            string
	Endpoint            string
	APIKey              string
	CloudflareAccountID string
	CloudflareAPIToken  string
}

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage local auth credentials",
	}
	cmd.AddCommand(newCodexAuthCmd())
	return cmd
}

func newCodexAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "codex",
		Short: "Manage OpenAI Codex OAuth login",
	}
	var loginOpts codexLoginOptions
	loginCmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in with OpenAI Codex OAuth device code",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCodexLogin(cmd.Context(), loginOpts)
		},
	}
	loginCmd.Flags().BoolVar(&loginOpts.SetDefault, "set-default", false, "Set llm.provider to openai_codex after login even when existing LLM credentials are configured.")
	cmd.AddCommand(loginCmd)
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show OpenAI Codex OAuth status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCodexStatus()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "logout",
		Short: "Delete local OpenAI Codex OAuth token",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCodexLogout()
		},
	})
	return cmd
}

func runCodexLogin(ctx context.Context, opts codexLoginOptions) error {
	stateDir := strings.TrimSpace(viper.GetString("file_state_dir"))
	cfg := codexauth.DefaultOAuthConfigValue()
	deviceCode, err := codexauth.RequestDeviceCode(ctx, cfg)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "Open this URL and enter the code:\n\n%s\n\nCode: %s\nExpires: %s\n\n", deviceCode.VerificationURL, deviceCode.UserCode, deviceCode.ExpiresAt.Format(time.RFC3339))
	fmt.Fprintln(os.Stdout, "Waiting for authorization...")

	interval := deviceCode.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	for {
		if !deviceCode.ExpiresAt.IsZero() && !deviceCode.ExpiresAt.After(time.Now().UTC()) {
			return fmt.Errorf("codex device code expired")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
		token, err := codexauth.CompleteDeviceCodeLogin(ctx, cfg, deviceCode)
		if codexauth.IsAuthorizationPending(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err := codexauth.WriteToken(stateDir, token); err != nil {
			return err
		}
		configUpdated, configPath, autoUpdated, err := maybeSetCodexAsDefaultLLM(opts.SetDefault)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "Logged in with OpenAI Codex OAuth.\nToken file: %s\n", codexauth.DisplayTokenPath())
		if !token.ExpiresAt.IsZero() {
			fmt.Fprintf(os.Stdout, "Access token expires: %s\n", token.ExpiresAt.Format(time.RFC3339))
		}
		if configUpdated {
			if autoUpdated {
				fmt.Fprintf(os.Stdout, "LLM config was empty; set default provider to openai_codex in %s.\n", configPath)
			} else {
				fmt.Fprintf(os.Stdout, "Set default provider to openai_codex in %s.\n", configPath)
			}
		} else {
			fmt.Fprintln(os.Stdout, "LLM config was not changed. Run login with --set-default to use openai_codex as the default provider.")
		}
		return nil
	}
}

func maybeSetCodexAsDefaultLLM(force bool) (updated bool, configPath string, autoUpdated bool, err error) {
	configPath, err = codexLoginConfigPath()
	if err != nil {
		return false, "", false, err
	}
	data, readErr := os.ReadFile(configPath)
	if readErr != nil {
		if !os.IsNotExist(readErr) {
			return false, configPath, false, readErr
		}
		data = nil
	}
	empty, err := codexLoginCurrentLLMConfigEmpty(data, codexLoginRuntimeConfigFromViper())
	if err != nil {
		return false, configPath, false, err
	}
	if !force && !empty {
		return false, configPath, false, nil
	}
	serialized, err := applyCodexDefaultLLMConfig(data)
	if err != nil {
		return false, configPath, false, err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return false, configPath, false, err
	}
	if err := fsstore.WriteTextAtomic(configPath, string(serialized), fsstore.FileOptions{DirPerm: 0o755, FilePerm: 0o600}); err != nil {
		return false, configPath, false, err
	}
	viper.Set("config", configPath)
	viper.Set("llm.provider", codexauth.ProviderName)
	viper.Set("llm.model", codexauth.DefaultModel)
	viper.Set("llm.endpoint", "")
	viper.Set("llm.api_key", "")
	viper.Set("llm.cloudflare.account_id", "")
	viper.Set("llm.cloudflare.api_token", "")
	return true, configPath, !force && empty, nil
}

func codexLoginConfigPath() (string, error) {
	if explicit := strings.TrimSpace(viper.GetString("config")); explicit != "" {
		return filepath.Clean(pathutil.ExpandHomePath(explicit)), nil
	}
	if path, _ := resolveConfigFile(); strings.TrimSpace(path) != "" {
		return filepath.Clean(path), nil
	}
	stateDir := strings.TrimSpace(viper.GetString("file_state_dir"))
	if stateDir == "" {
		stateDir = "~/.morph"
	}
	return filepath.Join(pathutil.ExpandHomePath(stateDir), "config.yaml"), nil
}

func codexLoginCurrentLLMConfigEmpty(data []byte, runtimeCfg codexLoginRuntimeConfig) (bool, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return codexLoginRuntimeLLMConfigEmpty(runtimeCfg), nil
	}
	doc, err := configbootstrap.LoadDocumentBytes(data)
	if err != nil {
		return false, err
	}
	root, err := configbootstrap.DocumentMapping(doc)
	if err != nil {
		return false, err
	}
	llmNode := configbootstrap.FindMappingValue(root, "llm")
	if llmNode == nil {
		return codexLoginRuntimeLLMConfigEmpty(runtimeCfg), nil
	}
	if llmNode.Kind != yaml.MappingNode {
		return false, nil
	}
	provider := codexLoginFirstNonEmpty(codexLoginScalarValue(llmNode, "provider"), runtimeCfg.Provider)
	if strings.EqualFold(provider, "cloudflare") {
		cloudflareNode := configbootstrap.FindMappingValue(llmNode, "cloudflare")
		accountIDConfigured := codexLoginScalarConfigured(cloudflareNode, "account_id", runtimeCfg.CloudflareAccountID) ||
			codexLoginScalarConfigured(llmNode, "account_id", runtimeCfg.CloudflareAccountID)
		apiTokenConfigured := codexLoginScalarConfigured(cloudflareNode, "api_token", runtimeCfg.CloudflareAPIToken) ||
			codexLoginScalarConfigured(llmNode, "api_token", runtimeCfg.CloudflareAPIToken)
		return !accountIDConfigured && !apiTokenConfigured, nil
	}
	endpointConfigured := codexLoginScalarConfigured(llmNode, "endpoint", runtimeCfg.Endpoint)
	apiKeyConfigured := codexLoginScalarConfigured(llmNode, "api_key", runtimeCfg.APIKey)
	return !endpointConfigured && !apiKeyConfigured, nil
}

func codexLoginRuntimeLLMConfigEmpty(cfg codexLoginRuntimeConfig) bool {
	if strings.EqualFold(strings.TrimSpace(cfg.Provider), "cloudflare") {
		return strings.TrimSpace(cfg.CloudflareAccountID) == "" && strings.TrimSpace(cfg.CloudflareAPIToken) == ""
	}
	return strings.TrimSpace(cfg.Endpoint) == "" && strings.TrimSpace(cfg.APIKey) == ""
}

func codexLoginRuntimeConfigFromViper() codexLoginRuntimeConfig {
	return codexLoginRuntimeConfig{
		Provider:            strings.TrimSpace(viper.GetString("llm.provider")),
		Endpoint:            strings.TrimSpace(viper.GetString("llm.endpoint")),
		APIKey:              strings.TrimSpace(viper.GetString("llm.api_key")),
		CloudflareAccountID: strings.TrimSpace(viper.GetString("llm.cloudflare.account_id")),
		CloudflareAPIToken:  strings.TrimSpace(viper.GetString("llm.cloudflare.api_token")),
	}
}

func applyCodexDefaultLLMConfig(data []byte) ([]byte, error) {
	doc, err := configbootstrap.LoadDocumentBytes(data)
	if err != nil {
		return nil, err
	}
	root, err := configbootstrap.DocumentMapping(doc)
	if err != nil {
		return nil, err
	}
	llmNode := configbootstrap.EnsureMappingValue(root, "llm")
	configbootstrap.SetOrDeleteMappingScalar(llmNode, "provider", codexauth.ProviderName)
	configbootstrap.SetOrDeleteMappingScalar(llmNode, "model", codexauth.DefaultModel)
	configbootstrap.SetOrDeleteMappingScalar(llmNode, "endpoint", "")
	configbootstrap.SetOrDeleteMappingScalar(llmNode, "api_key", "")
	configbootstrap.DeleteMappingKey(llmNode, "cloudflare")
	return configbootstrap.MarshalDocument(doc)
}

func codexLoginScalarConfigured(node *yaml.Node, key string, runtimeValue string) bool {
	if strings.TrimSpace(runtimeValue) != "" {
		return true
	}
	return strings.TrimSpace(codexLoginScalarValue(node, key)) != ""
}

func codexLoginScalarValue(node *yaml.Node, key string) string {
	value := configbootstrap.FindMappingValue(node, key)
	if value == nil || value.Kind != yaml.ScalarNode {
		return ""
	}
	return strings.TrimSpace(value.Value)
}

func codexLoginFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func runCodexStatus() error {
	status := codexauth.ReadStatus(viper.GetString("file_state_dir"), time.Now().UTC())
	if !status.LoggedIn {
		fmt.Fprintln(os.Stdout, "OpenAI Codex OAuth: not logged in")
		return nil
	}
	fmt.Fprintln(os.Stdout, "OpenAI Codex OAuth: logged in")
	fmt.Fprintf(os.Stdout, "Token file: %s\n", codexauth.DisplayTokenPath())
	fmt.Fprintf(os.Stdout, "Access token present: %t\n", status.AccessTokenPresent)
	fmt.Fprintf(os.Stdout, "Refresh token present: %t\n", status.RefreshTokenPresent)
	fmt.Fprintf(os.Stdout, "Access token expired: %t\n", status.AccessTokenExpired)
	if status.ExpiresAt != nil && !status.ExpiresAt.IsZero() {
		fmt.Fprintf(os.Stdout, "Access token expires: %s\n", status.ExpiresAt.Format(time.RFC3339))
	}
	if status.AccountID != "" {
		fmt.Fprintf(os.Stdout, "Account ID: %s\n", status.AccountID)
	}
	if status.PlanType != "" {
		fmt.Fprintf(os.Stdout, "Plan: %s\n", status.PlanType)
	}
	fmt.Fprintf(os.Stdout, "Token file permissions ok: %t\n", status.FileModeOK)
	if status.FileModeWarning != "" {
		fmt.Fprintf(os.Stdout, "Token file warning: %s\n", status.FileModeWarning)
	}
	return nil
}

func runCodexLogout() error {
	removed, err := codexauth.DeleteToken(viper.GetString("file_state_dir"))
	if err != nil {
		return err
	}
	if removed {
		fmt.Fprintf(os.Stdout, "Deleted local OpenAI Codex OAuth token at %s.\n", codexauth.DisplayTokenPath())
	} else {
		fmt.Fprintln(os.Stdout, "OpenAI Codex OAuth token was not present.")
	}
	fmt.Fprintln(os.Stdout, "This does not revoke the OpenAI authorization grant. Revoke it in OpenAI or ChatGPT settings if needed.")
	return nil
}
