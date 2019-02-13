package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

var outputPath string

func newCmdDoc() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "doc",
		Hidden: true,
		Short:  "Generate YAML documentation for the linkerd executable (hidden command)",
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return generateDocs(outputPath)
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output path for the resulting YAML files")

	return cmd
}

func generateDocs(outputPath string) error {
	if outputPath == "" {
		return errors.New("you must specify an output path for the resulting YAML files using the -o or --output flag")
	}

	fmt.Println("Generating YAML docs...")

	err := doc.GenYamlTree(RootCmd, outputPath)

	if err != nil {
		return err
	}

	return nil
}
