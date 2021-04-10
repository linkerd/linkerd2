package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/k8s/resource"
	mc "github.com/linkerd/linkerd2/pkg/multicluster"
	"github.com/spf13/cobra"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

			return uninstallRunE(cmd.Context(), k8sAPI)
		},
	}

	return cmd
}

func uninstallRunE(ctx context.Context, k8sAPI *k8s.KubernetesAPI) error {

	resources, err := resource.FetchKubernetesResources(ctx, k8sAPI,
		metav1.ListOptions{LabelSelector: "linkerd.io/extension=linkerd-multicluster"},
	)
	if err != nil {
		return err
	}

	if len(resources) == 0 {
		return errors.New("no resources found to uninstall")
	}
	for _, r := range resources {
		if err := r.RenderResource(os.Stdout); err != nil {
			return fmt.Errorf("error rendering Kubernetes resource: %v", err)
		}
	}
	return nil
}
