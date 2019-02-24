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

	// ProxyInjectAnnotation controls whether or not a pod should be injected
	// when set on a pod spec. When set on a namespace spec, it applies to all
	// pods in the namespace. Supported values are "enabled" or "disabled"
	ProxyInjectAnnotation = "linkerd.io/inject"

	// ProxyInjectEnabled is assigned to the ProxyInjectAnnotation annotation to
	// enable injection for a pod or namespace.
	ProxyInjectEnabled = "enabled"

	// ProxyInjectDisabled is assigned to the ProxyInjectAnnotation annotation to
	// disable injection for a pod or namespace.
	ProxyInjectDisabled = "disabled"

	// ProxyIdentityModeAnnotation controls how a pod participates
	// in service identity.
	ProxyIdentityModeAnnotation = "linkerd.io/identity-mode"

	// ProxyIdentityModeDisabled is assigned to ProxyIdentityModeAnnotation to
	// disable the proxy from participating in automatic identity.
	ProxyIdentityModeDisabled = "disabled"

	// ProxyIdentityModeOptional is assigned to ProxyIdentityModeAnnotation to
	// optionally configure the proxy to participate in automatic identity.
	//
	// Will be deprecated soon.
	ProxyIdentityModeOptional = "optional"

	/*
	 * Component Names
	 */

	// InitContainerName is the name assigned to the injected init container.
	InitContainerName = "linkerd-init"

	// ProxyContainerName is the name assigned to the injected proxy container.
	ProxyContainerName = "linkerd-proxy"

	// ProxyInjectorWebhookConfig is the name of the mutating webhook
	// configuration resource of the proxy-injector webhook.
	ProxyInjectorWebhookConfig = "linkerd-proxy-injector-webhook-config"

	// ProxySpecFileName is the name (key) within the proxy-injector ConfigMap
	// that contains the proxy container spec.
	ProxySpecFileName = "proxy.yaml"

	// ProxyInitSpecFileName is the name (key) within the
	// proxy-injector ConfigMap that contains the proxy-init container spec.
	ProxyInitSpecFileName = "proxy-init.yaml"

	// TLSTrustAnchorVolumeName is the name of the trust anchor volume,
	// used when injecting a proxy with TLS enabled.
	TLSTrustAnchorVolumeName = "linkerd-trust-anchors"

	// TLSSecretsVolumeName is the name of the volume holding the secrets,
	// when injecting a proxy with TLS enabled.
	TLSSecretsVolumeName = "linkerd-secrets"

	// TLSTrustAnchorVolumeSpecFileName is the name (key) within the
	// proxy-injector ConfigMap that contains the trust anchors volume spec.
	TLSTrustAnchorVolumeSpecFileName = "linkerd-trust-anchors.yaml"

	// TLSIdentityVolumeSpecFileName is the name (key) within the
	// proxy-injector ConfigMap that contains the TLS identity secrets volume spec.
	TLSIdentityVolumeSpecFileName = "linkerd-secrets.yaml"

	// TLSTrustAnchorConfigMapName is the name of the ConfigMap that holds the
	// trust anchors (trusted root certificates).
	TLSTrustAnchorConfigMapName = "linkerd-ca-bundle"

	// TLSTrustAnchorFileName is the name (key) within the trust anchor ConfigMap
	// that contains the actual trust anchor bundle.
	TLSTrustAnchorFileName = "trust-anchors.pem"

	// TLSCertFileName is the name (key) within proxy-injector ConfigMap that
	// contains the TLS certificate.
	TLSCertFileName = "certificate.crt"

	// TLSPrivateKeyFileName is the name (key) within proxy-injector ConfigMap
	// that contains the TLS private key.
	TLSPrivateKeyFileName = "private-key.p8"

	/*
	 * Mount paths
	 */

	// MountPathBase is the base directory of the mount path
	MountPathBase = "/var/linkerd-io"
)

