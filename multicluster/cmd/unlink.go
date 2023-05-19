package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/k8s/resource"
	mc "github.com/linkerd/linkerd2/pkg/multicluster"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
)

func newUnlinkCommand() *cobra.Command {
	opts, err := newLinkOptionsWithDefault()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	cmd := &cobra.Command{
		Use:   "unlink",
		Short: "Outputs link resources for deletion",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {

			if opts.clusterName == "" {
				return errors.New("You need to specify cluster name")
			}

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

			k, err := k8s.NewAPI(kubeconfigPath, config.CurrentContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			_, err = mc.GetLink(cmd.Context(), k.DynamicClient, opts.namespace, opts.clusterName)
			if err != nil {
				return err
			}

			secret := resource.NewNamespaced(corev1.SchemeGroupVersion.String(), "Secret", fmt.Sprintf("cluster-credentials-%s", opts.clusterName), opts.namespace)
			gatewayMirror := resource.NewNamespaced(corev1.SchemeGroupVersion.String(), "Service", fmt.Sprintf("probe-gateway-%s", opts.clusterName), opts.namespace)
			link := resource.NewNamespaced(k8s.LinkAPIGroupVersion, "Link", opts.clusterName, opts.namespace)
			clusterRole := resource.New(rbac.SchemeGroupVersion.String(), "ClusterRole", fmt.Sprintf("linkerd-service-mirror-access-local-resources-%s", opts.clusterName))
			clusterRoleBinding := resource.New(rbac.SchemeGroupVersion.String(), "ClusterRoleBinding", fmt.Sprintf("linkerd-service-mirror-access-local-resources-%s", opts.clusterName))
			role := resource.NewNamespaced(rbac.SchemeGroupVersion.String(), "Role", fmt.Sprintf("linkerd-service-mirror-read-remote-creds-%s", opts.clusterName), opts.namespace)
			roleBinding := resource.NewNamespaced(rbac.SchemeGroupVersion.String(), "RoleBinding", fmt.Sprintf("linkerd-service-mirror-read-remote-creds-%s", opts.clusterName), opts.namespace)
			serviceAccount := resource.NewNamespaced(corev1.SchemeGroupVersion.String(), "ServiceAccount", fmt.Sprintf("linkerd-service-mirror-%s", opts.clusterName), opts.namespace)
			serviceMirror := resource.NewNamespaced(appsv1.SchemeGroupVersion.String(), "Deployment", fmt.Sprintf("linkerd-service-mirror-%s", opts.clusterName), opts.namespace)

			resources := []resource.Kubernetes{
				secret, gatewayMirror, link, clusterRole, clusterRoleBinding,
				role, roleBinding, serviceAccount, serviceMirror,
			}

			selector := fmt.Sprintf("%s=%s,%s=%s",
				k8s.MirroredResourceLabel, "true",
				k8s.RemoteClusterNameLabel, opts.clusterName,
			)
			svcList, err := k.CoreV1().Services(metav1.NamespaceAll).List(cmd.Context(), metav1.ListOptions{LabelSelector: selector})
			if err != nil {
				return err
			}
			for _, svc := range svcList.Items {
				resources = append(resources,
					resource.NewNamespaced(corev1.SchemeGroupVersion.String(), "Service", svc.Name, svc.Namespace),
				)
			}

			for _, r := range resources {
				if err := r.RenderResource(stdout); err != nil {
					log.Errorf("failed to render resource %s: %s", r.Name, err)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&opts.namespace, "namespace", defaultMulticlusterNamespace, "The namespace for the service account")
	cmd.Flags().StringVar(&opts.clusterName, "cluster-name", "", "Cluster name")

	pkgcmd.ConfigureNamespaceFlagCompletion(
		cmd, []string{"namespace"},
		kubeconfigPath, impersonate, impersonateGroup, kubeContext)

	configureClusterNameFlagCompletion(cmd)
	return cmd
}

func configureClusterNameFlagCompletion(cmd *cobra.Command) {
	cmd.RegisterFlagCompletionFunc("cluster-name",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}

			cc := k8s.NewCommandCompletion(k8sAPI, corev1.NamespaceAll)
			results, err := cc.Complete([]string{strings.ToLower(k8s.LinkKind)}, toComplete)
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}

			return results, cobra.ShellCompDirectiveDefault
		})
}
