package k8s

import (
	"strings"

	serverv1beta1 "github.com/linkerd/linkerd2/controller/gen/apis/server/v1beta1"
	sazv1beta1 "github.com/linkerd/linkerd2/controller/gen/apis/serverauthorization/v1beta1"
	spv1alpha2 "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// APIResource is an enum for Kubernetes API resource types, for use when
// initializing a K8s API, to describe which resource types to interact with.
type APIResource int

// These constants enumerate Kubernetes resource types.
// TODO: Unify with the resources listed in pkg/k8s/k8s.go
const (
	CJ APIResource = iota
	CM
	Deploy
	DS
	Endpoint
	ES // EndpointSlice resource
	Job
	MWC
	NS
	Pod
	RC
	RS
	SP
	SS
	Svc
	Node
	Secret
	Srv
	Saz
)

// GVK returns the GroupVersionKind corresponding for the provided APIResource
func (res APIResource) GVK() (schema.GroupVersionKind, error) {
	switch res {
	case CJ:
		return batchv1.SchemeGroupVersion.WithKind("CronJob"), nil
	case CM:
		return v1.SchemeGroupVersion.WithKind("ConfigMap"), nil
	case Deploy:
		return appsv1.SchemeGroupVersion.WithKind("Deployment"), nil
	case DS:
		return appsv1.SchemeGroupVersion.WithKind("DaemonSet"), nil
	case Endpoint:
		return v1.SchemeGroupVersion.WithKind("Endpoint"), nil
	case ES:
		return discoveryv1.SchemeGroupVersion.WithKind("EndpointSlice"), nil
	case Job:
		return batchv1.SchemeGroupVersion.WithKind("Job"), nil
	case MWC:
		return admissionregistrationv1.SchemeGroupVersion.WithKind("MutatingWebhookConfiguration"), nil
	case Node:
		return v1.SchemeGroupVersion.WithKind("Node"), nil
	case NS:
		return v1.SchemeGroupVersion.WithKind("Namespace"), nil
	case Pod:
		return v1.SchemeGroupVersion.WithKind("Pod"), nil
	case RC:
		return v1.SchemeGroupVersion.WithKind("ReplicationController"), nil
	case RS:
		return appsv1.SchemeGroupVersion.WithKind("ReplicaSet"), nil
	case Saz:
		return sazv1beta1.SchemeGroupVersion.WithKind("ServerAuthorization"), nil
	case Secret:
		return v1.SchemeGroupVersion.WithKind("Secret"), nil
	case SP:
		return spv1alpha2.SchemeGroupVersion.WithKind("ServiceProfile"), nil
	case SS:
		return appsv1.SchemeGroupVersion.WithKind("StatefulSet"), nil
	case Srv:
		return serverv1beta1.SchemeGroupVersion.WithKind("Server"), nil
	case Svc:
		return v1.SchemeGroupVersion.WithKind("Service"), nil
	default:
		return schema.GroupVersionKind{}, status.Errorf(codes.Unimplemented, "unimplemented resource type: %d", res)
	}
}

// GetAPIResource returns the APIResource for the provided kind
func GetAPIResource(kind string) (APIResource, error) {
	switch strings.ToLower(kind) {
	case k8s.CronJob:
		return CJ, nil
	case k8s.ConfigMap:
		return CM, nil
	case k8s.Deployment:
		return Deploy, nil
	case k8s.DaemonSet:
		return DS, nil
	case k8s.Endpoints:
		return Endpoint, nil
	case k8s.EndpointSlices:
		return ES, nil
	case k8s.Job:
		return Job, nil
	case k8s.MutatingWebhookConfig:
		return MWC, nil
	case k8s.Namespace:
		return NS, nil
	case k8s.Node:
		return Node, nil
	case k8s.Pod:
		return Pod, nil
	case k8s.ReplicationController:
		return RC, nil
	case k8s.ReplicaSet:
		return RS, nil
	case k8s.ServerAuthorization:
		return Saz, nil
	case k8s.Secret:
		return Secret, nil
	case k8s.ServiceProfile:
		return SP, nil
	case k8s.Service:
		return Svc, nil
	case k8s.StatefulSet:
		return SS, nil
	case k8s.Server:
		return Srv, nil
	default:
		return 0, status.Errorf(codes.Unimplemented, "unimplemented resource type: %s", kind)
	}
}
