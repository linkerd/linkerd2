/*
Kubernetes labels and annotations used in Linkerd's control plane and data plane
Kubernetes configs.
*/

package k8s

import (
	"fmt"

	"github.com/linkerd/linkerd2/pkg/version"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

const (
	/*
	 * Labels
	 */

	// Prefix is the prefix common to all labels and annotations injected by Linkerd
	Prefix = "linkerd.io"

	// ControllerComponentLabel identifies this object as a component of Linkerd's
	// control plane (e.g. web, controller).
	ControllerComponentLabel = Prefix + "/control-plane-component"

	// ControllerNSLabel is injected into mesh-enabled apps, identifying the
	// namespace of the Linkerd control plane.
	ControllerNSLabel = Prefix + "/control-plane-ns"

	// ProxyDeploymentLabel is injected into mesh-enabled apps, identifying the
	// deployment that this proxy belongs to.
	ProxyDeploymentLabel = Prefix + "/proxy-deployment"

	// ProxyReplicationControllerLabel is injected into mesh-enabled apps,
	// identifying the ReplicationController that this proxy belongs to.
	ProxyReplicationControllerLabel = Prefix + "/proxy-replicationcontroller"

	// ProxyReplicaSetLabel is injected into mesh-enabled apps, identifying the
	// ReplicaSet that this proxy belongs to.
	ProxyReplicaSetLabel = Prefix + "/proxy-replicaset"

	// ProxyJobLabel is injected into mesh-enabled apps, identifying the Job that
	// this proxy belongs to.
	ProxyJobLabel = Prefix + "/proxy-job"

	// ProxyDaemonSetLabel is injected into mesh-enabled apps, identifying the
	// DaemonSet that this proxy belongs to.
	ProxyDaemonSetLabel = Prefix + "/proxy-daemonset"

	// ProxyStatefulSetLabel is injected into mesh-enabled apps, identifying the
	// StatefulSet that this proxy belongs to.
	ProxyStatefulSetLabel = Prefix + "/proxy-statefulset"

	/*
	 * Annotations
	 */

	// CreatedByAnnotation indicates the source of the injected data plane
	// (e.g. linkerd/cli v2.0.0).
	CreatedByAnnotation = Prefix + "/created-by"

	// IdentityIssuerExpiryAnnotation indicates the time at which this set of identity
	// issuer credentials will cease to be valid.
	IdentityIssuerExpiryAnnotation = Prefix + "/identity-issuer-expiry"

	// ProxyVersionAnnotation indicates the version of the injected data plane
	// (e.g. v0.1.3).
	ProxyVersionAnnotation = Prefix + "/proxy-version"

	// ProxyInjectAnnotation controls whether or not a pod should be injected
	// when set on a pod spec. When set on a namespace spec, it applies to all
	// pods in the namespace. Supported values are "enabled" or "disabled"
	ProxyInjectAnnotation = Prefix + "/inject"

	// ProxyInjectEnabled is assigned to the ProxyInjectAnnotation annotation to
	// enable injection for a pod or namespace.
	ProxyInjectEnabled = "enabled"

	// ProxyInjectDisabled is assigned to the ProxyInjectAnnotation annotation to
	// disable injection for a pod or namespace.
	ProxyInjectDisabled = "disabled"

	// IdentityModeAnnotation controls how a pod participates
	// in service identity.
	IdentityModeAnnotation = Prefix + "/identity-mode"

	/*
	 * Proxy config annotations
	 */

	// ProxyConfigAnnotationsPrefix is the prefix of all config-related annotations
	ProxyConfigAnnotationsPrefix = "config.linkerd.io"

	// ProxyImageAnnotation can be used to override the proxyImage config.
	ProxyImageAnnotation = ProxyConfigAnnotationsPrefix + "/proxy-image"

	// ProxyImagePullPolicyAnnotation can be used to override the
	// proxyImagePullPolicy and proxyInitImagePullPolicy configs.
	ProxyImagePullPolicyAnnotation = ProxyConfigAnnotationsPrefix + "/image-pull-policy"

	// ProxyInitImageAnnotation can be used to override the proxyInitImage
	// config.
	ProxyInitImageAnnotation = ProxyConfigAnnotationsPrefix + "/init-image"

	// ProxyControlPortAnnotation can be used to override the controlPort config.
	ProxyControlPortAnnotation = ProxyConfigAnnotationsPrefix + "/control-port"

	// ProxyIgnoreInboundPortsAnnotation can be used to override the
	// ignoreInboundPorts config.
	ProxyIgnoreInboundPortsAnnotation = ProxyConfigAnnotationsPrefix + "/skip-inbound-ports"

	// ProxyIgnoreOutboundPortsAnnotation can be used to override the
	// ignoreOutboundPorts config.
	ProxyIgnoreOutboundPortsAnnotation = ProxyConfigAnnotationsPrefix + "/skip-outbound-ports"

	// ProxyInboundPortAnnotation can be used to override the inboundPort config.
	ProxyInboundPortAnnotation = ProxyConfigAnnotationsPrefix + "/inbound-port"

	// ProxyAdminPortAnnotation can be used to override the adminPort config.
	ProxyAdminPortAnnotation = ProxyConfigAnnotationsPrefix + "/admin-port"

	// ProxyOutboundPortAnnotation can be used to override the outboundPort
	// config.
	ProxyOutboundPortAnnotation = ProxyConfigAnnotationsPrefix + "/outbound-port"

	// ProxyCPURequestAnnotation can be used to override the requestCPU config.
	ProxyCPURequestAnnotation = ProxyConfigAnnotationsPrefix + "/proxy-cpu-request"

	// ProxyMemoryRequestAnnotation can be used to override the
	// requestMemoryConfig.
	ProxyMemoryRequestAnnotation = ProxyConfigAnnotationsPrefix + "/proxy-memory-request"

	// ProxyCPULimitAnnotation can be used to override the limitCPU config.
	ProxyCPULimitAnnotation = ProxyConfigAnnotationsPrefix + "/proxy-cpu-limit"

	// ProxyMemoryLimitAnnotation can be used to override the limitMemory config.
	ProxyMemoryLimitAnnotation = ProxyConfigAnnotationsPrefix + "/proxy-memory-limit"

	// ProxyUIDAnnotation can be used to override the UID config.
	ProxyUIDAnnotation = ProxyConfigAnnotationsPrefix + "/proxy-uid"

	// ProxyLogLevelAnnotation can be used to override the log level config.
	ProxyLogLevelAnnotation = ProxyConfigAnnotationsPrefix + "/proxy-log-level"

	// ProxyDisableExternalProfilesAnnotation can be used to override the
	// disableExternalProfilesAnnotation config.
	ProxyDisableExternalProfilesAnnotation = ProxyConfigAnnotationsPrefix + "/disable-external-profiles"

	// IdentityModeDefault is assigned to IdentityModeAnnotation to
	// use the control plane's default identity scheme.
	IdentityModeDefault = "default"

	// IdentityModeDisabled is assigned to IdentityModeAnnotation to
	// disable the proxy from participating in automatic identity.
	IdentityModeDisabled = "disabled"

	/*
	 * Component Names
	 */

	// ConfigConfigMapName is the name of the ConfigMap containing the linkerd controller configuration.
	ConfigConfigMapName = "linkerd-config"

	// InitContainerName is the name assigned to the injected init container.
	InitContainerName = "linkerd-init"

	// ProxyContainerName is the name assigned to the injected proxy container.
	ProxyContainerName = "linkerd-proxy"

	// IdentityEndEntityVolumeName is the name assigned the temporary end-entity
	// volume mounted into each proxy to store identity credentials.
	IdentityEndEntityVolumeName = "linkerd-identity-end-entity"

	// ProxyPortName is the name of the Linkerd Proxy's proxy port.
	ProxyPortName = "linkerd-proxy"

	// ProxyAdminPortName is the name of the Linkerd Proxy's metrics port.
	ProxyAdminPortName = "linkerd-admin"

	// ProxyInjectorWebhookConfig is the name of the mutating webhook
	// configuration resource of the proxy-injector webhook.
	ProxyInjectorWebhookConfig = "linkerd-proxy-injector-webhook-config"

	/*
	 * Mount paths
	 */

	// MountPathBase is the base directory of the mount path.
	MountPathBase = "/var/run/linkerd"

	// MountPathServiceAccount is the default path where Kuberenetes stores
	// the service account token
	MountPathServiceAccount = "/var/run/secrets/kubernetes.io/serviceaccount"

	// MountPathGlobalConfig is the path at which the global config file is mounted.
	MountPathGlobalConfig = MountPathBase + "/config/global"

	// MountPathProxyConfig is the path at which the global config file is mounted.
	MountPathProxyConfig = MountPathBase + "/config/proxy"

	// MountPathEndEntity is the path at which a tmpfs directory is mounted to
	// store identity credentials.
	MountPathEndEntity = MountPathBase + "/identity/end-entity"

	// IdentityServiceAccountTokenPath is the path to the kubernetes service
	// account token used by proxies to provision identity.
	//
	// In the future, this should be changed to a time- and audience-scoped secret.
	IdentityServiceAccountTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
)

// CreatedByAnnotationValue returns the value associated with
// CreatedByAnnotation.
func CreatedByAnnotationValue() string {
	return fmt.Sprintf("%s %s", "linkerd/cli", version.Version)
}

// GetServiceAccountAndNS returns the pod's serviceaccount and namespace.
func GetServiceAccountAndNS(pod *corev1.Pod) (sa string, ns string) {
	sa = pod.Spec.ServiceAccountName
	if sa == "" {
		sa = "default"
	}

	ns = pod.GetNamespace()
	if ns == "" {
		ns = "default"
	}

	return
}

// GetPodLabels returns the set of prometheus owner labels for a given pod
func GetPodLabels(ownerKind, ownerName string, pod *corev1.Pod) map[string]string {
	labels := map[string]string{"pod": pod.Name}

	l5dLabel := KindToL5DLabel(ownerKind)
	labels[l5dLabel] = ownerName

	labels["serviceaccount"], _ = GetServiceAccountAndNS(pod)

	if controllerNS := pod.Labels[ControllerNSLabel]; controllerNS != "" {
		labels["control_plane_ns"] = controllerNS
	}

	if pth := pod.Labels[appsv1.DefaultDeploymentUniqueLabelKey]; pth != "" {
		labels["pod_template_hash"] = pth
	}

	return labels
}

// IsMeshed returns whether a given Pod is in a given controller's service mesh.
func IsMeshed(pod *corev1.Pod, controllerNS string) bool {
	return pod.Labels[ControllerNSLabel] == controllerNS
}
