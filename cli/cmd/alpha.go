package cmd

import (
	"github.com/spf13/cobra"
)

// newCmdAlpha creates a new cobra command for the `alpha` command which are
// used by experimental subcommands for Linkerd
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
