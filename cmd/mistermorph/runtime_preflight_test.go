package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestShouldRunRuntimeFilePreflightForMixin(t *testing.T) {
	root := &cobra.Command{Use: "morph"}
	command := &cobra.Command{Use: "mixin"}
	root.AddCommand(command)
	if !shouldRunRuntimeFilePreflight(command) {
		t.Fatal("Mixin runtime skipped file preflight")
	}
	if got := command.CommandPath(); !strings.HasSuffix(got, " mixin") {
		t.Fatalf("CommandPath() = %q", got)
	}
}

func TestShouldCheckOSSecretStoreForRuntimeCommands(t *testing.T) {
	for _, path := range []string{"run", "chat", "telegram", "console serve"} {
		t.Run(path, func(t *testing.T) {
			root := &cobra.Command{Use: "morph"}
			var command *cobra.Command
			if path == "console serve" {
				console := &cobra.Command{Use: "console"}
				command = &cobra.Command{Use: "serve"}
				console.AddCommand(command)
				root.AddCommand(console)
			} else {
				command = &cobra.Command{Use: path}
				root.AddCommand(command)
			}
			if !shouldCheckOSSecretStore(command) {
				t.Fatalf("shouldCheckOSSecretStore(%q) = false", command.CommandPath())
			}
		})
	}
}

func TestShouldNotCheckOSSecretStoreForNonRuntimeCommands(t *testing.T) {
	root := &cobra.Command{Use: "morph"}
	command := &cobra.Command{Use: "version"}
	root.AddCommand(command)
	if shouldCheckOSSecretStore(command) {
		t.Fatal("version command unexpectedly checks the OS secret store")
	}
}

func TestRootCommandUsesReleaseExecutableName(t *testing.T) {
	runtime := newRootRuntime()
	t.Cleanup(func() { _ = runtime.Close() })
	if got := runtime.command.Name(); got != "morph" {
		t.Fatalf("root command name = %q, want morph", got)
	}
}
