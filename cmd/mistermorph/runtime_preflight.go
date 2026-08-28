package main

import (
	"fmt"
	"io"

	"github.com/quailyquaily/mistermorph/internal/onboardingcheck"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/spf13/cobra"
)

func runRootPreflight(cmd *cobra.Command, _ []string) error {
	if err := loadRootConfig(); err != nil {
		if !isConsoleRepairCommand(cmd) {
			return err
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warn: console repair mode: %v; using defaults-only config until repaired\n", err)
	}
	if !shouldRunRuntimeFilePreflight(cmd) {
		return nil
	}
	return runRuntimeFilePreflight(cmd.ErrOrStderr())
}

func isConsoleRepairCommand(cmd *cobra.Command) bool {
	return cmd != nil && cmd.CommandPath() == "mistermorph console serve"
}

func shouldRunRuntimeFilePreflight(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	switch cmd.CommandPath() {
	case "mistermorph run", "mistermorph telegram", "mistermorph slack", "mistermorph line", "mistermorph lark", "mistermorph mixin":
		return true
	default:
		return false
	}
}

func runRuntimeFilePreflight(stderr io.Writer) error {
	cfgFile, _ := resolveConfigFile()
	if cfgFile != "" {
		item := onboardingcheck.InspectConfigPath(cfgFile)
		if item.IsBroken() {
			return fmt.Errorf("%s is malformed: %s", item.Name, item.Error)
		}
	}
	for _, item := range []onboardingcheck.Item{
		onboardingcheck.InspectIdentityYAMLPath(statepaths.PersonaIdentityPath()),
		onboardingcheck.InspectSoulPath(statepaths.PersonaSoulPath()),
	} {
		if !item.IsBroken() {
			continue
		}
		_, _ = fmt.Fprintf(stderr, "warn: %s is %s: %s\n", item.Name, item.Status, item.Error)
	}
	return nil
}
