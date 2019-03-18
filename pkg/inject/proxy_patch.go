package inject

import (
	"fmt"
	"strings"

	"github.com/linkerd/linkerd2/pkg/k8s"
	v1 "k8s.io/api/core/v1"
)

func newProxyPatch(proxy *v1.Container, identity k8s.TLSIdentity, config *ResourceConfig) *Patch {
	patch := patchKind(config.meta.Kind)

	// Special case if the caller specifies that
	// LINKERD2_PROXY_OUTBOUND_ROUTER_CAPACITY be set on the pod.
	// We key off of any container image in the pod. Ideally we would instead key
	// off of something at the top-level of the PodSpec, but there is nothing
	// easily identifiable at that level.
	// Currently this will be set on any proxy that gets injected into a Prometheus pod,
	// not just the one in Linkerd's Control Plane.
	for _, container := range config.pod.spec.Containers {
		if capacity, ok := config.proxyOutboundCapacity[container.Image]; ok {
			proxy.Env = append(proxy.Env,
				v1.EnvVar{
					Name:  "LINKERD2_PROXY_OUTBOUND_ROUTER_CAPACITY",
					Value: fmt.Sprintf("%d", capacity),
				},
			)
			break
		}
	}

	if config.globalConfig.GetIdentityContext() != nil {
		yes := true

		configMapVolume := &v1.Volume{
			Name: k8s.TLSTrustAnchorVolumeName,
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: v1.LocalObjectReference{Name: k8s.TLSTrustAnchorConfigMapName},
					Optional:             &yes,
				},
			},
		}
		secretVolume := &v1.Volume{
			Name: k8s.TLSSecretsVolumeName,
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: identity.ToSecretName(),
					Optional:   &yes,
				},
			},
		}

		base := "/var/linkerd-io"
		configMapBase := base + "/trust-anchors"
		secretBase := base + "/identity"
		tlsEnvVars := []v1.EnvVar{
			{Name: "LINKERD2_PROXY_TLS_TRUST_ANCHORS", Value: configMapBase + "/" + k8s.TLSTrustAnchorFileName},
			{Name: "LINKERD2_PROXY_TLS_CERT", Value: secretBase + "/" + k8s.TLSCertFileName},
			{Name: "LINKERD2_PROXY_TLS_PRIVATE_KEY", Value: secretBase + "/" + k8s.TLSPrivateKeyFileName},
			{
				Name:  "LINKERD2_PROXY_TLS_POD_IDENTITY",
				Value: identity.ToDNSName(),
			},
			{Name: "LINKERD2_PROXY_CONTROLLER_NAMESPACE", Value: config.globalConfig.GetLinkerdNamespace()},
			{Name: "LINKERD2_PROXY_TLS_CONTROLLER_IDENTITY", Value: identity.ToControllerIdentity().ToDNSName()},
		}

		proxy.Env = append(proxy.Env, tlsEnvVars...)
		proxy.VolumeMounts = []v1.VolumeMount{
			{Name: configMapVolume.Name, MountPath: configMapBase, ReadOnly: true},
			{Name: secretVolume.Name, MountPath: secretBase, ReadOnly: true},
		}

		if len(config.pod.spec.Volumes) == 0 {
			patch.addVolumeRoot()
		}
		patch.addVolume(configMapVolume)
		patch.addVolume(secretVolume)
	}

	patch.addContainer(proxy)
	return patch
}

func newProxyInitPatch(proxyInit *v1.Container, config *ResourceConfig) *Patch {
	patch := patchKind(config.meta.Kind)
	if len(config.pod.spec.InitContainers) == 0 {
		patch.addInitContainerRoot()
	}

	patch.addInitContainer(proxyInit)
	return patch
}

func newObjectMetaPatch(config *ResourceConfig) *Patch {
	patch := patchKind(config.meta.Kind)
	if len(config.pod.meta.Annotations) == 0 {
		patch.addPodAnnotationsRoot()
	}
	patch.addPodAnnotation(k8s.ProxyVersionAnnotation, config.globalConfig.GetVersion())

	if config.globalConfig.GetIdentityContext() != nil {
		patch.addPodAnnotation(k8s.IdentityModeAnnotation, k8s.IdentityModeOptional)
	} else {
		patch.addPodAnnotation(k8s.IdentityModeAnnotation, k8s.IdentityModeDisabled)
	}

	for k, v := range config.pod.labels {
		patch.addPodLabel(k, v)
	}

	return patch
}

func newOverrideProxyPatch(proxy *v1.Container, conf *ResourceConfig) *Patch {
	patch := patchKind(conf.meta.Kind)
	for i, c := range conf.pod.spec.Containers {
		if c.Name == k8s.ProxyContainerName {
			patch.replaceContainer(proxy, i)
			break
		}
	}

	return patch
}

func newOverrideProxyInitPatch(proxyInit *v1.Container, conf *ResourceConfig) *Patch {
	patch := patchKind(conf.meta.Kind)
	for i, c := range conf.pod.spec.InitContainers {
		if c.Name == k8s.InitContainerName {
			patch.replaceInitContainer(proxyInit, i)
			break
		}
	}

	return patch
}

func patchKind(kind string) *Patch {
	if strings.ToLower(kind) == k8s.Pod {
		return NewPatchPod()
	}
	return NewPatchDeployment()
}
