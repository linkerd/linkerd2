package cmd

import (
	"fmt"
	"strings"
	"time"

	pkgCmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/cli/values"
)

func newCmdPrune() *cobra.Command {
	var cniEnabled bool
	var wait time.Duration
	var options values.Options

	cmd := &cobra.Command{
		Use:   "prune [flags]",
		Args:  cobra.NoArgs,
		Short: "Output extraneous Kubernetes resources in the linkerd-jaeger extension",
		Long:  `Output extraneous Kubernetes resources in the linkerd-jaeger extension.`,
		Example: `  # Prune extraneous resources.
  linkerd jaeger prune | kubectl delete -f -
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

			err := install(&manifests, options, "", cniEnabled)
			if err != nil {
				return err
			}

			label := fmt.Sprintf("%s=%s", k8s.LinkerdExtensionLabel, JaegerExtensionName)
			return pkgCmd.Prune(cmd.Context(), hc.KubeAPIClient(), manifests.String(), label)
		},
	}

	cmd.Flags().DurationVar(&wait, "wait", 300*time.Second, "Wait for extension components to be available")

	flags.AddValueOptionsFlags(cmd.Flags(), &options)

	return cmd
}
