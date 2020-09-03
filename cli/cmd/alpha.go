package cmd

import (
	"github.com/spf13/cobra"
)

// newCmdAlpha creates a new cobra command `alpha` which contains experimental subcommands for linkerd
func newCmdAlpha() *cobra.Command {
	alphaCmd := &cobra.Command{
		Use:   "alpha",
		Short: "experimental subcommands for Linkerd",
		Args:  cobra.NoArgs,
	}

	return alphaCmd
}
