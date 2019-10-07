package cmd

import (
	"github.com/spf13/cobra"
)

func newCmdInternal() *cobra.Command {
	root := &cobra.Command{
		Use:   "internal",
		Short: "Used for managing internal linkerd components",
		Long:  `Used for managing internal linkerd components.`,
	}

	root.AddCommand(newCmdDebugSidecar())
	return root
}
