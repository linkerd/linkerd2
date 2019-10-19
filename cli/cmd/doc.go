package cmd

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	cobradoc "github.com/spf13/cobra/doc"
	"sigs.k8s.io/yaml"

	"github.com/linkerd/linkerd2/pkg/k8s"
)

type references struct {
	CLIReference         []cmdDoc
	AnnotationsReference []annotationDoc
}

type cmdOption struct {
	Name         string
	Shorthand    string
	DefaultValue string
	Usage        string
}

type cmdDoc struct {
	Name             string
	Synopsis         string
	Description      string
	Options          []cmdOption
	InheritedOptions []cmdOption
	Example          string
	SeeAlso          []string
}

type annotationDoc struct {
	Name        string
	Description string
}

func newCmdDoc() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "doc",
		Hidden: true,
		Short:  "Generate YAML documentation for the Linkerd CLI & Proxy annotations",
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdList, err := generateCLIDocs(RootCmd)
			if err != nil {
				return err
			}

			annotations := generateAnnotationsDocs(k8s.GetAnnotationsDocs(), cmdList)

			ref := references{
				CLIReference:         cmdList,
				AnnotationsReference: annotations,
			}
			out, err := yaml.Marshal(ref)
			if err != nil {
				return err
			}

			fmt.Println(string(out))

			return nil
		},
	}

	return cmd
}

// generateCliDocs takes a command and recursively walks the tree of commands,
// adding each as an item to cmdList.
func generateCLIDocs(cmd *cobra.Command) ([]cmdDoc, error) {
	var cmdList []cmdDoc

	for _, c := range cmd.Commands() {
		if !c.IsAvailableCommand() || c.IsAdditionalHelpTopicCommand() {
			continue
		}

		subList, err := generateCLIDocs(c)
		if err != nil {
			return nil, err
		}

		cmdList = append(cmdList, subList...)
	}

	var buf bytes.Buffer

	if err := cobradoc.GenYaml(cmd, io.Writer(&buf)); err != nil {
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

// generateAnnotationsDocs takes a labels file, parse it, iterate over nodes,
// recognize annotations, adding them to annotationsList.
func generateAnnotationsDocs(annotationsMap map[string]string, cmdList []cmdDoc) []annotationDoc {
	var annotationsList []annotationDoc

	for _, cmd := range cmdList {
		for _, flag := range cmd.Options {
			annotationName := fmt.Sprintf("%s/%s", k8s.ProxyConfigAnnotationsPrefix, flag.Name)
			desc, ok := annotationsMap[annotationName]
			if !ok {
				continue
			}

			if desc != "" {
				continue
			}

			annotation := annotationDoc{
				Name:        annotationName,
				Description: flag.Usage,
			}

			annotationsList = append(annotationsList, annotation)
			delete(annotationsMap, annotationName)
		}
	}

	for name, desc := range annotationsMap {
		annotation := annotationDoc{
			Name:        name,
			Description: desc,
		}

		annotationsList = append(annotationsList, annotation)
	}

	return annotationsList
}
