/*
Kubernetes labels and annotations used in Linkerd's control plane and data plane
Kubernetes configs.
*/

package k8s

import (
	"fmt"

	"github.com/linkerd/linkerd2/pkg/version"
	appsV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
)

const (
	/*
	 * Labels
	 */

	// ControllerComponentLabel identifies this object as a component of Linkerd's
	// control plane (e.g. web, controller).
	ControllerComponentLabel = "linkerd.io/control-plane-component"

	// ControllerNSLabel is injected into mesh-enabled apps, identifying the
	// namespace of the Linkerd control plane.
	ControllerNSLabel = "linkerd.io/control-plane-ns"

	// ProxyDeploymentLabel is injected into mesh-enabled apps, identifying the
	// deployment that this proxy belongs to.
	ProxyDeploymentLabel = "linkerd.io/proxy-deployment"

	// ProxyReplicationControllerLabel is injected into mesh-enabled apps,
	// identifying the ReplicationController that this proxy belongs to.
	ProxyReplicationControllerLabel = "linkerd.io/proxy-replicationcontroller"

	// ProxyReplicaSetLabel is injected into mesh-enabled apps, identifying the
	// ReplicaSet that this proxy belongs to.
	ProxyReplicaSetLabel = "linkerd.io/proxy-replicaset"

	// ProxyJobLabel is injected into mesh-enabled apps, identifying the Job that
	// this proxy belongs to.
	ProxyJobLabel = "linkerd.io/proxy-job"

	// ProxyDaemonSetLabel is injected into mesh-enabled apps, identifying the
	// DaemonSet that this proxy belongs to.
	ProxyDaemonSetLabel = "linkerd.io/proxy-daemonset"

	// ProxyStatefulSetLabel is injected into mesh-enabled apps, identifying the
	// StatefulSet that this proxy belongs to.
	ProxyStatefulSetLabel = "linkerd.io/proxy-statefulset"

	/*
	 * Annotations
	 */

	// CreatedByAnnotation indicates the source of the injected data plane
	// (e.g. linkerd/cli v2.0.0).
	CreatedByAnnotation = "linkerd.io/created-by"

	// ProxyVersionAnnotation indicates the version of the injected data plane
	// (e.g. v0.1.3).
	ProxyVersionAnnotation = "linkerd.io/proxy-version"

	/*
	 * Component Names
	 */

	// TLSTrustAnchorConfigMapName is the name of the ConfigMap that holds the
	// trust anchors (trusted root certificates).
	TLSTrustAnchorConfigMapName = "linkerd-ca-bundle"

	// TLSTrustAnchorFileName is the name (key) within the trust anchor ConfigMap
	// that contains the actual trust anchor bundle.
	TLSTrustAnchorFileName = "trust-anchors.pem"

	TLSCertFileName       = "certificate.crt"
	TLSPrivateKeyFileName = "private-key.p8"
)

// CreatedByAnnotationValue returns the value associated with
// CreatedByAnnotation.
func CreatedByAnnotationValue() string {
	return fmt.Sprintf("linkerd/cli %s", version.Version)
}

// GetPodLabels returns the set of prometheus owner labels for a given pod
func GetPodLabels(ownerKind, ownerName string, pod *coreV1.Pod) map[string]string {
	labels := map[string]string{"pod": pod.Name}
	if ownerKind == "job" {
		labels["k8s_job"] = ownerName
	} else {
		labels[ownerKind] = ownerName
	}

	if controllerNS := GetControllerNs(pod); controllerNS != "" {
		labels["linkerd_io_control_plane_ns"] = controllerNS
	}

	if pth := pod.Labels[appsV1.DefaultDeploymentUniqueLabelKey]; pth != "" {
		labels["pod_template_hash"] = pth
	}

	return labels
}

func GetControllerNs(pod *coreV1.Pod) string {
	return pod.Labels[ControllerNSLabel]
}

// TLSIdentity is the identity of a pod owner (Deployment, Pod,
// ReplicationController, etc.).
type TLSIdentity struct {
	// Name is the name of the pod owner.
	Name string

	// Kind is the singular, lowercased Kubernetes resource type of the pod owner
	// (deployment, daemonset, job, replicationcontroller, etc.).
	Kind string

	// Namespace is the pod's namespace. Kubernetes requires that pods and
	// pod owners be in the same namespace.
	Namespace string

	// ControllerNamespace is the namespace of the controller for the pod.
	ControllerNamespace string
}

func (i TLSIdentity) ToDNSName() string {
	return fmt.Sprintf("%s.%s.%s.linkerd-managed.%s.svc.cluster.local", i.Name,
		i.Kind, i.Namespace, i.ControllerNamespace)
}

func (i TLSIdentity) ToSecretName() string {
	return fmt.Sprintf("%s-%s-tls-linkerd-io", i.Name, i.Kind)
}

func (i TLSIdentity) ToControllerIdentity() TLSIdentity {
	return TLSIdentity{
		Name:                "controller",
		Kind:                "deployment",
		Namespace:           i.ControllerNamespace,
		ControllerNamespace: i.ControllerNamespace,
	}
}