// InjectedLabels contains the list of label keys subjected to be injected by Linkerd into resource definitions
var InjectedLabels = []string{ControllerNSLabel, ProxyDeploymentLabel, ProxyReplicationControllerLabel,
	ProxyReplicaSetLabel, ProxyJobLabel, ProxyDaemonSetLabel, ProxyStatefulSetLabel}

var (
	// MountPathTLSTrustAnchor is the path at which the trust anchor file is
	// mounted
	MountPathTLSTrustAnchor = MountPathBase + "/trust-anchors/" + TLSTrustAnchorFileName

	// MountPathTLSIdentityCert is the path at which the TLS identity cert file is
	// mounted
	MountPathTLSIdentityCert = MountPathBase + "/identity/" + TLSCertFileName

	// MountPathTLSIdentityKey is the path at which the TLS identity key file is
	// mounted
	MountPathTLSIdentityKey = MountPathBase + "/identity/" + TLSPrivateKeyFileName

	// MountPathConfigProxySpec is the path at which the proxy container spec is
	// mounted to the proxy-injector
	MountPathConfigProxySpec = MountPathBase + "/config/" + ProxySpecFileName

	// MountPathConfigProxyInitSpec is the path at which the proxy-init container
	// spec is mounted to the proxy-injector
	MountPathConfigProxyInitSpec = MountPathBase + "/config/" + ProxyInitSpecFileName

	// MountPathTLSTrustAnchorVolumeSpec is the path at which the trust anchor
	// volume spec is mounted to the proxy-injector
	MountPathTLSTrustAnchorVolumeSpec = MountPathBase + "/config/" + TLSTrustAnchorVolumeSpecFileName

	// MountPathTLSIdentityVolumeSpec is the path at which the TLS identity
	// secret volume spec is mounted to the proxy-injector
	MountPathTLSIdentityVolumeSpec = MountPathBase + "/config/" + TLSIdentityVolumeSpecFileName
)

// CreatedByAnnotationValue returns the value associated with
// CreatedByAnnotation.
func CreatedByAnnotationValue() string {
	return fmt.Sprintf("linkerd/cli %s", version.Version)
}

// GetPodLabels returns the set of prometheus owner labels for a given pod
func GetPodLabels(ownerKind, ownerName string, pod *coreV1.Pod) map[string]string {
	labels := map[string]string{"pod": pod.Name}

	l5dLabel := KindToL5DLabel(ownerKind)
	labels[l5dLabel] = ownerName

	if controllerNS := pod.Labels[ControllerNSLabel]; controllerNS != "" {
		labels["control_plane_ns"] = controllerNS
	}

	if pth := pod.Labels[appsV1.DefaultDeploymentUniqueLabelKey]; pth != "" {
		labels["pod_template_hash"] = pth
	}

	return labels
}

// IsMeshed returns whether a given Pod is in a given controller's service mesh.
func IsMeshed(pod *coreV1.Pod, controllerNS string) bool {
	return pod.Labels[ControllerNSLabel] == controllerNS
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

// ToDNSName formats a TLSIdentity as a DNS name.
func (i TLSIdentity) ToDNSName() string {
	if i.Kind == Service {
		return fmt.Sprintf("%s.%s.svc", i.Name, i.Namespace)
	}
	return fmt.Sprintf("%s.%s.%s.linkerd-managed.%s.svc.cluster.local", i.Name,
		i.Kind, i.Namespace, i.ControllerNamespace)
}

// ToSecretName formats a TLSIdentity as a secret name.
func (i TLSIdentity) ToSecretName() string {
	return fmt.Sprintf("%s-%s-tls-linkerd-io", i.Name, i.Kind)
}

// ToControllerIdentity returns the TLSIdentity of the Linkerd Controller, given
// an arbitrary TLSIdentity.
func (i TLSIdentity) ToControllerIdentity() TLSIdentity {
	return TLSIdentity{
		Name:                "linkerd-controller",
		Kind:                "deployment",
		Namespace:           i.ControllerNamespace,
		ControllerNamespace: i.ControllerNamespace,
	}
}
