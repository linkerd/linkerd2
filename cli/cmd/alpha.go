package cmd

import (
	"github.com/spf13/cobra"
)

// newCmdAlpha creates a new cobra command `alpha` which is used by experimental subcommands
// for linkerd
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
