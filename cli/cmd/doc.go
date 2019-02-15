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

var cmdList []cmdDoc

func newCmdDoc() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "doc",
		Hidden: true,
		Short:  "Generate YAML documentation for the Linkerd CLI",
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := generateDocs(RootCmd); err != nil {
				return err
			}

			out, err := yaml.Marshal(cmdList)
			if err != nil {
				return err
			}

			fmt.Println(fmt.Sprintf("%s", out))

			return nil
		},
	}

	return cmd
}

// generateDocs takes a command and recursively walks the tree of commands,
// adding each as an item to cmdList.
func generateDocs(cmd *cobra.Command) error {
	for _, c := range cmd.Commands() {
		if !c.IsAvailableCommand() || c.IsAdditionalHelpTopicCommand() {
			continue
		}

		if err := generateDocs(c); err != nil {
			return err
		}
	}

	var buf bytes.Buffer

	if err := doc.GenYaml(cmd, io.Writer(&buf)); err != nil {
		return err
	}

	var doc cmdDoc
	if err := yaml.Unmarshal(buf.Bytes(), &doc); err != nil {
		return err
	}

	// Cobra names start with linkerd, strip that off for the docs.
	doc.Name = strings.Replace(doc.Name, "linkerd ", "", 1)

	// Don't include the root command.
	if doc.Name != "linkerd" {
		cmdList = append(cmdList, doc)
	}

	return nil
}
