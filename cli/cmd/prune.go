package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	pkgCmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	valuespkg "helm.sh/helm/v3/pkg/cli/values"
)

func newCmdPrune() *cobra.Command {

	var output string

	cmd := &cobra.Command{
		Use:   "prune [flags]",
		Args:  cobra.NoArgs,
		Short: "Output extraneous Kubernetes resources in the linkerd control plane",
		Long:  `Output extraneous Kubernetes resources in the linkerd control plane.`,
		Example: `  # Prune extraneous resources.
  linkerd prune | kubectl delete -f -
  `,
		RunE: func(cmd *cobra.Command, _ []string) error {

			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 30*time.Second)
			if err != nil {
				return err
			}

			values, err := loadStoredValues(cmd.Context(), k8sAPI)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to load stored values: %s\n", err)
				os.Exit(1)
			}

			if values == nil {
				return errors.New(
					`Could not find the linkerd-config-overrides secret.
Please note this command is only intended for instances of Linkerd that were installed via the CLI`)
			}

			err = validateValues(cmd.Context(), k8sAPI, values)
			if err != nil {
				return err
			}

			manifests := strings.Builder{}

			if err = renderControlPlane(&manifests, values, make(map[string]interface{}), "yaml"); err != nil {
				return err
			}
			if err = renderCRDs(cmd.Context(), k8sAPI, &manifests, valuespkg.Options{}, "yaml"); err != nil {
				return err
			}

			return pkgCmd.Prune(cmd.Context(), k8sAPI, manifests.String(), k8s.ControllerNSLabel, output)
		},
	}

	cmd.PersistentFlags().StringVarP(&output, "output", "o", "yaml", "Output format. One of: json|yaml")

	return cmd
}
