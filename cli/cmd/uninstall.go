package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/k8s/resource"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	yamlSep = "---\n"
)

func newCmdUninstall() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Args:  cobra.NoArgs,
		Short: "Output Kubernetes resources to uninstall Linkerd control plane",
		Long: `Output Kubernetes resources to uninstall Linkerd control plane.

This command provides all Kubernetes namespace-scoped and cluster-scoped resources (e.g services, deployments, RBACs, etc.) necessary to uninstall Linkerd control plane.`,
		Example: ` linkerd uninstall | kubectl delete -f -`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return uninstallRunE(cmd.Context())
		},
	}

	return cmd
}

func uninstallRunE(ctx context.Context) error {
	k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
	if err != nil {
		return err
	}

	resources, err := resource.FetchKubernetesResources(ctx, k8sAPI,
		metav1.ListOptions{LabelSelector: k8s.ControllerNSLabel},
	)
	if err != nil {
		return err
	}

	for _, r := range resources {
		if err := r.RenderResource(os.Stdout); err != nil {
			return fmt.Errorf("error rendering Kubernetes resource:%v", err)
		}
	}
	return nil
}
