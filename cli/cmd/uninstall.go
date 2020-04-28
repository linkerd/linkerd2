package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/ghodss/yaml"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	admissionRegistration "k8s.io/api/admissionregistration/v1beta1"
	core "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1beta1"
	rbac "k8s.io/api/rbac/v1"
	apiextension "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	apiRegistration "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	apiregistrationv1client "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/typed/apiregistration/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	yamlSep = "---\n"
)

type kubernetesResource struct {
	runtime.TypeMeta
	metav1.ObjectMeta `json:"metadata"`
}

func newKubernetesResource(apiVersion, kind, name string) kubernetesResource {
	return kubernetesResource{
		runtime.TypeMeta{
			APIVersion: apiVersion,
			Kind:       kind,
		},
		metav1.ObjectMeta{
			Name: name,
		},
	}
}

func newCmdUninstall() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Args:  cobra.NoArgs,
		Short: "Output Kubernetes resources to uninstall Linkerd control plane",
		Long: `Output Kubernetes resources to uninstall Linkerd control plane.

This command provides all Kubernetes namespace-scoped and cluster-scoped resources (e.g services, deployments, RBACs, etc.) necessary to uninstall Linkerd control plane.`,
		Example: ` linkerd uninstall | kubectl delete -f -`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return uninstallRunE()
		},
	}

	return cmd
}

func uninstallRunE() error {
	k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
	if err != nil {
		return err
	}

	resources, err := fetchKubernetesResources(k8sAPI)
	if err != nil {
		return err
	}

	for _, r := range resources {
		if err := r.renderResource(os.Stdout); err != nil {
			return fmt.Errorf("error rendering Kubernetes resource:%v", err)
		}
	}
	return nil
}

func (r kubernetesResource) renderResource(w io.Writer) error {
	b, err := yaml.Marshal(r)
	if err != nil {
		return err
	}

	_, err = w.Write(b)
	if err != nil {
		return err
	}

	_, err = w.Write([]byte(yamlSep))
	return err
}

func fetchKubernetesResources(k *k8s.KubernetesAPI) ([]kubernetesResource, error) {
	options := metav1.ListOptions{
		LabelSelector: k8s.ControllerNSLabel,
	}

	resources := make([]kubernetesResource, 0)

	clusterRoles, err := fetchClusterRoles(k, options)
	if err != nil {
		return nil, fmt.Errorf("could not fetch ClusterRole resources:%v", err)
	}
	resources = append(resources, clusterRoles...)

	clusterRoleBindings, err := fetchClusterRoleBindings(k, options)
	if err != nil {
		return nil, fmt.Errorf("could not fetch ClusterRoleBinding resources:%v", err)
	}
	resources = append(resources, clusterRoleBindings...)

	roleBindings, err := fetchKubeSystemRoleBindings(k, options)
	if err != nil {
		return nil, fmt.Errorf("could not fetch RoleBindings from kube-system namespace:%v", err)
	}
	resources = append(resources, roleBindings...)

	crds, err := fetchCustomResourceDefinitions(k, options)
	if err != nil {
		return nil, fmt.Errorf("could not fetch CustomResourceDefinition resources:%v", err)
	}
	resources = append(resources, crds...)

	apiCRDs, err := fetchAPIRegistrationResources(k, options)
	if err != nil {
		return nil, fmt.Errorf("could not fetch APIService CRDs:%v", err)
	}
	resources = append(resources, apiCRDs...)

	psps, err := fetchPodSecurityPolicy(k, options)
	if err != nil {
		return nil, fmt.Errorf("could not fetch PodSecurityPolicy resources:%v", err)
	}
	resources = append(resources, psps...)

	mutatinghooks, err := fetchMutatingWebhooksConfiguration(k, options)
	if err != nil {
		return nil, fmt.Errorf("could not fetch MutatingWebhookConfigurations:%v", err)
	}
	resources = append(resources, mutatinghooks...)

	validationhooks, err := fetchValidatingWebhooksConfiguration(k, options)
	if err != nil {
		return nil, fmt.Errorf("could not fetch ValidatingWebhookConfiguration:%v", err)
	}
	resources = append(resources, validationhooks...)

	namespace, err := fetchNamespaceResource(k)
	if err != nil {
		return nil, fmt.Errorf("could not fetch Namespace %s:%v", controlPlaneNamespace, err)
	}

	if namespace.Name != "" {
		resources = append(resources, namespace)
	}

	return resources, nil
}

