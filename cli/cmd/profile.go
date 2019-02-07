package cmd

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/linkerd/linkerd2/pkg/profiles"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/validation"
)

type templateConfig struct {
	ControlPlaneNamespace string
	ServiceNamespace      string
	ServiceName           string
	ClusterZone           string
}

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
		Long: `Output service profile config for Kubernetes.

This outputs a service profile for the given service.

Examples:
  If the --template flag is specified, it outputs a service profile template.
  Edit the template and then apply it with kubectl to add a service profile to
  a service:

  linkerd profile -n emojivoto --template web-svc > web-svc-profile.yaml
  # (edit web-svc-profile.yaml manually)
  kubectl apply -f web-svc-profile.yaml

  If the --open-api flag is specified, it reads the given OpenAPI
  specification file and outputs a corresponding service profile:

  linkerd profile -n emojivoto --open-api web-svc.swagger web-svc | kubectl apply -f -

  If the --proto flag is specified, it reads the given protobuf definition file
  and outputs a corresponding service profile:

  linkerd profile -n emojivoto --proto Voting.proto vote-svc | kubectl apply -f -

  If the --tap flag is specified, it runs linkerd tap target for --tap-duration seconds,
  and creates a profile for the SERVICE based on the requests seen in that window:

  linkerd profile books --tap deploy/books --tap-duration 10s --tap-route-limit 5 > book-svc-profile.yaml
  # (edit book-svc-profile.yaml manually)
  kubectl apply -f book-svc-profile.yaml

  The command will run linkerd tap deploy/books for tap-duration seconds, and then create
  a service profile for the books service with routes prepopulated from the tap data.
  For high RPS, high-route-cardinality services, use tap-route-limit to limit the number of
  routes in the output profile.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.name = args[0]

			err := options.validate()
			if err != nil {
				return err
			}

			if options.template {
				return profiles.RenderProfileTemplate(options.namespace, options.name, os.Stdout)
			} else if options.openAPI != "" {
				return profiles.RenderOpenAPI(options.openAPI, options.namespace, options.name, os.Stdout)
			} else if options.tap != "" {
				return profiles.RenderTapOutputProfile(cliPublicAPIClient(), options.tap, options.namespace, options.name, options.tapDuration, int(options.tapRouteLimit), os.Stdout)
			} else if options.proto != "" {
				return profiles.RenderProto(options.proto, options.namespace, options.name, os.Stdout)
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
