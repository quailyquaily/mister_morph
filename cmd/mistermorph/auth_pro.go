package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/configbootstrap"
	"github.com/quailyquaily/mistermorph/internal/fsstore"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/proaccount"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type proLoginOptions struct {
	SetDefault bool
}

func newProAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pro",
		Short: "Manage MisterMorph Pro OAuth login",
	}
	var loginOpts proLoginOptions
	loginCmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in with MisterMorph Pro OAuth device code",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProLogin(cmd.Context(), loginOpts)
		},
	}
	loginCmd.Flags().BoolVar(&loginOpts.SetDefault, "set-default", false, "Set llm.inference_provider to mistermorph_pro after login even when existing LLM credentials are configured.")
	cmd.AddCommand(loginCmd)
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show MisterMorph Pro OAuth status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProStatus()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "logout",
		Short: "Delete local MisterMorph Pro OAuth session",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProLogout()
		},
	})
	return cmd
}

func runProLogin(ctx context.Context, opts proLoginOptions) error {
	stateDir := strings.TrimSpace(viper.GetString("file_state_dir"))
	oauthCfg := proaccount.DefaultOAuthConfigValue()
	routerCfg := proaccount.DefaultRouterConfigValue()
	deviceCode, err := proaccount.RequestDeviceCode(ctx, oauthCfg)
	if err != nil {
		return err
	}

	verificationURL := authLoginFirstNonEmpty(deviceCode.VerificationURLComplete, deviceCode.VerificationURL)
	fmt.Fprintf(os.Stdout, "Open this URL and enter the code:\n\n%s\n\nCode: %s\nExpires: %s\n\n", verificationURL, deviceCode.UserCode, deviceCode.ExpiresAt.Format(time.RFC3339))
	fmt.Fprintln(os.Stdout, "Waiting for authorization...")

	interval := deviceCode.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	for {
		if !deviceCode.ExpiresAt.IsZero() && !deviceCode.ExpiresAt.After(time.Now().UTC()) {
			return fmt.Errorf("MisterMorph Pro device code expired")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
		session, err := proaccount.CompleteDeviceCodeLogin(ctx, oauthCfg, routerCfg, deviceCode)
		if proaccount.IsAuthorizationPending(err) {
			continue
		}
		if proaccount.IsSlowDown(err) {
			interval += proaccount.SlowDownIncrement
			continue
		}
		if proaccount.IsDeviceCodeExpired(err) {
			return fmt.Errorf("MisterMorph Pro device code expired")
		}
		if proaccount.IsAccessDenied(err) {
			return fmt.Errorf("MisterMorph Pro authorization denied")
		}
		if err != nil {
			return err
		}
		if err := proaccount.WriteSession(stateDir, session); err != nil {
			return err
		}
		configUpdated, configPath, autoUpdated, err := maybeSetProAsDefaultLLM(opts.SetDefault)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "Logged in with MisterMorph Pro OAuth.\nSession file: %s\n", proaccount.DisplaySessionPath())
		if !session.ExpiresAt.IsZero() {
			fmt.Fprintf(os.Stdout, "Access token expires: %s\n", session.ExpiresAt.Format(time.RFC3339))
		}
		fmt.Fprintf(os.Stdout, "Subscription API key stored: %t\n", strings.TrimSpace(session.SubscriptionAPIKey) != "")
		if configUpdated {
			if autoUpdated {
				fmt.Fprintf(os.Stdout, "LLM config was empty; set default inference provider to mistermorph_pro in %s.\n", configPath)
			} else {
				fmt.Fprintf(os.Stdout, "Set default inference provider to mistermorph_pro in %s.\n", configPath)
			}
		} else {
			fmt.Fprintln(os.Stdout, "LLM config was not changed. Run login with --set-default to use mistermorph_pro as the default inference provider.")
		}
		return nil
	}
}

