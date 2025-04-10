package gwapi

import (
	"context"

	"github.com/linkerd/linkerd2/pkg/k8s"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type GatewayAPICRDs int

const (
	Absent GatewayAPICRDs = iota
	Linkerd
	External
)

// CheckGatewayAPICRDs returns true if the Gateway API CRDs are installed in the
// cluster, and false otherwise.
func CheckGatewayAPICRDs(ctx context.Context, k8sAPI *k8s.KubernetesAPI) (GatewayAPICRDs, error) {
	crds := k8sAPI.Apiextensions.ApiextensionsV1().CustomResourceDefinitions()
	result := Absent
	names := []string{
		"httproutes.gateway.networking.k8s.io",
		"grpcroutes.gateway.networking.k8s.io",
	}
	for _, name := range names {
		crd, err := crds.Get(ctx, name, metav1.GetOptions{})
		if err == nil && crd != nil {
			if crd.Annotations[k8s.CreatedByAnnotation] != "" {
				return Linkerd, nil
			}
			result = External
		} else if kerrors.IsNotFound(err) {
			// No action if CRD is not found.
		} else {
			return Absent, err
		}
	}
	return result, nil
}
