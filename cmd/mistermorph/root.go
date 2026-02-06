package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	envPrefix = "MISTER_MORPH"
)

func Execute() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mistermorph",
		Short: "Unified Agent CLI",
	}

	cobra.OnInitialize(initConfig)

	cmd.PersistentFlags().String("config", "", "Config file path (optional).")
	_ = viper.BindPFlag("config", cmd.PersistentFlags().Lookup("config"))

	// Global logging flags (usable across subcommands like run/serve/telegram).
	cmd.PersistentFlags().String("log-level", "", "Logging level: debug|info|warn|error (defaults to info; debug if --trace).")
	cmd.PersistentFlags().String("log-format", "text", "Logging format: text|json.")
	cmd.PersistentFlags().Bool("log-add-source", false, "Include source file:line in logs.")
	cmd.PersistentFlags().Bool("log-include-thoughts", false, "Include model thoughts in logs (may contain sensitive info).")
	cmd.PersistentFlags().Bool("log-include-tool-params", false, "Include tool params in logs (redacted).")
	cmd.PersistentFlags().Bool("log-include-skill-contents", false, "Include loaded SKILL.md contents in logs (truncated).")
	cmd.PersistentFlags().Int("log-max-thought-chars", 2000, "Max characters of thought to log.")
	cmd.PersistentFlags().Int("log-max-json-bytes", 32768, "Max bytes of JSON params to log.")
	cmd.PersistentFlags().Int("log-max-string-value-chars", 2000, "Max characters per string value in logged params.")
	cmd.PersistentFlags().Int("log-max-skill-content-chars", 8000, "Max characters of SKILL.md content to log.")
	cmd.PersistentFlags().StringArray("log-redact-key", nil, "Extra param keys to redact in logs (repeatable).")
	cmd.PersistentFlags().Bool("trace", false, "Print extra debug info to stderr.")

	_ = viper.BindPFlag("logging.level", cmd.PersistentFlags().Lookup("log-level"))
	_ = viper.BindPFlag("logging.format", cmd.PersistentFlags().Lookup("log-format"))
	_ = viper.BindPFlag("logging.add_source", cmd.PersistentFlags().Lookup("log-add-source"))
	_ = viper.BindPFlag("logging.include_thoughts", cmd.PersistentFlags().Lookup("log-include-thoughts"))
	_ = viper.BindPFlag("logging.include_tool_params", cmd.PersistentFlags().Lookup("log-include-tool-params"))
	_ = viper.BindPFlag("logging.include_skill_contents", cmd.PersistentFlags().Lookup("log-include-skill-contents"))
	_ = viper.BindPFlag("logging.max_thought_chars", cmd.PersistentFlags().Lookup("log-max-thought-chars"))
	_ = viper.BindPFlag("logging.max_json_bytes", cmd.PersistentFlags().Lookup("log-max-json-bytes"))
	_ = viper.BindPFlag("logging.max_string_value_chars", cmd.PersistentFlags().Lookup("log-max-string-value-chars"))
	_ = viper.BindPFlag("logging.max_skill_content_chars", cmd.PersistentFlags().Lookup("log-max-skill-content-chars"))
	_ = viper.BindPFlag("logging.redact_keys", cmd.PersistentFlags().Lookup("log-redact-key"))
	_ = viper.BindPFlag("trace", cmd.PersistentFlags().Lookup("trace"))

	viper.SetDefault("logging.format", "text")
	viper.SetDefault("logging.add_source", false)
	viper.SetDefault("logging.include_thoughts", false)
	viper.SetDefault("logging.include_tool_params", false)
	viper.SetDefault("logging.include_skill_contents", false)
	viper.SetDefault("logging.max_thought_chars", 2000)
	viper.SetDefault("logging.max_json_bytes", 32*1024)
	viper.SetDefault("logging.max_string_value_chars", 2000)
	viper.SetDefault("logging.max_skill_content_chars", 8000)
	viper.SetDefault("trace", false)

	cmd.AddCommand(newRunCmd())
	cmd.AddCommand(newServeCmd())
	cmd.AddCommand(newSubmitCmd())
	cmd.AddCommand(newTelegramCommand())
	cmd.AddCommand(newToolsCmd())
	cmd.AddCommand(newSkillsCmd())
	cmd.AddCommand(newInstallCmd())
	cmd.AddCommand(newVersionCmd())

	return cmd
}

func initConfig() {
	initViperDefaults()

	viper.SetEnvPrefix(envPrefix)
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	viper.AutomaticEnv()

	cfgFile := strings.TrimSpace(viper.GetString("config"))
	if cfgFile == "" {
		return
	}

	viper.SetConfigFile(cfgFile)
	if err := viper.ReadInConfig(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed to read config: %v\n", err)
	}
}
