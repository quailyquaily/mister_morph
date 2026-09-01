package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "morph %s\n", strings.TrimSpace(version))
			if c := strings.TrimSpace(commit); c != "" && c != "none" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "commit: %s\n", c)
			}
			if d := strings.TrimSpace(date); d != "" && d != "unknown" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "date: %s\n", d)
			}
			return nil
		},
	}
}
