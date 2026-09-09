package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestConsoleDefaultsToServe(t *testing.T) {
	for _, args := range [][]string{
		{"console", "--console-listen", "127.0.0.1:9081", "--allow-empty-password"},
		{"console", "serve", "--console-listen", "127.0.0.1:9081", "--allow-empty-password"},
	} {
		t.Run(strings.Join(args[:2], " "), func(t *testing.T) {
			resetRootConfigForTest(t)
			root := rootCommandForTest(t)
			console, _, _ := root.Find([]string{"console"})
			serve, _, _ := root.Find([]string{"console", "serve"})
			if console.RunE == nil || reflect.ValueOf(console.RunE).Pointer() != reflect.ValueOf(serve.RunE).Pointer() {
				t.Fatal("console must reuse the serve handler")
			}
			serve.Flags().VisitAll(func(flag *pflag.Flag) {
				if console.Flags().Lookup(flag.Name) != flag {
					t.Errorf("console must share serve flag %q", flag.Name)
				}
			})
			path := writeMalformedConfig(t)
			root.SetArgs(append([]string{"--config", path}, args...))
			var output bytes.Buffer
			root.SetOut(&output)
			root.SetErr(&output)
			ran := false
			run := func(cmd *cobra.Command, _ []string) error {
				ran = true
				listen, _ := cmd.Flags().GetString("console-listen")
				allow, _ := cmd.Flags().GetBool("allow-empty-password")
				if listen != "127.0.0.1:9081" || !allow {
					t.Errorf("flags = %q, %v", listen, allow)
				}
				if !isConsoleRepairCommand(cmd) || !shouldCheckOSSecretStore(cmd) || shouldPrepareRootRegistry(cmd) {
					t.Error("incorrect console preflight policy")
				}
				return nil
			}
			console.RunE, serve.RunE = run, run
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if !ran || !strings.Contains(output.String(), "repair mode") {
				t.Fatalf("console did not run in repair mode: %s", output.String())
			}
		})
	}
}

func TestConsoleHelpAndUnknownCommand(t *testing.T) {
	for _, arg := range []string{"--help", "serv"} {
		t.Run(arg, func(t *testing.T) {
			resetRootConfigForTest(t)
			root := rootCommandForTest(t)
			root.SetArgs([]string{"console", arg})
			var output bytes.Buffer
			root.SetOut(&output)
			root.SetErr(&output)
			root.PersistentPreRunE = func(*cobra.Command, []string) error {
				t.Fatal("help and invalid commands must not start console preflight")
				return nil
			}
			err := root.Execute()
			if arg == "--help" {
				if err != nil || !strings.Contains(output.String(), "--console-listen") {
					t.Fatalf("help error = %v, output = %s", err, output.String())
				}
			} else if err == nil || !strings.Contains(err.Error(), "unknown command") {
				t.Fatalf("error = %v, want unknown command", err)
			}
		})
	}
}