func maybeSetProAsDefaultLLM(force bool) (updated bool, configPath string, autoUpdated bool, err error) {
	configPath, err = authLoginConfigPath()
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
	empty, err := authLoginCurrentLLMConfigEmpty(data, authLoginRuntimeConfigFromViper())
	if err != nil {
		return false, configPath, false, err
	}
	if !force && !empty {
		return false, configPath, false, nil
	}
	serialized, err := applyProDefaultLLMConfig(data)
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
	viper.Set("llm.inference_provider", llmutil.InferenceProviderMisterMorphPro)
	viper.Set("llm.model", proaccount.DefaultModel)
	viper.Set("llm.provider", "")
	viper.Set("llm.endpoint", "")
	viper.Set("llm.api_key", "")
	viper.Set("llm.cloudflare.account_id", "")
	viper.Set("llm.cloudflare.api_token", "")
	viper.Set("llm.bedrock.aws_key", "")
	viper.Set("llm.bedrock.aws_secret", "")
	viper.Set("llm.bedrock.region", "")
	viper.Set("llm.bedrock.model_arn", "")
	return true, configPath, !force && empty, nil
}

func applyProDefaultLLMConfig(data []byte) ([]byte, error) {
	doc, err := configbootstrap.LoadDocumentBytes(data)
	if err != nil {
		return nil, err
	}
	root, err := configbootstrap.DocumentMapping(doc)
	if err != nil {
		return nil, err
	}
	llmNode := configbootstrap.EnsureMappingValue(root, "llm")
	configbootstrap.SetOrDeleteMappingScalar(llmNode, "inference_provider", llmutil.InferenceProviderMisterMorphPro)
	configbootstrap.SetOrDeleteMappingScalar(llmNode, "model", proaccount.DefaultModel)
	configbootstrap.SetOrDeleteMappingScalar(llmNode, "provider", "")
	configbootstrap.SetOrDeleteMappingScalar(llmNode, "endpoint", "")
	configbootstrap.SetOrDeleteMappingScalar(llmNode, "api_key", "")
	configbootstrap.DeleteMappingKey(llmNode, "azure")
	configbootstrap.DeleteMappingKey(llmNode, "bedrock")
	configbootstrap.DeleteMappingKey(llmNode, "cloudflare")
	return configbootstrap.MarshalDocument(doc)
}

func runProStatus() error {
	status := proaccount.ReadStatus(viper.GetString("file_state_dir"), time.Now().UTC())
	if !status.LoggedIn {
		fmt.Fprintln(os.Stdout, "MisterMorph Pro OAuth: not logged in")
		return nil
	}
	fmt.Fprintln(os.Stdout, "MisterMorph Pro OAuth: logged in")
	fmt.Fprintf(os.Stdout, "Session file: %s\n", proaccount.DisplaySessionPath())
	fmt.Fprintf(os.Stdout, "Access token present: %t\n", status.AccessTokenPresent)
	fmt.Fprintf(os.Stdout, "Refresh token present: %t\n", status.RefreshTokenPresent)
	fmt.Fprintf(os.Stdout, "Access token expired: %t\n", status.AccessTokenExpired)
	fmt.Fprintf(os.Stdout, "Subscription API key present: %t\n", status.SubscriptionAPIKeyPresent)
	if status.ExpiresAt != nil && !status.ExpiresAt.IsZero() {
		fmt.Fprintf(os.Stdout, "Access token expires: %s\n", status.ExpiresAt.Format(time.RFC3339))
	}
	if status.Subscription != "" {
		fmt.Fprintf(os.Stdout, "Subscription: %s\n", status.Subscription)
	}
	if user := proStatusUserLabel(status.UserInfo); user != "" {
		fmt.Fprintf(os.Stdout, "Account: %s\n", user)
	}
	fmt.Fprintf(os.Stdout, "Session file permissions ok: %t\n", status.FileModeOK)
	if status.FileModeWarning != "" {
		fmt.Fprintf(os.Stdout, "Session file warning: %s\n", status.FileModeWarning)
	}
	return nil
}

func runProLogout() error {
	removed, err := proaccount.DeleteSession(viper.GetString("file_state_dir"))
	if err != nil {
		return err
	}
	if removed {
		fmt.Fprintf(os.Stdout, "Deleted local MisterMorph Pro OAuth session at %s.\n", proaccount.DisplaySessionPath())
	} else {
		fmt.Fprintln(os.Stdout, "MisterMorph Pro OAuth session was not present.")
	}
	fmt.Fprintln(os.Stdout, "This only deletes the local session. Revoke server-side access from MisterMorph Pro account settings if needed.")
	return nil
}

func proStatusUserLabel(user map[string]any) string {
	for _, key := range []string{"name", "email", "union_id"} {
		if value, ok := user[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
