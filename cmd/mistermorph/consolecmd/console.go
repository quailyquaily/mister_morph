package consolecmd

import "github.com/spf13/cobra"

func New(version ...string) *cobra.Command {
	buildVersion := ""
	if len(version) > 0 {
		buildVersion = version[0]
	}
	cmd := &cobra.Command{
		Use:   "console",
		Short: "Run console HTTP APIs and SPA",
	}
	cmd.AddCommand(newServeCmd(buildVersion))
	return cmd
}
