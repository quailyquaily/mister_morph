package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/configbootstrap"
	"github.com/quailyquaily/mistermorph/internal/fsstore"
	"github.com/quailyquaily/mistermorph/internal/xaiauth"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type xaiLoginOptions struct {
	SetDefault bool
	wait       func(context.Context, time.Duration) error
}

func newXAIAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "xai",
		Short: "Manage xAI Grok OAuth login",
	}
	var loginOpts xaiLoginOptions
	loginCmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in with xAI Grok OAuth device code",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runXAILogin(cmd.Context(), loginOpts, xaiauth.OAuthConfig{}, cmd.OutOrStdout())
		},
	}
	loginCmd.Flags().BoolVar(
		&loginOpts.SetDefault,
		"set-default",
		false,
		"Set xAI Grok OAuth as the default LLM provider after login.",
	)
	cmd.AddCommand(loginCmd)
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show xAI Grok OAuth status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runXAIStatus(viper.GetString("file_state_dir"), cmd.OutOrStdout())
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "logout",
		Short: "Revoke and delete the local xAI Grok OAuth token",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runXAILogout(
				cmd.Context(),
				viper.GetString("file_state_dir"),
				xaiauth.OAuthConfig{},
				cmd.OutOrStdout(),
				cmd.ErrOrStderr(),
			)
		},
	})
	return cmd
}

func runXAILogin(
	ctx context.Context,
	opts xaiLoginOptions,
	cfg xaiauth.OAuthConfig,
	output io.Writer,
) error {
	stateDir := strings.TrimSpace(viper.GetString("file_state_dir"))
	deviceCode, err := xaiauth.RequestDeviceCode(ctx, cfg)
	if err != nil {
		return err
	}

	verificationURL := deviceCode.VerificationURL
	if strings.TrimSpace(deviceCode.VerificationURLComplete) != "" {
		verificationURL = deviceCode.VerificationURLComplete
	}
	fmt.Fprintf(
		output,
		"Open this URL and enter the code:\n\n%s\n\nCode: %s\nExpires: %s\n\n",
		verificationURL,
		deviceCode.UserCode,
		deviceCode.ExpiresAt.Format(time.RFC3339),
	)
	fmt.Fprintln(output, "Waiting for authorization...")

	interval := deviceCode.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	for {
		if !deviceCode.ExpiresAt.IsZero() && !deviceCode.ExpiresAt.After(time.Now().UTC()) {
			return xaiauth.ErrDeviceCodeExpired
		}
		if opts.wait != nil {
			if err := opts.wait(ctx, interval); err != nil {
				return err
			}
		} else {
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return ctx.Err()
			case <-timer.C:
			}
		}

		token, err := xaiauth.PollDeviceCode(ctx, cfg, deviceCode)
		switch {
		case errors.Is(err, xaiauth.ErrAuthorizationPending):
			continue
		case errors.Is(err, xaiauth.ErrSlowDown):
			interval += 5 * time.Second
			continue
		case err != nil:
			return err
		}
		if err := xaiauth.WriteToken(stateDir, token); err != nil {
			return err
		}
		if opts.SetDefault {
			configPath, err := setXAIDefaultLLMConfig()
			if err != nil {
				return err
			}
			fmt.Fprintf(output, "Set xAI Grok OAuth as the default LLM provider in %s.\n", configPath)
		}
		fmt.Fprintf(output, "Logged in with xAI Grok OAuth.\nToken file: %s\n", xaiauth.DisplayTokenPath)
		if !token.ExpiresAt.IsZero() {
			fmt.Fprintf(output, "Access token expires: %s\n", token.ExpiresAt.Format(time.RFC3339))
		}
		if !opts.SetDefault {
			fmt.Fprintln(output, "LLM config was not changed. Use --set-default to select xAI Grok OAuth.")
		}
		return nil
	}
}

