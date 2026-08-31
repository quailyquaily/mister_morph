package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/assets"
	"github.com/quailyquaily/mistermorph/internal/clifmt"
	"github.com/quailyquaily/mistermorph/internal/configbootstrap"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/secref"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newInstallCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "install [dir]",
		Short: "Install config.yaml plus the core onboarding markdown files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolveInstallDir(args)
			if err != nil {
				return err
			}

			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}

			cfgPath := filepath.Join(dir, "config.yaml")
			writeConfig := true
			if _, err := os.Stat(cfgPath); err == nil {
				fmt.Fprintf(os.Stderr, "warn: config already exists, skipping: %s\n", cfgPath)
				writeConfig = false
			}
			var cfgSetup *installConfigSetup
			var installSecretStore secref.OSStore
			if writeConfig {
				if source, ok := findReadableInstallConfig(cmd, dir); ok {
					fmt.Printf("[i] found config.yaml, skip interactive setup: %s\n", source)
				} else {
					cfgSetup, err = maybeCollectInstallConfigSetup(cmd, yes)
					if err != nil {
						return err
					}
					if cfgSetup != nil {
						installSecretStore = secref.NewOSStore()
						if err := protectInstallSetupSecrets(cmd.Context(), cfgSetup, installSecretStore); err != nil {
							return fmt.Errorf("save setup credential in the system secret store: %w", err)
						}
					}
				}
			}

			personaDir := filepath.Join(dir, statepaths.PersonaDirName)
			identityPath := filepath.Join(personaDir, statepaths.IdentityFilename)
			writeIdentity := true
			if _, err := os.Stat(identityPath); err == nil {
				writeIdentity = false
			}
			soulPath := filepath.Join(personaDir, statepaths.SoulFilename)
			writeSoul := true
			if _, err := os.Stat(soulPath); err == nil {
				writeSoul = false
			}
			hbPath := filepath.Join(dir, "HEARTBEAT.md")
			writeHeartbeat := true
			if _, err := os.Stat(hbPath); err == nil {
				writeHeartbeat = false
			}
			cronPath := filepath.Join(dir, "cron.yaml")
			writeCron := true
			if _, err := os.Stat(cronPath); err == nil {
				writeCron = false
			}

			initialSteps := []installStep{
				{
					Name:  "config.yaml",
					Path:  cfgPath,
					Write: writeConfig,
					Loader: func() (string, error) {
						body, err := loadConfigExample()
						if err != nil {
							return "", err
						}
						return patchInitConfigWithSetup(body, dir, cfgSetup)
					},
				},
				{
					Name:   statepaths.IdentityFilename,
					Path:   identityPath,
					Write:  writeIdentity,
					Loader: loadIdentityTemplate,
				},
				{
					Name:   statepaths.SoulFilename,
					Path:   soulPath,
					Write:  writeSoul,
					Loader: loadSoulTemplate,
				},
			}
			fmt.Println(clifmt.Headerf("==> Installing setup flow (%d steps)", len(initialSteps)))
			for i, step := range initialSteps {
				if err := writeInstallStepFile(i+1, len(initialSteps), step); err != nil {
					if step.Name == "config.yaml" {
						discardInstallSetupSecrets(cmd.Context(), cfgSetup, installSecretStore)
					}
					return err
				}
				if !step.Write {
					continue
				}
				if yes || !supportsInteractivePrompts(cmd) {
					continue
				}
				switch step.Name {
				case statepaths.IdentityFilename:
					if err := runInstallIdentitySetup(cmd.InOrStdin(), cmd.OutOrStdout(), step.Path); err != nil {
						return err
					}
				case statepaths.SoulFilename:
					if err := runInstallSoulSetup(cmd.InOrStdin(), cmd.OutOrStdout(), step.Path); err != nil {
						return err
					}
				}
			}

			deferredSteps := []installStep{
				{
					Name:   "HEARTBEAT.md",
					Path:   hbPath,
					Write:  writeHeartbeat,
					Loader: loadHeartbeatTemplate,
				},
				{
					Name:   "cron.yaml",
					Path:   cronPath,
					Write:  writeCron,
					Loader: loadCronTemplate,
				},
			}
			fmt.Println(clifmt.Headerf("==> Installing deferred state files (%d files)", len(deferredSteps)))
			for i, step := range deferredSteps {
				if err := writeInstallStepFile(i+1, len(deferredSteps), step); err != nil {
					return err
				}
			}

			fmt.Printf("[i] initialized %s\n", dir)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompts (dangerous)")

	return cmd
}

