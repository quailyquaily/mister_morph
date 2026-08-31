package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/quailyquaily/mistermorph/internal/onboardingcheck"
	"github.com/quailyquaily/mistermorph/internal/secref"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/spf13/cobra"
)

func runRootPreflight(cmd *cobra.Command, _ []string) error {
	if shouldCheckOSSecretStore(cmd) {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		if err := secref.CheckOSStore(ctx, secref.NewOSStore()); err != nil {
			slog.Warn("os_secret_store_unavailable", "error", err)
		}
	}
	if err := loadRootConfig(cmd); err != nil {
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

func shouldCheckOSSecretStore(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	switch cmd.CommandPath() {
	case "mistermorph run", "mistermorph chat", "mistermorph telegram", "mistermorph slack", "mistermorph line", "mistermorph lark", "mistermorph mixin", "mistermorph console serve":
		return true
	default:
		return false
	}
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
