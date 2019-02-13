package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"io"
)

type docOptions struct {
	outputPath string
}

func newDocOptions() *docOptions {
	return &docOptions{
		outputPath: "./",
	}
}

func newCmdDoc() *cobra.Command {
	options := newDocOptions()

	cmd := &cobra.Command{
		Use:    "doc",
		Hidden: true,
		Short:  "Generate YAML documentation for the linkerd executable (hidden command)",
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return generateDocs(stdout, options)
		},
	}

	cmd.Flags().StringVarP(&options.outputPath, "output", "o", options.outputPath, "Output path for the resulting YAML file")

	return cmd
}

func generateDocs(w io.Writer, options *docOptions) error {
	fmt.Println("Generating YAML docs...")

	err := doc.GenYamlTree(RootCmd, options.outputPath)

	if err != nil {
		return err
	}

	return nil
}
