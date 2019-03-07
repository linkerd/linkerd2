package k8s

import (
	"errors"
	"fmt"

	authV1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

// ResourceAuthz checks whether a given Kubernetes client is authorized to
// perform a given action.
func ResourceAuthz(
	k8sClient kubernetes.Interface,
	namespace, verb, group, version, resource, name string,
) (bool, string, error) {
	ssar := &authV1.SelfSubjectAccessReview{
		Spec: authV1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authV1.ResourceAttributes{
				Namespace: namespace,
				Verb:      verb,
				Group:     group,
				Version:   version,
				Resource:  resource,
				Name:      name,
			},
		},
	}

	result, err := k8sClient.
		AuthorizationV1().
		SelfSubjectAccessReviews().
		Create(ssar)
	if err != nil {
		return false, "", err
	}

	return result.Status.Allowed, result.Status.Reason, nil
}

// ServiceProfilesAccess checks whether the ServiceProfile CRD is installed
// on the cluster and the client is authorized to access ServiceProfiles.
func ServiceProfilesAccess(k8sClient kubernetes.Interface) (bool, error) {
	res, err := k8sClient.Discovery().ServerResources()
	if err != nil {
		return false, err
	}

	for _, r := range res {
		if r.GroupVersion == ServiceProfileAPIVersion {
			for _, apiRes := range r.APIResources {
				if apiRes.Kind == ServiceProfileKind {
					return resourceAccess(k8sClient, schema.GroupKind{
						Group: "linkerd.io",
						Kind:  "serviceprofiles",
					})
				}
			}
		}
	}

	return false, errors.New("ServiceProfiles not available")
}

// ClusterAccess verifies whether k8sClient is authorized to access all pods in
// all namespaces in the cluster.
func ClusterAccess(k8sClient kubernetes.Interface) (bool, error) {
	return resourceAccess(k8sClient, schema.GroupKind{Kind: "pods"})
}

// resourceAccess verifies whether k8sClient is authorized to access a resource
// in all namespaces in the cluster.
func resourceAccess(k8sClient kubernetes.Interface, gk schema.GroupKind) (bool, error) {
	allowed, reason, err := ResourceAuthz(
		k8sClient,
		"",
		"list",
		gk.Group,
		"",
		gk.Kind,
		"",
	)
	if err != nil {
		return false, err
	}
	if allowed {
		return true, nil
	}

	if len(reason) > 0 {
		return false, fmt.Errorf("not authorized to access %s: %s", gk, reason)
	}
	return false, fmt.Errorf("not authorized to access %s", gk)
}
