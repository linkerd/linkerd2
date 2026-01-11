package k8s

import (
	"context"
	"errors"
	"fmt"

	authV1 "k8s.io/api/authorization/v1"
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

// ResourceAuthz checks whether a given Kubernetes client is authorized to
// perform a given action.
func ResourceAuthz(
	ctx context.Context,
	k8sClient kubernetes.Interface,
	namespace, verb, group, version, resource, name string,
) error {
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
		Create(ctx, ssar, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	return evaluateAccessReviewStatus(group, resource, result.Status)
}

// ResourceAuthzForUser checks whether a given user is authorized to perform a
// given action.
func ResourceAuthzForUser(
	ctx context.Context,
	client kubernetes.Interface,
	namespace, verb, group, version, resource, subresource, name, user string,
	userGroups []string,
	extra map[string]authV1.ExtraValue,
) error {
	sar := &authV1.SubjectAccessReview{
		Spec: authV1.SubjectAccessReviewSpec{
			User:   user,
			Groups: userGroups,
			Extra:  extra,
			ResourceAttributes: &authV1.ResourceAttributes{
				Namespace:   namespace,
				Verb:        verb,
				Group:       group,
				Version:     version,
				Resource:    resource,
				Subresource: subresource,
				Name:        name,
			},
		},
	}

	result, err := client.
		AuthorizationV1().
		SubjectAccessReviews().
		Create(ctx, sar, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	return evaluateAccessReviewStatus(group, resource, result.Status)
}

func evaluateAccessReviewStatus(group, resource string, status authV1.SubjectAccessReviewStatus) error {
	if status.Allowed {
		return nil
	}

	gk := schema.GroupKind{
		Group: group,
		Kind:  resource,
	}
	if len(status.Reason) > 0 {
		return fmt.Errorf("not authorized to access %s: %s", gk, status.Reason)
	}
	return fmt.Errorf("not authorized to access %s", gk)
}

// ServiceProfilesAccess checks whether the ServiceProfile CRD is installed
// on the cluster and the client is authorized to access ServiceProfiles.
func ServiceProfilesAccess(ctx context.Context, k8sClient kubernetes.Interface) error {
	res, err := k8sClient.Discovery().ServerResourcesForGroupVersion(ServiceProfileAPIVersion)
	if err != nil {
		return err
	}

	if res.GroupVersion == ServiceProfileAPIVersion {
		for _, apiRes := range res.APIResources {
			if apiRes.Kind == ServiceProfileKind {
				return ResourceAuthz(ctx, k8sClient, "", "list", "linkerd.io", "", "serviceprofiles", "")
			}
		}
	}

	return errors.New("ServiceProfile CRD not found")
}

// ServersAccess checks whether the Server CRD is installed on the cluster
// and the client is authorized to access Servers.
func ServersAccess(ctx context.Context, k8sClient kubernetes.Interface) error {
	groupVersion := fmt.Sprintf("%s/%s", PolicyAPIGroup, PolicyServerCRDVersion)
	res, err := k8sClient.Discovery().ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		return err
	}
	if res.GroupVersion == groupVersion {
		for _, apiRes := range res.APIResources {
			if apiRes.Kind == ServerKind {
				return ResourceAuthz(ctx, k8sClient, "", "list", PolicyAPIGroup, "", "servers", "")
			}
		}
	}
	return fmt.Errorf("server CRD (%s) not found", groupVersion)
}

// ExtWorkloadAccess checks whether the ExternalWorkload CRD is installed on the
// cluster and the client is authorized to access ExternalWorkloads
func ExtWorkloadAccess(ctx context.Context, k8sClient kubernetes.Interface) error {
	groupVersion := fmt.Sprintf("%s/%s", WorkloadAPIGroup, WorkloadAPIVersion)
	res, err := k8sClient.Discovery().ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		return err
	}
	if res.GroupVersion == groupVersion {
		for _, apiRes := range res.APIResources {
			if apiRes.Kind == ExtWorkloadKind {
				return ResourceAuthz(ctx, k8sClient, "", "list", WorkloadAPIGroup, "", "externalworkloads", "")
			}
		}
	}
	return errors.New("ExternalWorkload CRD not found")
}

// LinksAccess checks whether the Links CRD is installed on the
// cluster and the client is authorized to access Links
func LinksAccess(ctx context.Context, k8sClient kubernetes.Interface) error {
	groupVersion := fmt.Sprintf("%s/%s", LinkAPIGroup, LinkAPIVersion)
	res, err := k8sClient.Discovery().ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		return err
	}
	if res.GroupVersion == groupVersion {
		for _, apiRes := range res.APIResources {
			if apiRes.Kind == LinkKind {
				return ResourceAuthz(ctx, k8sClient, "", "list", LinkAPIGroup, "", "links", "")
			}
		}
	}
	return errors.New("Links CRD not found")
}

// EndpointSliceAccess verifies whether the K8s cluster has
// access to EndpointSlice resources.
func EndpointSliceAccess(ctx context.Context, k8sClient kubernetes.Interface) error {
	gv := discovery.SchemeGroupVersion.String()
	res, err := k8sClient.Discovery().ServerResourcesForGroupVersion(gv)
	if err != nil {
		return err
	}

	if res.GroupVersion == gv {
		for _, apiRes := range res.APIResources {
			if apiRes.Kind == "EndpointSlice" {
				return checkEndpointSlicesExist(ctx, k8sClient)
			}
		}
	}

	return errors.New("EndpointSlice resource not found")
}

func checkEndpointSlicesExist(ctx context.Context, k8sClient kubernetes.Interface) error {
	sliceList, err := k8sClient.DiscoveryV1().EndpointSlices("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	if len(sliceList.Items) > 0 {
		return nil
	}

	return errors.New("no EndpointSlice resources exist in the cluster")
}

// ClusterAccess verifies whether k8sClient is authorized to access all pods in
// all namespaces in the cluster.
func ClusterAccess(ctx context.Context, k8sClient kubernetes.Interface) error {
	return ResourceAuthz(ctx, k8sClient, "", "list", "", "", "pods", "")
}
