package k8s

import (
	"fmt"

	authV1 "k8s.io/api/authorization/v1"
	"k8s.io/client-go/kubernetes"
)

// ResourceAuthz checks whether a given Kubernetes client is authorized to
// perform a given action.
func ResourceAuthz(
	k8sClient kubernetes.Interface,
	namespace, verb, group, version, resource string,
) (bool, string, error) {
	ssar := &authV1.SelfSubjectAccessReview{
		Spec: authV1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authV1.ResourceAttributes{
				Namespace: namespace,
				Verb:      verb,
				Group:     group,
				Version:   version,
				Resource:  resource,
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
					// TODO: Modify this to honor the error returned, once we give the
					// control-plane namespace-wide access to ServiceProfiles.
					access, _ := resourceAccess(k8sClient, "", "linkerd.io", "serviceprofiles")
					return access, nil
				}
			}
		}
	}

	return false, nil
}

// ClusterAccess verifies whether k8sClient is authorized to access all
// namespaces in the cluster, or only the given namespace. If k8sClient does not
// have at least namespace-wide access, it returns an error.
func ClusterAccess(k8sClient kubernetes.Interface, namespace string) (bool, error) {
	return resourceAccess(k8sClient, namespace, "", "pods")
}

// resourceAccess verifies whether k8sClient is authorized to access a resource
// in all namespaces in the cluster, or only the given namespace. If k8sClient
// does not have at least namespace-wide access, it returns an error.
func resourceAccess(k8sClient kubernetes.Interface, namespace, group, resource string) (bool, error) {
	// first check for cluster-wide access
	allowed, _, err := ResourceAuthz(
		k8sClient,
		"",
		"list",
		group,
		"",
		resource,
	)
	if err != nil {
		return false, err
	}
	if allowed {
		// authorized for cluster-wide access
		return true, nil
	}

	// next check for namespace-wide access
	allowed, reason, err := ResourceAuthz(
		k8sClient,
		namespace,
		"list",
		group,
		"",
		resource,
	)
	if err != nil {
		return false, err
	}
	if allowed {
		// authorized for namespace-wide access
		return false, nil
	}

	if len(reason) > 0 {
		return false, fmt.Errorf("not authorized to access \"%s\" namespace: %s", namespace, reason)
	}
	return false, fmt.Errorf("not authorized to access \"%s\" namespace", namespace)
}
