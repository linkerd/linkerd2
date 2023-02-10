package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	pkgCmd "github.com/linkerd/linkerd2/pkg/cmd"
	flagspkg "github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	valuespkg "helm.sh/helm/v3/pkg/cli/values"
)

func newCmdPrune() *cobra.Command {
	values, err := l5dcharts.NewValues()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	var options valuespkg.Options

	installOnlyFlags, installOnlyFlagSet := makeInstallFlags(values)
	installUpgradeFlags, installUpgradeFlagSet, err := makeInstallUpgradeFlags(values)
	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
	}
	proxyFlags, proxyFlagSet := makeProxyFlags(values)

	flags := flattenFlags(installOnlyFlags, installUpgradeFlags, proxyFlags)

	cmd := &cobra.Command{
		Use:   "prune [flags]",
		Args:  cobra.NoArgs,
		Short: "Output extraneous Kubernetes resources in the linkerd control plane",
		Long:  `Output extraneous Kubernetes resources in the linkerd control plane.`,
		Example: `  # Prune extraneous resources.
  linkerd prune | kubectl delete -f -
  `,
		RunE: func(cmd *cobra.Command, _ []string) error {

			manifests := strings.Builder{}

			err := installControlPlane(cmd.Context(), nil, &manifests, values, flags, options)
			if err != nil {
				return err
			}

			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 30*time.Second)
			if err != nil {
				return err
			}

			return pkgCmd.Prune(cmd.Context(), k8sAPI, manifests.String(), k8s.ControllerNSLabel)
		},
	}

	cmd.Flags().AddFlagSet(installOnlyFlagSet)
	cmd.Flags().AddFlagSet(installUpgradeFlagSet)
	cmd.Flags().AddFlagSet(proxyFlagSet)
	flagspkg.AddValueOptionsFlags(cmd.Flags(), &options)

	return cmd
}