func runXAIStatus(stateDir string, output io.Writer) error {
	status := xaiauth.ReadStatus(stateDir, time.Now().UTC())
	if !status.LoggedIn {
		fmt.Fprintln(output, "xAI Grok OAuth: not logged in")
		return nil
	}
	fmt.Fprintln(output, "xAI Grok OAuth: logged in")
	fmt.Fprintf(output, "Token file: %s\n", xaiauth.DisplayTokenPath)
	fmt.Fprintf(output, "Access token present: %t\n", status.AccessTokenPresent)
	fmt.Fprintf(output, "Refresh token present: %t\n", status.RefreshTokenPresent)
	fmt.Fprintf(output, "Access token expired: %t\n", status.AccessTokenExpired)
	if status.AccessTokenExpired && status.RefreshTokenPresent {
		fmt.Fprintln(output, "Access token can be refreshed when next used: true")
	}
	if status.ExpiresAt != nil && !status.ExpiresAt.IsZero() {
		fmt.Fprintf(output, "Access token expires: %s\n", status.ExpiresAt.Format(time.RFC3339))
	}
	fmt.Fprintf(output, "Token file permissions ok: %t\n", status.FileModeOK)
	if status.FileModeWarning != "" {
		fmt.Fprintf(output, "Token file warning: %s\n", status.FileModeWarning)
	}
	return nil
}

func runXAILogout(
	ctx context.Context,
	stateDir string,
	cfg xaiauth.OAuthConfig,
	output io.Writer,
	warnings io.Writer,
) error {
	token, present, readErr := xaiauth.ReadToken(stateDir)
	var revokeErr error
	if readErr != nil {
		revokeErr = fmt.Errorf("read local token: %w", readErr)
	} else if present {
		revokeErr = xaiauth.RevokeToken(ctx, cfg, token)
	}

	removed, err := xaiauth.DeleteToken(stateDir)
	if err != nil {
		return err
	}
	if revokeErr != nil {
		fmt.Fprintf(warnings, "Warning: could not revoke the xAI OAuth grant; the local token was still deleted: %v\n", revokeErr)
	}
	if removed {
		fmt.Fprintf(output, "Deleted local xAI Grok OAuth token at %s.\n", xaiauth.DisplayTokenPath)
	} else {
		fmt.Fprintln(output, "xAI Grok OAuth token was not present.")
	}
	return nil
}

func setXAIDefaultLLMConfig() (string, error) {
	configPath, err := authLoginConfigPath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return configPath, err
	}
	serialized, err := applyXAIDefaultLLMConfig(data)
	if err != nil {
		return configPath, err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return configPath, err
	}
	if err := fsstore.WriteTextAtomic(configPath, string(serialized), fsstore.FileOptions{
		DirPerm:  0o755,
		FilePerm: 0o600,
	}); err != nil {
		return configPath, err
	}

	viper.Set("config", configPath)
	viper.Set("llm.inference_provider", xaiauth.ProviderName)
	viper.Set("llm.provider", xaiauth.ProviderName)
	viper.Set("llm.model", xaiauth.DefaultModel)
	viper.Set("llm.endpoint", "")
	viper.Set("llm.api_key", "")
	viper.Set("llm.cloudflare.account_id", "")
	viper.Set("llm.cloudflare.api_token", "")
	viper.Set("llm.bedrock.aws_key", "")
	viper.Set("llm.bedrock.aws_secret", "")
	viper.Set("llm.bedrock.aws_session_token", "")
	viper.Set("llm.bedrock.aws_profile", "")
	viper.Set("llm.bedrock.region", "")
	viper.Set("llm.bedrock.model_arn", "")
	viper.Set("llm.aws.key", "")
	viper.Set("llm.aws.secret", "")
	viper.Set("llm.aws.session_token", "")
	viper.Set("llm.aws.profile", "")
	viper.Set("llm.aws.region", "")
	viper.Set("llm.aws.bedrock_model_arn", "")
	return configPath, nil
}

func applyXAIDefaultLLMConfig(data []byte) ([]byte, error) {
	doc, err := configbootstrap.LoadDocumentBytes(data)
	if err != nil {
		return nil, err
	}
	root, err := configbootstrap.DocumentMapping(doc)
	if err != nil {
		return nil, err
	}
	llmNode := configbootstrap.EnsureMappingValue(root, "llm")
	configbootstrap.SetOrDeleteMappingScalar(llmNode, "inference_provider", xaiauth.ProviderName)
	configbootstrap.SetOrDeleteMappingScalar(llmNode, "provider", xaiauth.ProviderName)
	configbootstrap.SetOrDeleteMappingScalar(llmNode, "model", xaiauth.DefaultModel)
	configbootstrap.SetOrDeleteMappingScalar(llmNode, "endpoint", "")
	configbootstrap.SetOrDeleteMappingScalar(llmNode, "api_key", "")
	configbootstrap.DeleteMappingKey(llmNode, "cloudflare")
	configbootstrap.DeleteMappingKey(llmNode, "bedrock")
	configbootstrap.DeleteMappingKey(llmNode, "aws")
	return configbootstrap.MarshalDocument(doc)
}
