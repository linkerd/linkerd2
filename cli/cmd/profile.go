package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"

	"github.com/linkerd/linkerd2/cli/profile"
	"github.com/spf13/cobra"
)

type templateConfig struct {
	ControlPlaneNamespace string
	ServiceNamespace      string
	ServiceName           string
}

func newCmdProfile() *cobra.Command {

	var namespace string
	var template bool

	cmd := &cobra.Command{
		Use:   "profile [flags] --template (SERVICE)",
		Short: "Output template service profile config for Kubernetes",
		Long: `Output template service profile config for Kubernetes.
		
		This outputs a service profile template for the given service.  Edit the
		template and then apply it with kubectl to add a service profile to a
		service.

		Example:
		linkerd profile --template web-svc.emojivoto > web-svc-profile.yaml
		# (edit web-svc-profile.yaml manually)
		kubectl apply -f web-svc-profile.yaml
		`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := buildConfig(namespace, args[0])
			if err != nil {
				return err
			}
			return renderProfileTemplate(config, os.Stdout)
		},
	}

	cmd.PersistentFlags().BoolVar(&template, "template", true, "Output a service profile template")
	cmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", namespace, "Namespace of the service")

	return cmd
}

func buildConfig(namespace, service string) (templateConfig, error) {

	serviceParts := strings.Split(service, ".")

	serviceName := serviceParts[0]
	serviceNamespace := "default"

	if len(serviceParts) >= 2 && namespace != "" && serviceParts[1] != namespace {
		return templateConfig{}, fmt.Errorf("service namespace cannot be specified by namespace flag (%s) and by service name (%s)", namespace, serviceParts[1])
	}

	if len(serviceParts) >= 2 {
		serviceNamespace = serviceParts[1]
	}

	if namespace != "" {
		serviceNamespace = namespace
	}
	return templateConfig{
		controlPlaneNamespace,
		serviceNamespace,
		serviceName,
	}, nil
}

func renderProfileTemplate(config templateConfig, w io.Writer) error {
	template, err := template.New("profile").Parse(profile.Template)
	if err != nil {
		return err
	}
	buf := &bytes.Buffer{}
	err = template.Execute(buf, config)
	if err != nil {
		return err
	}

	_, err = w.Write(buf.Bytes())
	return err
}
