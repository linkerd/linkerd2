package cmd

import (
	"context"
	"fmt"
	"os"

	pkgCmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/k8s/resource"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newCmdUninstall() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Args:  cobra.NoArgs,
		Short: "Output Kubernetes resources to uninstall the linkerd-viz extension",
		Long: `Output Kubernetes resources to uninstall the linkerd-viz extension.

This command provides all Kubernetes namespace-scoped and cluster-scoped resources (e.g services, deployments, RBACs, etc.) necessary to uninstall the Linkerd-viz extension.`,
		Example: `linkerd viz uninstall | kubectl delete -f -`,
		RunE: func(cmd *cobra.Command, args []string) error {
			err := uninstallRunE(cmd.Context())
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return nil
		},
	}

	return cmd
}

func uninstallRunE(ctx context.Context) error {
	k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
	if err != nil {
		return err
	}

	selector, err := pkgCmd.GetLabelSelector(k8s.LinkerdExtensionLabel, ExtensionName, LegacyExtensionName)
	if err != nil {
		return err
	}

	if err := pkgCmd.Uninstall(ctx, k8sAPI, selector); err != nil {
		return err
	}

	// delete any HTTPRoute, AuthorizationPolicy, and Server resources created
	// by the viz extension in any namespace
	nses, err := k8sAPI.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	policy := k8sAPI.L5dCrdClient.PolicyV1alpha1()
	for _, ns := range nses.Items {
		authzs, err := policy.AuthorizationPolicies(ns.GetName()).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return err
		}

		for _, authz := range authzs.Items {
			if err := deleteResource(authz.TypeMeta, authz.ObjectMeta); err != nil {
				return err
			}
		}

		rts, err := policy.HTTPRoutes(ns.GetName()).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return err
		}

		for _, rt := range rts.Items {
			if err := deleteResource(rt.TypeMeta, rt.ObjectMeta); err != nil {
				return err
			}
		}

		srvs, err := k8sAPI.L5dCrdClient.ServerV1beta1().Servers(ns.GetName()).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return err
		}

		for _, srv := range srvs.Items {
			if err := deleteResource(srv.TypeMeta, srv.ObjectMeta); err != nil {
				return err
			}
		}
	}

	return nil
}

func deleteResource(ty metav1.TypeMeta, meta metav1.ObjectMeta) error {
	r := resource.NewNamespaced(ty.APIVersion, ty.Kind, meta.Name, meta.Namespace)
	if err := r.RenderResource(os.Stdout); err != nil {
		return fmt.Errorf("error rendering Kubernetes resource: %w", err)
	}
	return nil
}
