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

	// LinkerdNamespaceLabel is a label that helps identifying the namespaces
	// that contain a Linkerd control plane
	LinkerdNamespaceLabel = Prefix + "/is-control-plane"

	// LinkerdExtensionLabel is a label that helps identifying the namespace
	// that contain a Linkerd Extension
	LinkerdExtensionLabel = Prefix + "/extension"

	// ControllerComponentLabel identifies this object as a component of Linkerd's
	// control plane (e.g. web, controller).
	ControllerComponentLabel = Prefix + "/control-plane-component"

	// ExtensionAPIServerAuthenticationConfigMapName is the name of the ConfigMap where
	// authentication data for extension API servers is placed.
	ExtensionAPIServerAuthenticationConfigMapName = "extension-apiserver-authentication"

	// ExtensionAPIServerAuthenticationRequestHeaderClientCAFileKey is the key that
	// contains the value of the "--requestheader-client-ca-file" flag.
	ExtensionAPIServerAuthenticationRequestHeaderClientCAFileKey = "requestheader-client-ca-file"

	// RequireIDHeader signals to the proxy that a certain identity should be expected
	// of the remote peer
	RequireIDHeader = "l5d-require-id"

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

	// ProxyCronJobLabel is injected into mesh-enabled apps, identifying the
	// CronJob that this proxy belongs to.
	ProxyCronJobLabel = Prefix + "/proxy-cronjob"

	// WorkloadNamespaceLabel is injected into mesh-enabled apps, identifying the
	// Namespace that this proxy belongs to.
	WorkloadNamespaceLabel = Prefix + "/workload-ns"

	// Enabled is used by annotations whose valid values include "enabled".
	Enabled = "enabled"

	// Disabled is used by annotations whose valid values incluce "disabled".
	Disabled = "disabled"

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
	// pods in the namespace. Supported values are Enabled or Disabled
	ProxyInjectAnnotation = Prefix + "/inject"

	// ProxyInjectEnabled is assigned to the ProxyInjectAnnotation annotation to
	// enable injection for a pod or namespace.
	ProxyInjectEnabled = Enabled

	// ProxyInjectIngress is assigned to the ProxyInjectAnnotation annotation to
	// enable injection in ingress mode for a pod.
	ProxyInjectIngress = "ingress"

	// ProxyInjectDisabled is assigned to the ProxyInjectAnnotation annotation to
	// disable injection for a pod or namespace.
	ProxyInjectDisabled = Disabled

	// IdentityModeAnnotation controls how a pod participates
	// in service identity.
	IdentityModeAnnotation = Prefix + "/identity-mode"

	/*
	 * Proxy config annotations
	 */

	// ProxyConfigAnnotationsPrefix is the prefix of all config-related annotations
	ProxyConfigAnnotationsPrefix = "config.linkerd.io"

	// ProxyConfigAnnotationsPrefixAlpha is the prefix of newly released config-related annotations
	ProxyConfigAnnotationsPrefixAlpha = "config.alpha.linkerd.io"

	// ProxyImageAnnotation can be used to override the proxyImage config.
	ProxyImageAnnotation = ProxyConfigAnnotationsPrefix + "/proxy-image"

	// ProxyImagePullPolicyAnnotation can be used to override the
	// proxyImagePullPolicy and proxyInitImagePullPolicy configs.
	ProxyImagePullPolicyAnnotation = ProxyConfigAnnotationsPrefix + "/image-pull-policy"

	// ProxyInitImageAnnotation can be used to override the proxyInitImage
	// config.
	ProxyInitImageAnnotation = ProxyConfigAnnotationsPrefix + "/init-image"

	// ProxyInitImageVersionAnnotation can be used to override the proxy-init image version
	ProxyInitImageVersionAnnotation = ProxyConfigAnnotationsPrefix + "/init-image-version"

	// DebugImageAnnotation can be used to override the debugImage config.
	DebugImageAnnotation = ProxyConfigAnnotationsPrefix + "/debug-image"

	// DebugImageVersionAnnotation can be used to override the debugImageVersion config.
	DebugImageVersionAnnotation = ProxyConfigAnnotationsPrefix + "/debug-image-version"

	// DebugImagePullPolicyAnnotation can be used to override the debugImagePullPolicy config.
	DebugImagePullPolicyAnnotation = ProxyConfigAnnotationsPrefix + "/debug-image-pull-policy"

	// ProxyControlPortAnnotation can be used to override the controlPort config.
	ProxyControlPortAnnotation = ProxyConfigAnnotationsPrefix + "/control-port"

	// ProxyIgnoreInboundPortsAnnotation can be used to override the
	// ignoreInboundPorts config.
	ProxyIgnoreInboundPortsAnnotation = ProxyConfigAnnotationsPrefix + "/skip-inbound-ports"

	// ProxyOpaquePortsAnnotation can be used to override the opaquePorts
	// config.
	ProxyOpaquePortsAnnotation = ProxyConfigAnnotationsPrefix + "/opaque-ports"

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

	// ProxyLogFormatAnnotation can be used to override the log format config.
	ProxyLogFormatAnnotation = ProxyConfigAnnotationsPrefix + "/proxy-log-format"

	// ProxyEnableExternalProfilesAnnotation can be used to override the
	// disableExternalProfilesAnnotation config.
	ProxyEnableExternalProfilesAnnotation = ProxyConfigAnnotationsPrefix + "/enable-external-profiles"

	// ProxyVersionOverrideAnnotation can be used to override the proxy version config.
	ProxyVersionOverrideAnnotation = ProxyConfigAnnotationsPrefix + "/proxy-version"

	// ProxyRequireIdentityOnInboundPortsAnnotation can be used to configure the proxy
	// to always require identity on inbound ports
	ProxyRequireIdentityOnInboundPortsAnnotation = ProxyConfigAnnotationsPrefix + "/proxy-require-identity-inbound-ports"

	// ProxyOutboundConnectTimeout can be used to configure the outbound TCP connection
	// timeout in the proxy
	ProxyOutboundConnectTimeout = ProxyConfigAnnotationsPrefix + "/proxy-outbound-connect-timeout"

	// ProxyInboundConnectTimeout can be used to configure the inbound TCP connection
	// timeout in the proxy
	ProxyInboundConnectTimeout = ProxyConfigAnnotationsPrefix + "/proxy-inbound-connect-timeout"

	// ProxyEnableGatewayAnnotation can be used to configure the proxy
	// to operate as a gateway, routing requests that target the inbound router.
	ProxyEnableGatewayAnnotation = ProxyConfigAnnotationsPrefix + "/enable-gateway"

	// ProxyDisableIdentityAnnotation can be used to disable identity on the injected proxy.
	ProxyDisableIdentityAnnotation = ProxyConfigAnnotationsPrefix + "/disable-identity"

	// ProxyEnableDebugAnnotation is set to true if the debug container is
	// injected.
	ProxyEnableDebugAnnotation = ProxyConfigAnnotationsPrefix + "/enable-debug-sidecar"

	// CloseWaitTimeoutAnnotation configures nf_conntrack_tcp_timeout_close_wait.
	CloseWaitTimeoutAnnotation = ProxyConfigAnnotationsPrefix + "/close-wait-timeout"

	// ProxyWaitBeforeExitSecondsAnnotation makes the proxy container to wait for the given period before exiting
	// after the Pod entered the Terminating state. Must be smaller than terminationGracePeriodSeconds
	// configured for the Pod
	ProxyWaitBeforeExitSecondsAnnotation = ProxyConfigAnnotationsPrefixAlpha + "/proxy-wait-before-exit-seconds"

	// ProxyAwait can be used to force the application to wait for the proxy
	// to be ready.
	ProxyAwait = ProxyConfigAnnotationsPrefix + "/proxy-await"

	// IdentityModeDefault is assigned to IdentityModeAnnotation to
	// use the control plane's default identity scheme.
	IdentityModeDefault = "default"

	// IdentityModeDisabled is assigned to IdentityModeAnnotation to
	// disable the proxy from participating in automatic identity.
	IdentityModeDisabled = Disabled

	/*
	 * Component Names
	 */

	// ConfigConfigMapName is the name of the ConfigMap containing the linkerd controller configuration.
	ConfigConfigMapName = "linkerd-config"

	// AddOnsConfigMapName is the name of the ConfigMap containing the linkerd add-ons configuration.
	AddOnsConfigMapName = "linkerd-config-addons"

	// DebugSidecarName is the name of the default linkerd debug container
	DebugSidecarName = "linkerd-debug"

	// DebugSidecarImage is the image name of the default linkerd debug container
	DebugSidecarImage = "cr.l5d.io/linkerd/debug"

	// InitContainerName is the name assigned to the injected init container.
	InitContainerName = "linkerd-init"

	// InitXtablesLockVolumeMountName is the name of the volumeMount used by proxy-init
	// to handle iptables-legacy
	InitXtablesLockVolumeMountName = "linkerd-proxy-init-xtables-lock"

	// ProxyContainerName is the name assigned to the injected proxy container.
	ProxyContainerName = "linkerd-proxy"

	// IdentityEndEntityVolumeName is the name assigned the temporary end-entity
	// volume mounted into each proxy to store identity credentials.
	IdentityEndEntityVolumeName = "linkerd-identity-end-entity"

	// IdentityIssuerSecretName is the name of the Secret that stores issuer credentials.
	IdentityIssuerSecretName = "linkerd-identity-issuer"

	// IdentityIssuerSchemeLinkerd is the issuer secret scheme used by linkerd
	IdentityIssuerSchemeLinkerd = "linkerd.io/tls"

	// IdentityIssuerKeyName is the issuer's private key file.
	IdentityIssuerKeyName = "key.pem"

	// IdentityIssuerCrtName is the issuer's certificate file.
	IdentityIssuerCrtName = "crt.pem"

	// IdentityIssuerTrustAnchorsNameExternal is the issuer's certificate file (when using cert-manager).
	IdentityIssuerTrustAnchorsNameExternal = "ca.crt"

	// ProxyPortName is the name of the Linkerd Proxy's proxy port.
	ProxyPortName = "linkerd-proxy"

	// ProxyAdminPortName is the name of the Linkerd Proxy's metrics port.
	ProxyAdminPortName = "linkerd-admin"

	// ProxyInjectorWebhookServiceName is the name of the mutating webhook service
	ProxyInjectorWebhookServiceName = "linkerd-proxy-injector"

	// ProxyInjectorWebhookConfigName is the name of the mutating webhook configuration
	ProxyInjectorWebhookConfigName = ProxyInjectorWebhookServiceName + "-webhook-config"

	// SPValidatorWebhookServiceName is the name of the validating webhook service
	SPValidatorWebhookServiceName = "linkerd-sp-validator"

	// SPValidatorWebhookConfigName is the name of the validating webhook configuration
	SPValidatorWebhookConfigName = SPValidatorWebhookServiceName + "-webhook-config"

	// TapServiceName is the name of the tap APIService
	TapServiceName = "linkerd-tap"

	// TapAPIRegistrationServiceName is the name of the tap APIService registration resource
	TapAPIRegistrationServiceName = "v1alpha1.tap.linkerd.io"

	// AdmissionWebhookLabel indicates whether admission webhooks are enabled for a namespace
	AdmissionWebhookLabel = ProxyConfigAnnotationsPrefix + "/admission-webhooks"

	/*
	 * Mount paths
	 */

	// MountPathBase is the base directory of the mount path.
	MountPathBase = "/var/run/linkerd"

	// MountPathServiceAccount is the default path where Kubernetes stores
	// the service account token
	MountPathServiceAccount = "/var/run/secrets/kubernetes.io/serviceaccount"

	// MountPathGlobalConfig is the path at which the global config file is mounted.
	MountPathGlobalConfig = MountPathBase + "/config/global"

	// MountPathProxyConfig is the path at which the global config file is mounted.
	MountPathProxyConfig = MountPathBase + "/config/proxy"

	// MountPathInstallConfig is the path at which the install config file is mounted.
	MountPathInstallConfig = MountPathBase + "/config/install"

	// MountPathValuesConfig is the path at which the values config file is mounted.
	MountPathValuesConfig = MountPathBase + "/config/values"

	// MountPathEndEntity is the path at which a tmpfs directory is mounted to
	// store identity credentials.
	MountPathEndEntity = MountPathBase + "/identity/end-entity"

	// MountPathTLSBase is the path at which the TLS cert and key PEM files are mounted
	MountPathTLSBase = MountPathBase + "/tls"

	// MountPathTLSKeyPEM is the path at which the TLS key PEM file is mounted.
	MountPathTLSKeyPEM = MountPathTLSBase + "/tls.key"

	// MountPathTLSCrtPEM is the path at which the TLS cert PEM file is mounted.
	MountPathTLSCrtPEM = MountPathTLSBase + "/tls.crt"

	// MountPathXtablesLock is the path at which the proxy init container mounts xtables
	// This is necessary for xtables-legacy support
	MountPathXtablesLock = "/run"

	// IdentityServiceAccountTokenPath is the path to the kubernetes service
	// account token used by proxies to provision identity.
	//
	// In the future, this should be changed to a time- and audience-scoped secret.
	IdentityServiceAccountTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"

	/*
	 * Service mirror constants
	 */

	// SvcMirrorPrefix is the prefix common to all labels and annotations
	// and types used by the service mirror component
	SvcMirrorPrefix = "mirror.linkerd.io"

	// MirrorSecretType is the type of secret that is supposed to contain
	// the access information for remote clusters.
	MirrorSecretType = SvcMirrorPrefix + "/remote-kubeconfig"

	// DefaultExportedServiceSelector is the default label selector for exported
	// services.
	DefaultExportedServiceSelector = SvcMirrorPrefix + "/exported"

	// MirroredResourceLabel indicates that this resource is the result
	// of a mirroring operation (can be a namespace or a service)
	MirroredResourceLabel = SvcMirrorPrefix + "/mirrored-service"

	// MirroredGatewayLabel indicates that this is a mirrored gateway
	MirroredGatewayLabel = SvcMirrorPrefix + "/mirrored-gateway"

	// RemoteClusterNameLabel put on a local mirrored service, it
	// allows us to associate a mirrored service with a remote cluster
	RemoteClusterNameLabel = SvcMirrorPrefix + "/cluster-name"

	// RemoteResourceVersionAnnotation is the last observed remote resource
	// version of a mirrored resource. Useful when doing updates
	RemoteResourceVersionAnnotation = SvcMirrorPrefix + "/remote-resource-version"

	// RemoteServiceFqName is the fully qualified name of the mirrored service
	// on the remote cluster
	RemoteServiceFqName = SvcMirrorPrefix + "/remote-svc-fq-name"

	// RemoteGatewayResourceVersionAnnotation is the last observed remote resource
	// version of the gateway for a particular mirrored service. It is used
	// in cases we detect a change in a remote gateway
	RemoteGatewayResourceVersionAnnotation = SvcMirrorPrefix + "/remote-gateway-resource-version"

	// RemoteGatewayIdentity follows the same kind of logic as RemoteGatewayNameLabel
	RemoteGatewayIdentity = SvcMirrorPrefix + "/remote-gateway-identity"

	// GatewayIdentity can be found on the remote gateway service
	GatewayIdentity = SvcMirrorPrefix + "/gateway-identity"

	// GatewayProbePeriod the interval at which the health of the gateway should be probed
	GatewayProbePeriod = SvcMirrorPrefix + "/probe-period"

	// GatewayProbePath the path at which the health of the gateway should be probed
	GatewayProbePath = SvcMirrorPrefix + "/probe-path"

	// ConfigKeyName is the key in the secret that stores the kubeconfig needed to connect
	// to a remote cluster
	ConfigKeyName = "kubeconfig"

	// GatewayPortName is the name of the incoming port of the gateway
	GatewayPortName = "mc-gateway"

	// ProbePortName is the name of the probe port of the gateway
	ProbePortName = "mc-probe"

	// ServiceMirrorLabel is the value used in the controller component label
	ServiceMirrorLabel = "servicemirror"
)

// CreatedByAnnotationValue returns the value associated with
// CreatedByAnnotation.
func CreatedByAnnotationValue() string {
	return fmt.Sprintf("linkerd/cli %s", version.Version)
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
