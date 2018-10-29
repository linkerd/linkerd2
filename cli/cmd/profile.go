package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"text/template"

	"github.com/linkerd/linkerd2/cli/profile"
	"github.com/spf13/cobra"
)

type templateConfig struct {
	ControlPlaneNamespace string
	ServiceNamespace      string
	ServiceName           string
	ClusterZone           string
}

type profileOptions struct {
	namespace string
	template  bool
}

func newProfileOptions() *profileOptions {
	return &profileOptions{
		namespace: "default",
		template:  false,
	}
}

func newCmdProfile() *cobra.Command {

	options := newProfileOptions()

	cmd := &cobra.Command{
		Use:   "profile [flags] --template (SERVICE)",
		Short: "Output template service profile config for Kubernetes",
		Long: `Output template service profile config for Kubernetes.
		
		This outputs a service profile template for the given service.  Edit the
		template and then apply it with kubectl to add a service profile to a
		service.

		Example:
		linkerd profile -n emojivoto --template web-svc > web-svc-profile.yaml
		# (edit web-svc-profile.yaml manually)
		kubectl apply -f web-svc-profile.yaml
		`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !options.template {
				return errors.New("only template mode is currently supported, please run with --template")
			}

			return renderProfileTemplate(buildConfig(options.namespace, args[0]), os.Stdout)
		},
	}

	cmd.PersistentFlags().BoolVar(&options.template, "template", options.template, "Output a service profile template")
	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace of the service")

	return cmd
}

func buildConfig(namespace, service string) *templateConfig {
	return &templateConfig{
		ControlPlaneNamespace: controlPlaneNamespace,
		ServiceNamespace:      namespace,
		ServiceName:           service,
		ClusterZone:           "svc.cluster.local",
	}
}

func renderProfileTemplate(config *templateConfig, w io.Writer) error {
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
