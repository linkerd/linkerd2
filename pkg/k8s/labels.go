/*
Kubernetes labels and annotations used in Conduit's control plane and data plane
Kubernetes configs.
*/

package k8s

import (
	"fmt"
	"strings"

	"github.com/runconduit/conduit/pkg/version"
	k8sV1 "k8s.io/api/apps/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	/*
	 * Labels
	 */

	// ControllerComponentLabel identifies this object as a component of Conduit's
	// control plane (e.g. web, controller).
	ControllerComponentLabel = "conduit.io/control-plane-component"

	// ControllerNSLabel is injected into mesh-enabled apps, identifying the
	// namespace of the Conduit control plane.
	ControllerNSLabel = "conduit.io/control-plane-ns"

	// ProxyDeploymentLabel is injected into mesh-enabled apps, identifying the
	// deployment that this proxy belongs to.
	ProxyDeploymentLabel = "conduit.io/proxy-deployment"

	// ProxyReplicationControllerLabel is injected into mesh-enabled apps,
	// identifying the ReplicationController that this proxy belongs to.
	ProxyReplicationControllerLabel = "conduit.io/proxy-replication-controller"

	// ProxyReplicaSetLabel is injected into mesh-enabled apps, identifying the
	// ReplicaSet that this proxy belongs to.
	ProxyReplicaSetLabel = "conduit.io/proxy-replica-set"

	// ProxyJobLabel is injected into mesh-enabled apps, identifying the Job that
	// this proxy belongs to.
	ProxyJobLabel = "conduit.io/proxy-job"

	// ProxyDaemonSetLabel is injected into mesh-enabled apps, identifying the
	// DaemonSet that this proxy belongs to.
	ProxyDaemonSetLabel = "conduit.io/proxy-daemon-set"

	// ProxyStatefulSetLabel is injected into mesh-enabled apps, identifying the
	// StatefulSet that this proxy belongs to.
	ProxyStatefulSetLabel = "conduit.io/proxy-stateful-set"

	/*
	 * Annotations
	 */

	// CreatedByAnnotation indicates the source of the injected data plane
	// (e.g. conduit/cli v0.1.3).
	CreatedByAnnotation = "conduit.io/created-by"

	// ProxyVersionAnnotation indicates the version of the injected data plane
	// (e.g. v0.1.3).
	ProxyVersionAnnotation = "conduit.io/proxy-version"

	/*
	 * Component Names
	 */

	// TLSTrustAnchorConfigMapName is the name of the ConfigMap that holds the
	// trust anchors (trusted root certificates).
	TLSTrustAnchorConfigMapName = "conduit-ca-bundle"

	// TLSTrustAnchorFileName is the name (key) within the trust anchor ConfigMap
	// that contains the actual trust anchor bundle.
	TLSTrustAnchorFileName = "trust-anchors.pem"

	TLSCertFileName       = "certificate.crt"
	TLSPrivateKeyFileName = "private-key.p8"
)

var podOwnerLabels = []string{
	ProxyDeploymentLabel,
	ProxyReplicationControllerLabel,
	ProxyReplicaSetLabel,
	ProxyJobLabel,
	ProxyDaemonSetLabel,
	ProxyStatefulSetLabel,
}

var proxyLabels = append(podOwnerLabels, []string{
	ControllerNSLabel,
	k8sV1.DefaultDeploymentUniqueLabelKey,
}...)

// CreatedByAnnotationValue returns the value associated with
// CreatedByAnnotation.
func CreatedByAnnotationValue() string {
	return fmt.Sprintf("conduit/cli %s", version.Version)
}

// GetOwnerLabels returns the set of prometheus owner labels that can be
// extracted from the proxy labels that have been added to an injected
// kubernetes resource
func GetOwnerLabels(objectMeta meta.ObjectMeta) map[string]string {
	labels := make(map[string]string)
	for _, label := range proxyLabels {
		if labelValue, ok := objectMeta.Labels[label]; ok {
			labels[toOwnerLabel(label)] = labelValue
		}
	}
	return labels
}

func GetControllerNs(objectMeta meta.ObjectMeta) string {
	return objectMeta.Labels[ControllerNSLabel]
}

func GetOwnerType(objectMeta meta.ObjectMeta) string {
	return GetOwnerTypeFromLabels(objectMeta.Labels)
}

func GetOwnerTypeFromLabels(labels map[string]string) string {
	for _, label := range podOwnerLabels {
		if _, ok := labels[label]; ok {
			return toOwnerLabel(label)
		}
	}
	return ""
}

// toOwnerLabel converts a proxy label to a prometheus label, following the
// relabel conventions from the prometheus scrape config file
func toOwnerLabel(proxyLabel string) string {
	if proxyLabel == ControllerNSLabel {
		return "conduit_io_control_plane_ns"
	}
	stripped := strings.TrimPrefix(proxyLabel, "conduit.io/proxy-")
	if stripped == "job" {
		return "k8s_job"
	}
	return strings.Replace(stripped, "-", "_", -1)
}

// TLSIdentity is the identity of a pod template (Deployment, Pod,
// ReplicationController, etc.).
type TLSIdentity struct {
	// The name of the pod template.
	Name string

	// Kind is the result of GetOwnerType(pod.ObjectMeta) for a pod template.
	Kind string

	// Namespace is the pod template's namespace.
	Namespace string

	// ControllerNamespace is the namespace of the controller for the pod.
	ControllerNamespace string
}

func (i TLSIdentity) ToDNSName() string {
	return fmt.Sprintf("%s.%s.%s.conduit-managed.%s.svc.cluster.local", i.Name,
		i.Kind, i.Namespace, i.ControllerNamespace)
}

func (i TLSIdentity) ToSecretName() string {
	return fmt.Sprintf("%s-%s-tls-conduit-io", i.Name, i.Kind)
}

func (i TLSIdentity) ToControllerIdentity() TLSIdentity {
	return TLSIdentity{
		Name:                "controller",
		Kind:                "deployment",
		Namespace:           i.ControllerNamespace,
		ControllerNamespace: i.ControllerNamespace,
	}
}