type installStep struct {
	Name   string
	Path   string
	Write  bool
	Loader func() (string, error)
}

func writeInstallStepFile(index int, total int, step installStep) error {
	fmt.Printf("[%d/%d] %s (1 file) ... ", index, total, step.Name)
	if !step.Write {
		fmt.Printf("%s %s\n", clifmt.Success("done"), clifmt.Warn("(skipped)"))
		fmt.Printf("    %s %s\n", step.Path, clifmt.Warn("(skipped)"))
		return nil
	}
	body, err := step.Loader()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(step.Path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(step.Path, []byte(body), 0o644); err != nil {
		return err
	}
	fmt.Println(clifmt.Success("done"))
	fmt.Printf("    %s\n", step.Path)
	return nil
}

func resolveInstallDir(args []string) (string, error) {
	var dir string
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		dir = strings.TrimSpace(args[0])
	} else {
		dir = strings.TrimSpace(viper.GetString("file_state_dir"))
		if dir == "" {
			dir = "~/.morph"
		}
	}
	dir = pathutil.ExpandHomePath(dir)
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("invalid dir")
	}
	return filepath.Clean(dir), nil
}

func loadConfigExample() (string, error) {
	data, err := assets.ConfigFS.ReadFile("config/config.example.yaml")
	if err != nil {
		return "", fmt.Errorf("read embedded config.example.yaml: %w", err)
	}
	return string(data), nil
}

func loadHeartbeatTemplate() (string, error) {
	data, err := assets.ConfigFS.ReadFile("config/HEARTBEAT.md")
	if err != nil {
		return "", fmt.Errorf("read embedded HEARTBEAT.md: %w", err)
	}
	return string(data), nil
}

func loadIdentityTemplate() (string, error) {
	data, err := assets.ConfigFS.ReadFile("config/persona/identity.yaml")
	if err != nil {
		return "", fmt.Errorf("read embedded identity.yaml: %w", err)
	}
	return string(data), nil
}

func loadCronTemplate() (string, error) {
	data, err := assets.ConfigFS.ReadFile("config/cron.yaml")
	if err != nil {
		return "", fmt.Errorf("read embedded cron.yaml: %w", err)
	}
	return string(data), nil
}

func loadSoulTemplate() (string, error) {
	data, err := assets.ConfigFS.ReadFile("config/persona/soul.md")
	if err != nil {
		return "", fmt.Errorf("read embedded soul.md: %w", err)
	}
	return string(data), nil
}

func patchInitConfigWithSetup(cfg string, dir string, setup *installConfigSetup) (string, error) {
	if strings.TrimSpace(cfg) == "" {
		return cfg, nil
	}
	dir = filepath.Clean(dir)
	dir = filepath.ToSlash(dir)
	rendered, err := configbootstrap.Apply([]byte(cfg), buildInstallBootstrapConfig(dir, setup))
	if err != nil {
		return "", err
	}
	return string(rendered), nil
}

func buildInstallBootstrapConfig(dir string, setup *installConfigSetup) configbootstrap.Config {
	cfg := configbootstrap.Config{
		FileStateDir: dir,
		LLM: configbootstrap.LLMConfig{
			InferenceProvider: "openai",
		},
	}
	if setup == nil {
		return cfg
	}

	cfg.LLM.InferenceProvider = normalizeInferenceProviderForSetup(setup.Provider)
	cfg.LLM.Endpoint = strings.TrimSpace(setup.Endpoint)
	cfg.LLM.Model = strings.TrimSpace(setup.Model)
	switch cfg.LLM.InferenceProvider {
	case setupProviderCloudflare:
		cfg.LLM.CloudflareAccountID = strings.TrimSpace(setup.CloudflareAccount)
		cfg.LLM.CloudflareAPIToken = strings.TrimSpace(setup.CloudflareAPIToken)
	default:
		cfg.LLM.APIKey = strings.TrimSpace(setup.APIKey)
	}

	return cfg
}
