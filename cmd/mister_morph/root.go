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
		Use:   "mister_morph",
		Short: "Unified Agent CLI",
	}

	cobra.OnInitialize(initConfig)

	cmd.PersistentFlags().String("config", "", "Config file path (optional).")
	_ = viper.BindPFlag("config", cmd.PersistentFlags().Lookup("config"))

	cmd.AddCommand(newRunCmd())
	cmd.AddCommand(newServeCmd())
	cmd.AddCommand(newSubmitCmd())
	cmd.AddCommand(newTelegramCmd())
	cmd.AddCommand(newToolsCmd())
	cmd.AddCommand(newSkillsCmd())
	cmd.AddCommand(newVersionCmd())

	return cmd
}

func initConfig() {
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
