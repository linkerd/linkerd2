package cmd

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/profiles"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/validation"
)

type profileOptions struct {
	name          string
	namespace     string
	template      bool
	openAPI       string
	proto         string
	tap           string
	tapDuration   time.Duration
	tapRouteLimit uint
}

func newProfileOptions() *profileOptions {
	return &profileOptions{
		name:          "",
		namespace:     "default",
		template:      false,
		openAPI:       "",
		proto:         "",
		tap:           "",
		tapDuration:   5 * time.Second,
		tapRouteLimit: 20,
	}
}

func (options *profileOptions) validate() error {
	outputs := 0
	if options.template {
		outputs++
	}
	if options.openAPI != "" {
		outputs++
	}
	if options.proto != "" {
		outputs++
	}
	if options.tap != "" {
		outputs++
	}
	if outputs != 1 {
		return errors.New("You must specify exactly one of --template or --open-api or --proto or --tap")
	}

	// a DNS-1035 label must consist of lower case alphanumeric characters or '-',
	// start with an alphabetic character, and end with an alphanumeric character
	if errs := validation.IsDNS1035Label(options.name); len(errs) != 0 {
		return fmt.Errorf("invalid service %q: %v", options.name, errs)
	}

	// a DNS-1123 label must consist of lower case alphanumeric characters or '-',
	// and must start and end with an alphanumeric character
	if errs := validation.IsDNS1123Label(options.namespace); len(errs) != 0 {
		return fmt.Errorf("invalid namespace %q: %v", options.namespace, errs)
	}

	return nil
}

// NewCmdProfile creates a new cobra command for the Profile subcommand which
// generates Linkerd service profiles.
func newCmdProfile() *cobra.Command {
	options := newProfileOptions()

	cmd := &cobra.Command{
		Use:   "profile [flags] (--template | --open-api file | --proto file | --tap resource) (SERVICE)",
		Short: "Output service profile config for Kubernetes",
		Long:  "Output service profile config for Kubernetes.",
		Example: `  # Output a basic template to apply after modification.
  linkerd profile -n emoijvoto --template web-svc

  # Generate a profile from an OpenAPI specification.
  linkerd profile -n emojivoto --open-api web-svc.swagger web-svc

  # Generate a profile from a protobuf definition.
  linkerd profile -n emojivoto --proto Voting.proto vote-svc

  # Generate a profile by watching live traffic based off tap data.
  linkerd profile -n emojivoto web-svc --tap deploy/web --tap-duration 10s --tap-route-limit 5
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.name = args[0]

			err := options.validate()
			if err != nil {
				return err
			}

			if options.template {
				return profiles.RenderProfileTemplate(options.namespace, options.name, defaultClusterDomain, os.Stdout)
			} else if options.openAPI != "" {
				return profiles.RenderOpenAPI(options.openAPI, options.namespace, defaultClusterDomain, options.name, os.Stdout)
			} else if options.tap != "" {
				k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, 0)
				if err != nil {
					return err
				}
				return profiles.RenderTapOutputProfile(k8sAPI, options.tap, options.namespace, options.name, defaultClusterDomain, options.tapDuration, int(options.tapRouteLimit), os.Stdout)
			} else if options.proto != "" {
				return profiles.RenderProto(options.proto, options.namespace, options.name, defaultClusterDomain, os.Stdout)
			}

			// we should never get here
			return errors.New("Unexpected error")
		},
	}

	cmd.PersistentFlags().BoolVar(&options.template, "template", options.template, "Output a service profile template")
	cmd.PersistentFlags().StringVar(&options.openAPI, "open-api", options.openAPI, "Output a service profile based on the given OpenAPI spec file")
	cmd.PersistentFlags().StringVar(&options.tap, "tap", options.tap, "Output a service profile based on tap data for the given target resource")
	cmd.PersistentFlags().DurationVar(&options.tapDuration, "tap-duration", options.tapDuration, "Duration over which tap data is collected (for example: \"10s\", \"1m\", \"10m\")")
	cmd.PersistentFlags().UintVar(&options.tapRouteLimit, "tap-route-limit", options.tapRouteLimit, "Max number of routes to add to the profile")
	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace of the service")
	cmd.PersistentFlags().StringVar(&options.proto, "proto", options.proto, "Output a service profile based on the given Protobuf spec file")

	return cmd
}
