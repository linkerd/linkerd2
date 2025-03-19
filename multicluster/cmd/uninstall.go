package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	pkgCmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
)

func newMulticlusterUninstallCommand() *cobra.Command {
	var output string

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

			links, err := k8sAPI.L5dCrdClient.LinkV1alpha3().Links("").List(cmd.Context(), metav1.ListOptions{})
			if err != nil && !kerrors.IsNotFound(err) {
				return err
			}

			if len(links.Items) > 0 {
				err := []string{"Please unlink the following clusters before uninstalling multicluster:"}
				for _, link := range links.Items {
					err = append(err, fmt.Sprintf("  * %s", link.Spec.TargetClusterName))
				}
				return errors.New(strings.Join(err, "\n"))
			}

			selector, err := pkgCmd.GetLabelSelector(k8s.LinkerdExtensionLabel, MulticlusterExtensionName, MulticlusterLegacyExtension)
			if err != nil {
				return err
			}

			err = pkgCmd.Uninstall(cmd.Context(), k8sAPI, selector, output)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.PersistentFlags().StringVarP(&output, "output", "o", "yaml", "Output format. One of: json|yaml")

	return cmd
}
