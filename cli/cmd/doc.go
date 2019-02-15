package cmd

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"sigs.k8s.io/yaml"
)

type cmdOption struct {
	Name         string
	Shorthand    string `yaml:",omitempty"`
	DefaultValue string `yaml:"default_value,omitempty"`
	Usage        string `yaml:",omitempty"`
}

type cmdDoc struct {
	Name             string
	Synopsis         string      `yaml:",omitempty"`
	Description      string      `yaml:",omitempty"`
	Options          []cmdOption `yaml:",omitempty"`
	InheritedOptions []cmdOption `yaml:"inherited_options,omitempty"`
	Example          string      `yaml:",omitempty"`
	SeeAlso          []string    `yaml:"see_also,omitempty"`
}

func newCmdDoc() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "doc",
		Hidden: true,
		Short:  "Generate YAML documentation for the Linkerd CLI",
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdList, err := generateDocs(RootCmd)
			if err != nil {
				return err
			}

			out, err := yaml.Marshal(cmdList)
			if err != nil {
				return err
			}

			fmt.Println(string(out))

			return nil
		},
	}

	return cmd
}

// generateDocs takes a command and recursively walks the tree of commands,
// adding each as an item to cmdList.
func generateDocs(cmd *cobra.Command) ([]cmdDoc, error) {
	var cmdList []cmdDoc

	for _, c := range cmd.Commands() {
		if !c.IsAvailableCommand() || c.IsAdditionalHelpTopicCommand() {
			continue
		}

		subList, err := generateDocs(c)
		if err != nil {
			return nil, err
		}

		cmdList = append(cmdList, subList...)
	}

	var buf bytes.Buffer

	if err := doc.GenYaml(cmd, io.Writer(&buf)); err != nil {
		return nil, err
	}

	var doc cmdDoc
	if err := yaml.Unmarshal(buf.Bytes(), &doc); err != nil {
		return nil, err
	}

	// Cobra names start with linkerd, strip that off for the docs.
	doc.Name = strings.TrimPrefix(doc.Name, "linkerd ")

	// Don't include the root command.
	if doc.Name != "linkerd" {
		cmdList = append(cmdList, doc)
	}

	return cmdList, nil
}
