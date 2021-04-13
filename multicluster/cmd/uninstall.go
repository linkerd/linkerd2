package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	pkgCmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
	mc "github.com/linkerd/linkerd2/pkg/multicluster"
	"github.com/spf13/cobra"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/clientcmd"
)

func newMulticlusterUninstallCommand() *cobra.Command {

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Output Kubernetes configs to uninstall the Linkerd multicluster add-on",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {

			rules := clientcmd.NewDefaultClientConfigLoadingRules()
			rules.ExplicitPath = kubeconfigPath
			loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
			config, err := loader.RawConfig()
			if err != nil {
				return err
			}

			if kubeContext != "" {
				config.CurrentContext = kubeContext
			}

			k8sAPI, err := k8s.NewAPI(kubeconfigPath, config.CurrentContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			links, err := mc.GetLinks(cmd.Context(), k8sAPI.DynamicClient)
			if err != nil && !kerrors.IsNotFound(err) {
				return err
			}

			if len(links) > 0 {
				err := []string{"Please unlink the following clusters before uninstalling multicluster:"}
				for _, link := range links {
					err = append(err, fmt.Sprintf("  * %s", link.TargetClusterName))
				}
				return errors.New(strings.Join(err, "\n"))
			}

			err = pkgCmd.Uninstall(cmd.Context(), k8sAPI, fmt.Sprintf("%s=%s", k8s.LinkerdExtensionLabel, MulticlusterExtensionName))
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return nil
		},
	}

	return cmd
}