func fetchClusterRoles(k *k8s.KubernetesAPI, options metav1.ListOptions) ([]kubernetesResource, error) {
	list, err := k.RbacV1().ClusterRoles().List(options)
	if err != nil {
		return nil, err
	}

	resources := make([]kubernetesResource, len(list.Items))
	for i, item := range list.Items {
		resources[i] = newKubernetesResource(rbac.SchemeGroupVersion.String(), "ClusterRole", item.Name)
	}

	return resources, nil
}

func fetchClusterRoleBindings(k *k8s.KubernetesAPI, options metav1.ListOptions) ([]kubernetesResource, error) {
	list, err := k.RbacV1().ClusterRoleBindings().List(options)
	if err != nil {
		return nil, err
	}

	resources := make([]kubernetesResource, len(list.Items))
	for i, item := range list.Items {
		resources[i] = newKubernetesResource(rbac.SchemeGroupVersion.String(), "ClusterRoleBinding", item.Name)
	}

	return resources, nil
}

// Although role bindings are namespaced resources in nature
// some admin role bindings are created and persisted in the kube-system namespace and will not be deleted
// when the namespace is deleted
func fetchKubeSystemRoleBindings(k *k8s.KubernetesAPI, options metav1.ListOptions) ([]kubernetesResource, error) {
	list, err := k.RbacV1().RoleBindings("kube-system").List(options)
	if err != nil {
		return nil, err
	}

	resources := make([]kubernetesResource, len(list.Items))
	for i, item := range list.Items {
		r := newKubernetesResource(rbac.SchemeGroupVersion.String(), "RoleBinding", item.Name)
		r.Namespace = item.Namespace
		resources[i] = r
	}
	return resources, nil
}

func fetchCustomResourceDefinitions(k *k8s.KubernetesAPI, options metav1.ListOptions) ([]kubernetesResource, error) {
	list, err := k.Apiextensions.ApiextensionsV1beta1().CustomResourceDefinitions().List(options)
	if err != nil {
		return nil, err
	}

	resources := make([]kubernetesResource, len(list.Items))
	for i, item := range list.Items {
		resources[i] = newKubernetesResource(apiextension.SchemeGroupVersion.String(), "CustomResourceDefinition", item.Name)
	}

	return resources, nil
}

func fetchNamespaceResource(k *k8s.KubernetesAPI) (kubernetesResource, error) {
	obj, err := k.CoreV1().Namespaces().Get(controlPlaneNamespace, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return kubernetesResource{}, nil
		}
		return kubernetesResource{}, err
	}

	return newKubernetesResource(core.SchemeGroupVersion.String(), "Namespace", obj.Name), nil
}

func fetchPodSecurityPolicy(k *k8s.KubernetesAPI, options metav1.ListOptions) ([]kubernetesResource, error) {
	list, err := k.PolicyV1beta1().PodSecurityPolicies().List(options)
	if err != nil {
		return nil, err
	}

	resources := make([]kubernetesResource, len(list.Items))
	for i, item := range list.Items {
		resources[i] = newKubernetesResource(policy.SchemeGroupVersion.String(), "PodSecurityPolicy", item.Name)
	}

	return resources, nil
}

func fetchValidatingWebhooksConfiguration(k *k8s.KubernetesAPI, options metav1.ListOptions) ([]kubernetesResource, error) {
	list, err := k.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().List(options)
	if err != nil {
		return nil, err
	}

	resources := make([]kubernetesResource, len(list.Items))
	for i, item := range list.Items {
		resources[i] = newKubernetesResource(admissionRegistration.SchemeGroupVersion.String(), "ValidatingWebhookConfiguration", item.Name)
	}

	return resources, nil
}

func fetchMutatingWebhooksConfiguration(k *k8s.KubernetesAPI, options metav1.ListOptions) ([]kubernetesResource, error) {
	list, err := k.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().List(options)
	if err != nil {
		return nil, err
	}

	resources := make([]kubernetesResource, len(list.Items))
	for i, item := range list.Items {
		resources[i] = newKubernetesResource(admissionRegistration.SchemeGroupVersion.String(), "MutatingWebhookConfiguration", item.Name)
	}

	return resources, nil
}
func fetchAPIRegistrationResources(k *k8s.KubernetesAPI, options metav1.ListOptions) ([]kubernetesResource, error) {
	apiClient, err := apiregistrationv1client.NewForConfig(k.Config)
	if err != nil {
		return nil, err
	}

	list, err := apiClient.APIServices().List(options)
	if err != nil {
		return nil, err
	}

	resources := make([]kubernetesResource, len(list.Items))
	for i, item := range list.Items {
		resources[i] = newKubernetesResource(apiRegistration.SchemeGroupVersion.String(), "APIService", item.Name)
	}

	return resources, nil
}
