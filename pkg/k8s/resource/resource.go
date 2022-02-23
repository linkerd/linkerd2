package resource

import (
	"context"
	"fmt"
	"io"

	"github.com/linkerd/linkerd2/pkg/k8s"
	admissionRegistration "k8s.io/api/admissionregistration/v1"
	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	apiextension "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	apiRegistration "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	apiregistrationv1client "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/typed/apiregistration/v1"
	"sigs.k8s.io/yaml"
)

const (
	yamlSep = "---\n"
)

// Kubernetes is a parent object used to generalize all k8s types
type Kubernetes struct {
	runtime.TypeMeta
	metav1.ObjectMeta `json:"metadata"`
}

// New returns a kubernetes resource with the given data
func New(apiVersion, kind, name string) Kubernetes {
	return Kubernetes{
		runtime.TypeMeta{
			APIVersion: apiVersion,
			Kind:       kind,
		},
		metav1.ObjectMeta{
			Name: name,
		},
	}
}

// NewNamespaced returns a namespace scoped kubernetes resource with the given data
func NewNamespaced(apiVersion, kind, name, namespace string) Kubernetes {
	return Kubernetes{
		runtime.TypeMeta{
			APIVersion: apiVersion,
			Kind:       kind,
		},
		metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

// RenderResource renders a kubernetes object as a yaml object
func (r Kubernetes) RenderResource(w io.Writer) error {
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

// FetchKubernetesResources returns a slice of all cluster scoped kubernetes
// resources which match the given ListOptions.
func FetchKubernetesResources(ctx context.Context, k *k8s.KubernetesAPI, options metav1.ListOptions) ([]Kubernetes, error) {

	resources := make([]Kubernetes, 0)

	clusterRoles, err := fetchClusterRoles(ctx, k, options)
	if err != nil {
		return nil, fmt.Errorf("could not fetch ClusterRole resources: %w", err)
	}
	resources = append(resources, clusterRoles...)

	clusterRoleBindings, err := fetchClusterRoleBindings(ctx, k, options)
	if err != nil {
		return nil, fmt.Errorf("could not fetch ClusterRoleBinding resources: %w", err)
	}
	resources = append(resources, clusterRoleBindings...)

	roles, err := fetchRoles(ctx, k, options)
	if err != nil {
		return nil, fmt.Errorf("could not fetch Roles: %w", err)
	}
	resources = append(resources, roles...)

	roleBindings, err := fetchRoleBindings(ctx, k, options)
	if err != nil {
		return nil, fmt.Errorf("could not fetch RoleBindings: %w", err)
	}
	resources = append(resources, roleBindings...)

	crds, err := fetchCustomResourceDefinitions(ctx, k, options)
	if err != nil {
		return nil, fmt.Errorf("could not fetch CustomResourceDefinition resources: %w", err)
	}
	resources = append(resources, crds...)

	apiCRDs, err := fetchAPIRegistrationResources(ctx, k, options)
	if err != nil {
		return nil, fmt.Errorf("could not fetch APIService CRDs: %w", err)
	}
	resources = append(resources, apiCRDs...)

	mutatinghooks, err := fetchMutatingWebhooksConfiguration(ctx, k, options)
	if err != nil {
		return nil, fmt.Errorf("could not fetch MutatingWebhookConfigurations: %w", err)
	}
	resources = append(resources, mutatinghooks...)

	validationhooks, err := fetchValidatingWebhooksConfiguration(ctx, k, options)
	if err != nil {
		return nil, fmt.Errorf("could not fetch ValidatingWebhookConfiguration: %w", err)
	}
	resources = append(resources, validationhooks...)

	namespaces, err := fetchNamespace(ctx, k, options)
	if err != nil {
		return nil, fmt.Errorf("could not fetch Namespace: %w", err)
	}
	resources = append(resources, namespaces...)

	return resources, nil
}

func fetchClusterRoles(ctx context.Context, k *k8s.KubernetesAPI, options metav1.ListOptions) ([]Kubernetes, error) {
	list, err := k.RbacV1().ClusterRoles().List(ctx, options)
	if err != nil {
		return nil, err
	}

	resources := make([]Kubernetes, len(list.Items))
	for i, item := range list.Items {
		resources[i] = New(rbac.SchemeGroupVersion.String(), "ClusterRole", item.Name)
	}

	return resources, nil
}

func fetchClusterRoleBindings(ctx context.Context, k *k8s.KubernetesAPI, options metav1.ListOptions) ([]Kubernetes, error) {
	list, err := k.RbacV1().ClusterRoleBindings().List(ctx, options)
	if err != nil {
		return nil, err
	}

	resources := make([]Kubernetes, len(list.Items))
	for i, item := range list.Items {
		resources[i] = New(rbac.SchemeGroupVersion.String(), "ClusterRoleBinding", item.Name)
	}

	return resources, nil
}

func fetchRoles(ctx context.Context, k *k8s.KubernetesAPI, options metav1.ListOptions) ([]Kubernetes, error) {
	list, err := k.RbacV1().Roles("").List(ctx, options)
	if err != nil {
		return nil, err
	}

	resources := make([]Kubernetes, len(list.Items))
	for i, item := range list.Items {
		r := New(rbac.SchemeGroupVersion.String(), "Role", item.Name)
		r.Namespace = item.Namespace
		resources[i] = r
	}
	return resources, nil
}

func fetchRoleBindings(ctx context.Context, k *k8s.KubernetesAPI, options metav1.ListOptions) ([]Kubernetes, error) {
	list, err := k.RbacV1().RoleBindings("").List(ctx, options)
	if err != nil {
		return nil, err
	}

	resources := make([]Kubernetes, len(list.Items))
	for i, item := range list.Items {
		r := New(rbac.SchemeGroupVersion.String(), "RoleBinding", item.Name)
		r.Namespace = item.Namespace
		resources[i] = r
	}
	return resources, nil
}

func fetchCustomResourceDefinitions(ctx context.Context, k *k8s.KubernetesAPI, options metav1.ListOptions) ([]Kubernetes, error) {
	list, err := k.Apiextensions.ApiextensionsV1().CustomResourceDefinitions().List(ctx, options)
	if err != nil {
		return nil, err
	}

	resources := make([]Kubernetes, len(list.Items))
	for i, item := range list.Items {
		resources[i] = New(apiextension.SchemeGroupVersion.String(), "CustomResourceDefinition", item.Name)
	}

	return resources, nil
}

func fetchNamespace(ctx context.Context, k *k8s.KubernetesAPI, options metav1.ListOptions) ([]Kubernetes, error) {
	list, err := k.CoreV1().Namespaces().List(ctx, options)
	if err != nil {
		return nil, err
	}

	resources := make([]Kubernetes, len(list.Items))
	for i, item := range list.Items {
		r := New(core.SchemeGroupVersion.String(), "Namespace", item.Name)
		r.Namespace = item.Namespace
		resources[i] = r
	}
	return resources, nil
}

func fetchValidatingWebhooksConfiguration(ctx context.Context, k *k8s.KubernetesAPI, options metav1.ListOptions) ([]Kubernetes, error) {
	list, err := k.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(ctx, options)
	if err != nil {
		return nil, err
	}

	resources := make([]Kubernetes, len(list.Items))
	for i, item := range list.Items {
		resources[i] = New(admissionRegistration.SchemeGroupVersion.String(), "ValidatingWebhookConfiguration", item.Name)
	}

	return resources, nil
}

func fetchMutatingWebhooksConfiguration(ctx context.Context, k *k8s.KubernetesAPI, options metav1.ListOptions) ([]Kubernetes, error) {
	list, err := k.AdmissionregistrationV1().MutatingWebhookConfigurations().List(ctx, options)
	if err != nil {
		return nil, err
	}

	resources := make([]Kubernetes, len(list.Items))
	for i, item := range list.Items {
		resources[i] = New(admissionRegistration.SchemeGroupVersion.String(), "MutatingWebhookConfiguration", item.Name)
	}

	return resources, nil
}
func fetchAPIRegistrationResources(ctx context.Context, k *k8s.KubernetesAPI, options metav1.ListOptions) ([]Kubernetes, error) {
	apiClient, err := apiregistrationv1client.NewForConfig(k.Config)
	if err != nil {
		return nil, err
	}

	list, err := apiClient.APIServices().List(ctx, options)
	if err != nil {
		return nil, err
	}

	resources := make([]Kubernetes, len(list.Items))
	for i, item := range list.Items {
		resources[i] = New(apiRegistration.SchemeGroupVersion.String(), "APIService", item.Name)
	}

	return resources, nil
}
