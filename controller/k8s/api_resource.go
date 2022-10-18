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
		return strToGVK("CronJob.v1.batch"), nil
	case CM:
		return strToGVK("Configmap.v1."), nil
	case Deploy:
		return strToGVK("Deployment.v1.apps"), nil
	case DS:
		return strToGVK("DaemonSet.v1.apps"), nil
	case Endpoint:
		return strToGVK("Endpoint.v1."), nil
	case ES:
		return strToGVK("Endpointslice.v1.discovery.k8s.io"), nil
	case Job:
		return strToGVK("Job.v1.batch"), nil
	case MWC:
		return strToGVK("Mutatingwebhookconfiguration.v1.admissionregistration.k8s.io"), nil
	case Node:
		return strToGVK("Node.v1."), nil
	case NS:
		return strToGVK("Namespace.v1."), nil
	case Pod:
		return strToGVK("Pod.v1."), nil
	case RC:
		return strToGVK("ReplicationController.v1."), nil
	case RS:
		return strToGVK("Replicaset.v1.apps"), nil
	case Saz:
		return strToGVK("Serverauthorization.v1beta1.policy.linkerd.io"), nil
	case Secret:
		return strToGVK("Secret.v1."), nil
	case SP:
		return strToGVK("Serviceprofile.v1alpha2.linkerd.io"), nil
	case SS:
		return strToGVK("StatefulSet.v1.apps"), nil
	case Srv:
		return strToGVK("Server.v1beta1.policy.linkerd.io"), nil
	case Svc:
		return strToGVK("Service.v1."), nil
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

func strToGVK(str string) schema.GroupVersionKind {
	parts := strings.SplitN(str, ".", 3)
	if len(parts) != 3 {
		return schema.GroupVersionKind{}
	}
	return schema.GroupVersionKind{Group: parts[2], Version: parts[1], Kind: parts[0]}
}
