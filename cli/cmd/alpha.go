package cmd

import (
	"github.com/spf13/cobra"
)

func newCmdAlpha() *cobra.Command {
	alphaCmd := &cobra.Command{
		Use:   "alpha",
		Short: "experimental subcommands for Linkerd",
		Args:  cobra.NoArgs,
	}

	alphaCmd.AddCommand(newCmdAlphaStat())
	alphaCmd.AddCommand(newCmdAlphaClients())

	return alphaCmd
}
