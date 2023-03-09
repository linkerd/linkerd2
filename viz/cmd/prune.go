package cmd

import (
	"fmt"
	"strings"
	"time"

	pkgCmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	vizHealthcheck "github.com/linkerd/linkerd2/viz/pkg/healthcheck"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/cli/values"
)

func newCmdPrune() *cobra.Command {
	var ha bool
	var cniEnabled bool
	var wait time.Duration
	var options values.Options

	cmd := &cobra.Command{
		Use:   "prune [flags]",
		Args:  cobra.NoArgs,
		Short: "Output extraneous Kubernetes resources in the linkerd-viz extension",
		Long:  `Output extraneous Kubernetes resources in the linkerd-viz extension.`,
		Example: `  # Prune extraneous resources.
  linkerd viz prune | kubectl delete -f -
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

			err := install(&manifests, options, ha, cniEnabled)
			if err != nil {
				return err
			}

			label := fmt.Sprintf("%s=%s", k8s.LinkerdExtensionLabel, vizHealthcheck.VizExtensionName)
			return pkgCmd.Prune(cmd.Context(), hc.KubeAPIClient(), manifests.String(), label)
		},
	}

	cmd.Flags().BoolVar(&ha, "ha", false, `Set if Linkerd Viz Extension is installed in High Availability mode.`)
	cmd.Flags().DurationVar(&wait, "wait", 300*time.Second, "Wait for extension components to be available")

	flags.AddValueOptionsFlags(cmd.Flags(), &options)

	return cmd
}
