package main

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestRootReusesChat(t *testing.T) {
	resetRootConfigForTest(t)
	root := rootCommandForTest(t)
	chat, _, err := root.Find([]string{"chat"})
	if err != nil {
		t.Fatal(err)
	}
	if root.RunE == nil || reflect.ValueOf(root.RunE).Pointer() != reflect.ValueOf(chat.RunE).Pointer() {
		t.Fatal("root must reuse the chat handler")
	}
	chat.Flags().VisitAll(func(flag *pflag.Flag) {
		if root.Flags().Lookup(flag.Name) != flag {
			t.Errorf("root must share chat flag %q", flag.Name)
		}
	})
	if !shouldPrepareRootRegistry(root) || !shouldCheckOSSecretStore(root) {
		t.Fatal("default chat must prepare tools and check the secret store")
	}
	if shouldRunRuntimeFilePreflight(root) != shouldRunRuntimeFilePreflight(chat) {
		t.Fatal("default and explicit chat must use the same file preflight policy")
	}
}

func TestRootDefaultChatDispatch(t *testing.T) {
	for _, tt := range []struct {
		name       string
		args       []string
		wantRun    string
		wantModel  string
		wantConfig string
		wantError  string
		wantHelp   bool
	}{
		{name: "bare", args: []string{}, wantRun: "morph"},
		{name: "global flag", args: []string{"--config", "custom.yaml"}, wantRun: "morph", wantConfig: "custom.yaml"},
		{name: "chat flags", args: []string{"--model", "test-model", "--no-workspace"}, wantRun: "morph", wantModel: "test-model"},
		{name: "command as flag value", args: []string{"--model", "version"}, wantRun: "morph", wantModel: "version"},
		{name: "explicit chat", args: []string{"--config", "custom.yaml", "chat", "--model", "test-model"}, wantRun: "morph chat", wantModel: "test-model", wantConfig: "custom.yaml"},
		{name: "other command", args: []string{"version"}, wantRun: "morph version"},
		{name: "help", args: []string{"--help"}, wantHelp: true},
		{name: "short help", args: []string{"-h"}, wantHelp: true},
		{name: "help command", args: []string{"help"}, wantHelp: true},
		{name: "unknown command", args: []string{"caht"}, wantError: "unknown command"},
		{name: "unknown flag", args: []string{"--unknown"}, wantError: "unknown flag"},
		{name: "missing flag value", args: []string{"--model"}, wantError: "flag needs an argument"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetRootConfigForTest(t)
			root := rootCommandForTest(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var output bytes.Buffer
			root.SetOut(&output)
			root.SetErr(&output)
			root.SilenceErrors = true
			root.SilenceUsage = true
			root.SetArgs(tt.args)
			preflight := ""
			root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
				preflight = cmd.CommandPath()
				return nil
			}
			ran := ""
			run := func(cmd *cobra.Command, _ []string) error {
				ran = cmd.CommandPath()
				if cmd.Context() != ctx {
					t.Error("command lost the execution context")
				}
				if tt.wantModel != "" {
					got, _ := cmd.Flags().GetString("model")
					if got != tt.wantModel || !cmd.Flags().Changed("model") {
						t.Errorf("model = %q, want explicit %q", got, tt.wantModel)
					}
				}
				if tt.wantConfig != "" {
					got, _ := cmd.Flags().GetString("config")
					if got != tt.wantConfig {
						t.Errorf("config = %q, want %q", got, tt.wantConfig)
					}
				}
				return nil
			}
			if root.RunE != nil {
				root.RunE = run
			}
			for _, name := range []string{"chat", "version"} {
				cmd, _, err := root.Find([]string{name})
				if err != nil {
					t.Fatal(err)
				}
				cmd.RunE = run
			}
			err := root.ExecuteContext(ctx)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error = %v, want %q", err, tt.wantError)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			wantPreflight := tt.wantRun
			if tt.name == "help command" {
				wantPreflight = "morph help"
			}
			if ran != tt.wantRun || preflight != wantPreflight {
				t.Errorf("run = %q, preflight = %q, want %q, %q", ran, preflight, tt.wantRun, wantPreflight)
			}
			if tt.wantHelp && !strings.Contains(output.String(), "Available Commands:") {
				t.Errorf("missing root help: %s", output.String())
			}
		})
	}
}

func TestDefaultChatRejectsMalformedConfig(t *testing.T) {
	resetRootConfigForTest(t)
	path := writeMalformedConfig(t)
	root := rootCommandForTest(t)
	root.SetArgs([]string{"--config", path})
	root.SilenceErrors = true
	root.SilenceUsage = true
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("error = %v, want config error for %q", err, path)
	}
}
