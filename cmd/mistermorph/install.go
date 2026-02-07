package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/assets"
	"github.com/quailyquaily/mistermorph/cmd/mistermorph/skillscmd"
	"github.com/quailyquaily/mistermorph/internal/clifmt"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/maep"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install [dir]",
		Short: "Install config.yaml, HEARTBEAT.md, and built-in skills",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "~/.morph/"
			if len(args) == 1 && strings.TrimSpace(args[0]) != "" {
				dir = args[0]
			}
			dir = pathutil.ExpandHomePath(dir)
			if strings.TrimSpace(dir) == "" {
				return fmt.Errorf("invalid dir")
			}
			dir = filepath.Clean(dir)

			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}

			cfgPath := filepath.Join(dir, "config.yaml")
			writeConfig := true
			if _, err := os.Stat(cfgPath); err == nil {
				fmt.Fprintf(os.Stderr, "warn: config already exists, skipping: %s\n", cfgPath)
				writeConfig = false
			}

			hbPath := filepath.Join(dir, "HEARTBEAT.md")
			writeHeartbeat := true
			if _, err := os.Stat(hbPath); err == nil {
				writeHeartbeat = false
			}

			type initFilePlan struct {
				Name   string
				Path   string
				Write  bool
				Loader func() (string, error)
			}
			filePlans := []initFilePlan{
				{
					Name:  "config.yaml",
					Path:  cfgPath,
					Write: writeConfig,
					Loader: func() (string, error) {
						body, err := loadConfigExample()
						if err != nil {
							return "", err
						}
						return patchInitConfig(body, dir), nil
					},
				},
				{
					Name:   "HEARTBEAT.md",
					Path:   hbPath,
					Write:  writeHeartbeat,
					Loader: loadHeartbeatTemplate,
				},
			}
			totalSkipped := 0
			fmt.Println(clifmt.Headerf("==> Installing required files (%d)", len(filePlans)))
			for i, plan := range filePlans {
				fmt.Printf("[%d/%d] %s (1 file) ... ", i+1, len(filePlans), plan.Name)
				if !plan.Write {
					totalSkipped++
					fmt.Printf("%s %s\n", clifmt.Success("done"), clifmt.Warn("(skipped)"))
					skillscmd.PrintInstalledFileInfos([]skillscmd.InstalledFileInfo{{Path: plan.Path, Skipped: true}})
					continue
				}
				body, err := plan.Loader()
				if err != nil {
					return err
				}
				if err := os.WriteFile(plan.Path, []byte(body), 0o644); err != nil {
					return err
				}
				fmt.Println(clifmt.Success("done"))
				skillscmd.PrintInstalledFileInfos([]skillscmd.InstalledFileInfo{{Path: plan.Path}})
			}
			if totalSkipped > 0 {
				fmt.Printf("%s: %d files %s\n", clifmt.Success("done"), len(filePlans), clifmt.Warn(fmt.Sprintf("(%d skipped)", totalSkipped)))
			} else {
				fmt.Printf("%s: %d files\n", clifmt.Success("done"), len(filePlans))
			}

			identity, created, err := ensureInstallMAEPIdentity(dir)
			if err != nil {
				return fmt.Errorf("initialize maep identity: %w", err)
			}
			if created {
				fmt.Printf("[i] maep identity created: %s\n", identity.PeerID)
			} else {
				fmt.Printf("[i] maep identity exists: %s\n", identity.PeerID)
			}

			skillsDir := filepath.Join(dir, "skills")
			skillDirs, err := skillscmd.DiscoverBuiltInSkills()
			if err != nil {
				return err
			}
			selected, err := skillscmd.SelectBuiltInSkills(skillDirs, false)
			if err != nil {
				return err
			}
			if err := skillscmd.InstallBuiltInSkills(skillsDir, false, false, false, selected); err != nil {
				return err
			}

			fmt.Printf("[i] initialized %s\n", dir)
			return nil
		},
	}

	return cmd
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

func patchInitConfig(cfg string, dir string) string {
	if strings.TrimSpace(cfg) == "" {
		return cfg
	}
	dir = filepath.Clean(dir)
	dir = filepath.ToSlash(dir)
	cfg = strings.ReplaceAll(cfg, `file_state_dir: "~/.morph"`, fmt.Sprintf(`file_state_dir: "%s"`, dir))
	return cfg
}

func ensureInstallMAEPIdentity(dir string) (maep.Identity, bool, error) {
	maepDirName := strings.TrimSpace(viper.GetString("maep.dir_name"))
	if maepDirName == "" {
		maepDirName = "maep"
	}
	maepDir := filepath.Join(dir, maepDirName)
	svc := maep.NewService(maep.NewFileStore(maepDir))
	return svc.EnsureIdentity(context.Background(), time.Now().UTC())
}
