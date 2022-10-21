package k8s

import (
	"fmt"
	"strings"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
		return schema.GroupVersionKind{
			Group:   "batch",
			Version: "v1",
			Kind:    "CronJob",
		}, nil
	case CM:
		return schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "Configmap",
		}, nil
	case Deploy:
		return schema.GroupVersionKind{
			Group:   "apps",
			Version: "v1",
			Kind:    "Deployment",
		}, nil
	case DS:
		return schema.GroupVersionKind{
			Group:   "apps",
			Version: "v1",
			Kind:    "DaemonSet",
		}, nil
	case Endpoint:
		return schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "Endpoint",
		}, nil
	case ES:
		return schema.GroupVersionKind{
			Group:   "discovery.k8s.io",
			Version: "v1",
			Kind:    "Endpointslice",
		}, nil
	case Job:
		return schema.GroupVersionKind{
			Group:   "batch",
			Version: "v1",
			Kind:    "Job",
		}, nil
	case MWC:
		return schema.GroupVersionKind{
			Group:   "admissionregistration.k8s.io",
			Version: "v1",
			Kind:    "Mutatingwebhookconfiguration",
		}, nil
	case Node:
		return schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "Node",
		}, nil
	case NS:
		return schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "Namespace",
		}, nil
	case Pod:
		return schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "Pod",
		}, nil
	case RC:
		return schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "ReplicationController",
		}, nil
	case RS:
		return schema.GroupVersionKind{
			Group:   "apps",
			Version: "v1",
			Kind:    "Replicaset",
		}, nil
	case Saz:
		return schema.GroupVersionKind{
			Group:   "policy.linkerd.io",
			Version: "v1beta1",
			Kind:    "Serverauthorization",
		}, nil
	case Secret:
		return schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "Secret",
		}, nil
	case SP:
		return schema.GroupVersionKind{
			Group:   "linkerd.io",
			Version: "v1alpha2",
			Kind:    "Serviceprofile",
		}, nil
	case SS:
		return schema.GroupVersionKind{
			Group:   "apps",
			Version: "v1",
			Kind:    "StatefulSet",
		}, nil
	case Srv:
		return schema.GroupVersionKind{
			Group:   "policy.linkerd.io",
			Version: "v1beta1",
			Kind:    "Server",
		}, nil
	case Svc:
		return schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "Service",
		}, nil
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
		return 0, fmt.Errorf("APIResource not found: %s", kind)
	}
}
