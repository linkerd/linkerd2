package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/k8s/resource"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	yamlSep = "---\n"
)

func newCmdUninstall() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Args:  cobra.NoArgs,
		Short: "Output Kubernetes resources to uninstall Linkerd control plane",
		Long: `Output Kubernetes resources to uninstall Linkerd control plane.

This command provides all Kubernetes namespace-scoped and cluster-scoped resources (e.g services, deployments, RBACs, etc.) necessary to uninstall Linkerd control plane.`,
		Example: ` linkerd uninstall | kubectl delete -f -`,
		RunE: func(cmd *cobra.Command, args []string) error {

			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			if !force {

				var fail bool
				// Retrtieve any installed extensions
				extensionNamespaces, err := k8sAPI.GetAllNamespacesWithExtensionLabel(cmd.Context())
				if err != nil {
					return err
				}

				// map of the namespace and the extension name
				// Namespace is used as key so as to support custom namespace installs
				extensions := make(map[string]string)
				if len(extensionNamespaces) > 0 {
					for _, extension := range extensionNamespaces {
						extensions[extension.Name] = extension.Labels[k8s.LinkerdExtensionLabel]
					}

					// Retrieve all the extension names
					extensionNames := make([]string, 0, len(extensions))
					for _, v := range extensions {
						extensionNames = append(extensionNames, fmt.Sprintf("* %s", v))
					}

					fmt.Fprintln(os.Stderr, fmt.Sprintf("Please uninstall the following extensions before uninstalling the control-plane:\n\t%s", strings.Join(extensionNames, "\n\t")))
					fail = true
				}

				podList, err := k8sAPI.CoreV1().Pods("").List(cmd.Context(), metav1.ListOptions{LabelSelector: k8s.ControllerNSLabel})
				if err != nil {
					return err
				}

				var injectedPods []string
				for _, pod := range podList.Items {
					// skip core control-plane namespace, and extension namespaces
					if pod.Namespace != controlPlaneNamespace && extensions[pod.Namespace] == "" {
						injectedPods = append(injectedPods, fmt.Sprintf("* %s", pod.Name))
					}
				}

				if len(injectedPods) > 0 {
					fmt.Fprintln(os.Stderr, fmt.Sprintf("Please uninject the following pods before uninstalling the control-plane:\n\t%s", strings.Join(injectedPods, "\n\t")))
					fail = true
				}

				if fail {
					os.Exit(1)
				}
			}

			return uninstallRunE(cmd.Context(), k8sAPI)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", force, "Force uninstall even if there exist non-control-plane injected pods")
	return cmd
}

func uninstallRunE(ctx context.Context, k8sAPI *k8s.KubernetesAPI) error {

	resources, err := resource.FetchKubernetesResources(ctx, k8sAPI,
		metav1.ListOptions{LabelSelector: k8s.ControllerNSLabel},
	)
	if err != nil {
		return err
	}

	if len(resources) == 0 {
		return errors.New("no resources found to uninstall")
	}
	for _, r := range resources {
		if err := r.RenderResource(os.Stdout); err != nil {
			return fmt.Errorf("error rendering Kubernetes resource:%v", err)
		}
	}
	return nil
}
