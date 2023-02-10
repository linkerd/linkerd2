package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	pkgCmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/spf13/cobra"
	valuespkg "helm.sh/helm/v3/pkg/cli/values"
)

func newCmdPrune() *cobra.Command {
	var ha bool
	var cniEnabled bool
	var wait time.Duration
	options, err := newMulticlusterInstallOptionsWithDefault()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var valuesOptions valuespkg.Options

	cmd := &cobra.Command{
		Use:   "prune [flags]",
		Args:  cobra.NoArgs,
		Short: "Output extraneous Kubernetes resources in the linkerd-multicluster extension",
		Long:  `Output extraneous Kubernetes resources in the linkerd-multicluster extension.`,
		Example: `  # Prune extraneous resources.
  linkerd multicluster prune | kubectl delete -f -
  `,
		RunE: func(cmd *cobra.Command, _ []string) error {
			hc := healthcheck.NewWithCoreChecks(&healthcheck.Options{
				ControlPlaneNamespace: controlPlaneNamespace,
				KubeConfig:            kubeconfigPath,
				KubeContext:           kubeContext,
				Impersonate:           impersonate,
				ImpersonateGroup:      impersonateGroup,
				APIAddr:               apiAddr,
				RetryDeadline:         time.Now().Add(wait),
			})
			hc.RunWithExitOnError()
			cniEnabled = hc.CNIEnabled

			manifests := strings.Builder{}

			err := install(cmd.Context(), &manifests, options, valuesOptions, ha, false, cniEnabled)
			if err != nil {
				return err
			}

			return pkgCmd.Prune(cmd.Context(), hc.KubeAPIClient(), manifests.String(), "linkerd.io/extension=multicluster")
		},
	}

	cmd.Flags().BoolVar(&ha, "ha", false, `Set if Linkerd Multicluster Extension is installed in High Availability mode.`)
	cmd.Flags().DurationVar(&wait, "wait", 300*time.Second, "Wait for extension components to be available")

	flags.AddValueOptionsFlags(cmd.Flags(), &valuesOptions)

	return cmd
}
