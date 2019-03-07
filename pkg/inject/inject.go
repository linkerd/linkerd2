package inject

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	k8sMeta "k8s.io/apimachinery/pkg/api/meta"
	k8sResource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"
)

const (
	// localhostDNSNameOverride allows override of the controlPlaneDNS. This
	// must be in absolute form for the proxy to special-case it.
	localhostDNSNameOverride = "localhost."
	// controlPlanePodName default control plane pod name.
	controlPlanePodName = "linkerd-controller"
	// podNamespaceEnvVarName is the name of the variable used to pass the pod's namespace.
	podNamespaceEnvVarName = "LINKERD2_PROXY_POD_NAMESPACE"
	// defaultKeepaliveMs is used in the proxy configuration for remote connections
	defaultKeepaliveMs = 10000
	// destinationAPIPort is the port exposed by the linkerd-destination service
	destinationAPIPort = 8086
)

var injectableKinds = []string{
	k8s.DaemonSet,
	k8s.Deployment,
	k8s.Job,
	k8s.Pod,
	k8s.ReplicaSet,
	k8s.ReplicationController,
	k8s.StatefulSet,
}

// objMeta provides a generic struct to parse the names of Kubernetes objects
type objMeta struct {
	*metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
}

// ResourceConfig contains the parsed information for a given workload
type ResourceConfig struct {
	globalConfig          *config.Global
	proxyConfig           *config.Proxy
	nsAnnotations         map[string]string
	meta                  metav1.TypeMeta
	obj                   runtime.Object
	workLoadMeta          *metav1.ObjectMeta
	podMeta               objMeta
	podLabels             map[string]string
	podSpec               *v1.PodSpec
	dnsNameOverride       string
	proxyOutboundCapacity map[string]uint
	proxyConfigOverrides  map[string]string
}

// NewResourceConfig creates and initializes a ResourceConfig
func NewResourceConfig(globalConfig *config.Global, proxyConfig *config.Proxy) *ResourceConfig {
	return &ResourceConfig{
		globalConfig:          globalConfig,
		proxyConfig:           proxyConfig,
		podLabels:             map[string]string{k8s.ControllerNSLabel: globalConfig.GetLinkerdNamespace()},
		proxyOutboundCapacity: map[string]uint{},
		proxyConfigOverrides:  map[string]string{},
	}
}

// String satisfies the Stringer interface
func (conf *ResourceConfig) String() string {
	l := []string{}

	if conf.meta.Kind != "" {
		l = append(l, conf.meta.Kind)
	}
	if conf.workLoadMeta != nil {
		l = append(l, fmt.Sprintf("%s.%s", conf.workLoadMeta.GetName(), conf.workLoadMeta.GetNamespace()))
	}

	return strings.Join(l, "/")
}

// WithKind enriches ResourceConfig with the workload kind
func (conf *ResourceConfig) WithKind(kind string) *ResourceConfig {
	conf.meta = metav1.TypeMeta{Kind: kind}
	return conf
}

// WithNsAnnotations enriches ResourceConfig with the namespace annotations, that can
// be used in shouldInject()
func (conf *ResourceConfig) WithNsAnnotations(m map[string]string) *ResourceConfig {
	conf.nsAnnotations = m
	return conf
}

// WithProxyOutboundCapacity enriches ResourceConfig with a map of image names
// to capacities, which can be used by the install code to modify the outbound
// capacity for the prometheus container in the control plane install
func (conf *ResourceConfig) WithProxyOutboundCapacity(m map[string]uint) *ResourceConfig {
	conf.proxyOutboundCapacity = m
	return conf
}

// YamlMarshalObj returns the yaml for the workload in conf
func (conf *ResourceConfig) YamlMarshalObj() ([]byte, error) {
	return yaml.Marshal(conf.obj)
}

// ParseMetaAndYaml fills conf fields with both the metatada and the workload contents
func (conf *ResourceConfig) ParseMetaAndYaml(bytes []byte) (*Report, error) {
	if _, err := conf.ParseMeta(bytes); err != nil {
		return nil, err
	}
	r := newReport(conf)
	return &r, conf.parse(bytes)
}

// ParseMeta extracts metadata from bytes.
// It returns false if the workload's payload is empty
func (conf *ResourceConfig) ParseMeta(bytes []byte) (bool, error) {
	if err := yaml.Unmarshal(bytes, &conf.meta); err != nil {
		return false, err
	}
	if err := yaml.Unmarshal(bytes, &conf.podMeta); err != nil {
		return false, err
	}
	return conf.podMeta.ObjectMeta != nil, nil
}

// GetPatch returns the JSON patch containing the proxy and init containers specs, if any
func (conf *ResourceConfig) GetPatch(
	bytes []byte,
	shouldInject func(*ResourceConfig, Report) bool,
) (*Patch, []Report, error) {
	report := newReport(conf)
	log.Infof("working on %s %s..", strings.ToLower(conf.meta.Kind), report.Name)

	if err := conf.parse(bytes); err != nil {
		return nil, nil, err
	}

	conf.useOverridesOrDefaults()

	var patch *Patch
	if strings.ToLower(conf.meta.Kind) == k8s.Pod {
		patch = NewPatchPod()
	} else {
		patch = NewPatchDeployment()
	}

	// If we don't inject anything into the pod template then output the
	// original serialization of the original object. Otherwise, output the
	// serialization of the modified object.
	if conf.podSpec != nil {
		metaAccessor, err := k8sMeta.Accessor(conf.obj)
		if err != nil {
			return nil, nil, err
		}

		// The namespace isn't necessarily in the input so it has to be substituted
		// at runtime. The proxy recognizes the "$NAME" syntax for this variable
		// but not necessarily other variables.
		identity := k8s.TLSIdentity{
			Name:                metaAccessor.GetName(),
			Kind:                strings.ToLower(conf.meta.Kind),
			Namespace:           "$" + podNamespaceEnvVarName,
			ControllerNamespace: conf.globalConfig.GetLinkerdNamespace(),
		}

		report.update(conf)
		if shouldInject(conf, report) {
			conf.injectPodSpec(patch, identity)
			conf.injectObjectMeta(patch)
		}
	} else {
		report.UnsupportedResource = true
	}

	return patch, []Report{report}, nil
}

// KindInjectable returns true if the resource in conf can be injected with a proxy
func (conf *ResourceConfig) KindInjectable() bool {
	for _, kind := range injectableKinds {
		if strings.ToLower(conf.meta.Kind) == kind {
			return true
		}
	}
	return false
}

// Note this switch must be kept in sync with injectableKinds (declared above)
func (conf *ResourceConfig) getFreshWorkloadObj() runtime.Object {
	switch strings.ToLower(conf.meta.Kind) {
	case k8s.Deployment:
		return &v1beta1.Deployment{}
	case k8s.ReplicationController:
		return &v1.ReplicationController{}
	case k8s.ReplicaSet:
		return &v1beta1.ReplicaSet{}
	case k8s.Job:
		return &batchv1.Job{}
	case k8s.DaemonSet:
		return &v1beta1.DaemonSet{}
	case k8s.StatefulSet:
		return &appsv1.StatefulSet{}
	case k8s.Pod:
		return &v1.Pod{}
	}

	return nil
}

// JSONToYAML is a replacement for the same function in sigs.k8s.io/yaml
// that does conserve the field order as portrayed in k8s' api structs
func (conf *ResourceConfig) JSONToYAML(bytes []byte) ([]byte, error) {
	obj := conf.getFreshWorkloadObj()
	if err := json.Unmarshal(bytes, obj); err != nil {
		return nil, err
	}
	return yaml.Marshal(obj)
}

func (conf *ResourceConfig) parse(bytes []byte) error {
	// The Kubernetes API is versioned and each version has an API modeled
	// with its own distinct Go types. If we tell `yaml.Unmarshal()` which
	// version we support then it will provide a representation of that
	// object using the given type if possible. However, it only allows us
	// to supply one object (of one type), so first we have to determine
	// what kind of object `bytes` represents so we can pass an object of
	// the correct type to `yaml.Unmarshal()`.
	// ---------------------------------------
	// Note: bytes is expected to be YAML and will only modify it when a
	// supported type is found. Otherwise, conf is left unmodified.

	// When injecting the linkerd proxy into a linkerd controller pod. The linkerd proxy's
	// LINKERD2_PROXY_CONTROL_URL variable must be set to localhost for the following reasons:
	//	1. According to https://github.com/kubernetes/minikube/issues/1568, minikube has an issue
	//     where pods are unable to connect to themselves through their associated service IP.
	//     Setting the LINKERD2_PROXY_CONTROL_URL to localhost allows the proxy to bypass kube DNS
	//     name resolution as a workaround to this issue.
	//  2. We avoid the TLS overhead in encrypting and decrypting intra-pod traffic i.e. traffic
	//     between containers in the same pod.
	//  3. Using a Service IP instead of localhost would mean intra-pod traffic would be load-balanced
	//     across all controller pod replicas. This is undesirable as we would want all traffic between
	//	   containers to be self contained.
	//  4. We skip recording telemetry for intra-pod traffic within the control plane.

	obj := conf.getFreshWorkloadObj()

	switch v := obj.(type) {
	case *v1beta1.Deployment:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		if v.Name == controlPlanePodName && v.Namespace == conf.globalConfig.GetLinkerdNamespace() {
			conf.dnsNameOverride = localhostDNSNameOverride
		}

		conf.obj = v
		conf.workLoadMeta = &v.ObjectMeta
		conf.podLabels[k8s.ProxyDeploymentLabel] = v.Name
		conf.complete(&v.Spec.Template)

	case *v1.ReplicationController:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		conf.obj = v
		conf.workLoadMeta = &v.ObjectMeta
		conf.podLabels[k8s.ProxyReplicationControllerLabel] = v.Name
		conf.complete(v.Spec.Template)

	case *v1beta1.ReplicaSet:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		conf.obj = v
		conf.workLoadMeta = &v.ObjectMeta
		conf.podLabels[k8s.ProxyReplicaSetLabel] = v.Name
		conf.complete(&v.Spec.Template)

	case *batchv1.Job:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		conf.obj = v
		conf.workLoadMeta = &v.ObjectMeta
		conf.podLabels[k8s.ProxyJobLabel] = v.Name
		conf.complete(&v.Spec.Template)

	case *v1beta1.DaemonSet:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		conf.obj = v
		conf.workLoadMeta = &v.ObjectMeta
		conf.podLabels[k8s.ProxyDaemonSetLabel] = v.Name
		conf.complete(&v.Spec.Template)

	case *appsv1.StatefulSet:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		conf.obj = v
		conf.workLoadMeta = &v.ObjectMeta
		conf.podLabels[k8s.ProxyStatefulSetLabel] = v.Name
		conf.complete(&v.Spec.Template)

	case *v1.Pod:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		conf.obj = v
		conf.podSpec = &v.Spec
		conf.podMeta = objMeta{&v.ObjectMeta}
	}

	return nil
}

func (conf *ResourceConfig) complete(template *v1.PodTemplateSpec) {
	conf.podSpec = &template.Spec
	conf.podMeta = objMeta{&template.ObjectMeta}
}

// injectPodSpec adds linkerd sidecars to the provided PodSpec.
func (conf *ResourceConfig) injectPodSpec(patch *Patch, identity k8s.TLSIdentity) error {
	var (
		f = false

		controlPort       = conf.proxyConfigOverrides[k8s.ProxyControlPortAnnotation]
		metricsPort       = conf.proxyConfigOverrides[k8s.ProxyMetricsPortAnnotation]
		inboundSkipPorts  = conf.proxyConfigOverrides[k8s.ProxyIgnoreInboundPortsAnnotation]
		outboundSkipPorts = conf.proxyConfigOverrides[k8s.ProxyIgnoreOutboundPortsAnnotation]
		inboundPort       = conf.proxyConfigOverrides[k8s.ProxyInboundPortAnnotation]
		outboundPort      = conf.proxyConfigOverrides[k8s.ProxyOutboundPortAnnotation]
	)

	if len(inboundSkipPorts) > 0 {
		inboundSkipPorts += ","
	}
	inboundSkipPorts += controlPort + "," + metricsPort

	proxyUID, err := strconv.ParseInt(conf.proxyConfigOverrides[k8s.ProxyUIDAnnotation], 10, 64)
	if err != nil {
		return err
	}

	initArgs := []string{
		"--incoming-proxy-port", inboundPort,
		"--outgoing-proxy-port", outboundPort,
		"--proxy-uid", fmt.Sprintf("%d", proxyUID),
	}

	if len(inboundSkipPorts) > 0 {
		initArgs = append(initArgs, "--inbound-ports-to-ignore")
		initArgs = append(initArgs, inboundSkipPorts)
	}

	if len(outboundSkipPorts) > 0 {
		initArgs = append(initArgs, "--outbound-ports-to-ignore")
		initArgs = append(initArgs, outboundSkipPorts)
	}

	controlPlaneDNS := fmt.Sprintf("linkerd-destination.%s.svc.cluster.local", conf.globalConfig.GetLinkerdNamespace())
	if conf.dnsNameOverride != "" {
		controlPlaneDNS = conf.dnsNameOverride
	}

	probePort, err := strconv.ParseUint(metricsPort, 10, 32)
	if err != nil {
		return err
	}
	proxyProbe := v1.Probe{
		Handler: v1.Handler{
			HTTPGet: &v1.HTTPGetAction{
				Path: "/metrics",
				Port: intstr.IntOrString{
					IntVal: int32(probePort),
				},
			},
		},
		InitialDelaySeconds: 10,
	}

	resources := v1.ResourceRequirements{
		Requests: v1.ResourceList{},
		Limits:   v1.ResourceList{},
	}

	if request := conf.proxyConfigOverrides[k8s.ProxyRequestCPUAnnotation]; request != "" {
		resources.Requests["cpu"] = k8sResource.MustParse(request)
	}

	if request := conf.proxyConfigOverrides[k8s.ProxyRequestMemoryAnnotation]; request != "" {
		resources.Requests["memory"] = k8sResource.MustParse(request)
	}

	if limit := conf.proxyConfigOverrides[k8s.ProxyLimitCPUAnnotation]; limit != "" {
		resources.Limits["cpu"] = k8sResource.MustParse(limit)
	}

	if limit := conf.proxyConfigOverrides[k8s.ProxyLimitMemoryAnnotation]; limit != "" {
		resources.Limits["memory"] = k8sResource.MustParse(limit)
	}

	disableExternalProfiles, err := strconv.ParseBool(conf.proxyConfigOverrides[k8s.ProxyDisableExternalProfilesAnnotation])
	if err != nil {
		return err
	}

	profileSuffixes := "."
	if disableExternalProfiles {
		profileSuffixes = "svc.cluster.local."
	}

	inboundPortInt, err := strconv.ParseInt(inboundPort, 10, 32)
	if err != nil {
		return nil
	}

	metricsPortInt, err := strconv.ParseInt(metricsPort, 10, 32)
	if err != nil {
		return nil
	}

	sidecar := v1.Container{
		Name:                     k8s.ProxyContainerName,
		Image:                    conf.taggedProxyImage(),
		ImagePullPolicy:          v1.PullPolicy(conf.proxyConfigOverrides[k8s.ProxyImagePullPolicyAnnotation]),
		TerminationMessagePolicy: v1.TerminationMessageFallbackToLogsOnError,
		SecurityContext: &v1.SecurityContext{
			RunAsUser: &proxyUID,
		},
		Ports: []v1.ContainerPort{
			{
				Name:          k8s.ProxyPortName,
				ContainerPort: int32(inboundPortInt),
			},
			{
				Name:          k8s.ProxyMetricsPortName,
				ContainerPort: int32(metricsPortInt),
			},
		},
		Resources: resources,
		Env: []v1.EnvVar{
			{Name: "LINKERD2_PROXY_LOG", Value: conf.proxyConfigOverrides[k8s.ProxyLogLevelAnnotation]},
			{
				Name:  "LINKERD2_PROXY_CONTROL_URL",
				Value: fmt.Sprintf("tcp://%s:%d", controlPlaneDNS, destinationAPIPort),
			},
			{Name: "LINKERD2_PROXY_CONTROL_LISTENER", Value: fmt.Sprintf("tcp://0.0.0.0:%s", controlPort)},
			{Name: "LINKERD2_PROXY_METRICS_LISTENER", Value: fmt.Sprintf("tcp://0.0.0.0:%s", metricsPort)},
			{Name: "LINKERD2_PROXY_OUTBOUND_LISTENER", Value: fmt.Sprintf("tcp://127.0.0.1:%s", outboundPort)},
			{Name: "LINKERD2_PROXY_INBOUND_LISTENER", Value: fmt.Sprintf("tcp://0.0.0.0:%s", inboundPort)},
			{Name: "LINKERD2_PROXY_DESTINATION_PROFILE_SUFFIXES", Value: profileSuffixes},
			{
				Name:      podNamespaceEnvVarName,
				ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
			},
			{Name: "LINKERD2_PROXY_INBOUND_ACCEPT_KEEPALIVE", Value: fmt.Sprintf("%dms", defaultKeepaliveMs)},
			{Name: "LINKERD2_PROXY_OUTBOUND_CONNECT_KEEPALIVE", Value: fmt.Sprintf("%dms", defaultKeepaliveMs)},
			{Name: "LINKERD2_PROXY_ID", Value: identity.ToDNSName()},
		},
		LivenessProbe:  &proxyProbe,
		ReadinessProbe: &proxyProbe,
	}

	// Special case if the caller specifies that
	// LINKERD2_PROXY_OUTBOUND_ROUTER_CAPACITY be set on the pod.
	// We key off of any container image in the pod. Ideally we would instead key
	// off of something at the top-level of the PodSpec, but there is nothing
	// easily identifiable at that level.
	// Currently this will bet set on any proxy that gets injected into a Prometheus pod,
	// not just the one in Linkerd's Control Plane.
	for _, container := range conf.podSpec.Containers {
		if capacity, ok := conf.proxyOutboundCapacity[container.Image]; ok {
			sidecar.Env = append(sidecar.Env,
				v1.EnvVar{
					Name:  "LINKERD2_PROXY_OUTBOUND_ROUTER_CAPACITY",
					Value: fmt.Sprintf("%d", capacity),
				},
			)
			break
		}
	}

	if conf.globalConfig.GetIdentityContext() != nil {
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
			{Name: "LINKERD2_PROXY_CONTROLLER_NAMESPACE", Value: conf.globalConfig.GetLinkerdNamespace()},
			{Name: "LINKERD2_PROXY_TLS_CONTROLLER_IDENTITY", Value: identity.ToControllerIdentity().ToDNSName()},
		}

		sidecar.Env = append(sidecar.Env, tlsEnvVars...)
		sidecar.VolumeMounts = []v1.VolumeMount{
			{Name: configMapVolume.Name, MountPath: configMapBase, ReadOnly: true},
			{Name: secretVolume.Name, MountPath: secretBase, ReadOnly: true},
		}

		if len(conf.podSpec.Volumes) == 0 {
			patch.addVolumeRoot()
		}
		patch.addVolume(configMapVolume)
		patch.addVolume(secretVolume)
	}

	patch.addContainer(&sidecar)

	if !conf.globalConfig.GetCniEnabled() {
		nonRoot := false
		runAsUser := int64(0)
		initContainer := &v1.Container{
			Name:                     k8s.InitContainerName,
			Image:                    conf.taggedProxyInitImage(),
			ImagePullPolicy:          v1.PullPolicy(conf.proxyConfigOverrides[k8s.ProxyInitImagePullPolicyAnnotation]),
			TerminationMessagePolicy: v1.TerminationMessageFallbackToLogsOnError,
			Args:                     initArgs,
			SecurityContext: &v1.SecurityContext{
				Capabilities: &v1.Capabilities{
					Add: []v1.Capability{v1.Capability("NET_ADMIN")},
				},
				Privileged:   &f,
				RunAsNonRoot: &nonRoot,
				RunAsUser:    &runAsUser,
			},
		}
		if len(conf.podSpec.InitContainers) == 0 {
			patch.addInitContainerRoot()
		}
		patch.addInitContainer(initContainer)
	}

	return nil
}

// Given a ObjectMeta, update ObjectMeta in place with the new labels and
// annotations.
func (conf *ResourceConfig) injectObjectMeta(patch *Patch) {
	if len(conf.podMeta.Annotations) == 0 {
		patch.addPodAnnotationsRoot()
	}
	patch.addPodAnnotation(k8s.ProxyVersionAnnotation, conf.globalConfig.GetVersion())

	if conf.globalConfig.GetIdentityContext() != nil {
		patch.addPodAnnotation(k8s.IdentityModeAnnotation, k8s.IdentityModeOptional)
	} else {
		patch.addPodAnnotation(k8s.IdentityModeAnnotation, k8s.IdentityModeDisabled)
	}

	for k, v := range conf.podLabels {
		patch.addPodLabel(k, v)
	}
}

// AddRootLabels adds all the pod labels into the root workload (e.g. Deployment)
func (conf *ResourceConfig) AddRootLabels(patch *Patch) {
	for k, v := range conf.podLabels {
		patch.addRootLabel(k, v)
	}
}

func (conf *ResourceConfig) taggedProxyImage() string {
	return fmt.Sprintf("%s:%s",
		conf.proxyConfigOverrides[k8s.ProxyImageAnnotation],
		conf.globalConfig.GetVersion())
}

func (conf *ResourceConfig) taggedProxyInitImage() string {
	return fmt.Sprintf("%s:%s",
		conf.proxyConfigOverrides[k8s.ProxyInitImageAnnotation],
		conf.globalConfig.GetVersion())
}

func (conf *ResourceConfig) useOverridesOrDefaults() {
	if !conf.KindInjectable() {
		return
	}

	log.Debugf("object annotations: %+v\n", conf.podMeta.Annotations)
	log.Debugf("namespace annotations: %+v\n", conf.nsAnnotations)

	useDefault := true
	for _, annotation := range k8s.ProxyConfigAnnotations {
		if value, exists := conf.podMeta.Annotations[annotation]; exists {
			typed := conf.annotationValueType(annotation, value, !useDefault)
			conf.proxyConfigOverrides[annotation] = typed
			continue
		}

		if value, exists := conf.nsAnnotations[annotation]; exists {
			typed := conf.annotationValueType(annotation, value, !useDefault)
			conf.proxyConfigOverrides[annotation] = typed
			continue
		}

		typed := conf.annotationValueType(annotation, "", useDefault)
		conf.proxyConfigOverrides[annotation] = typed
	}

	log.Debugf("proxy config: %+v", conf.proxyConfigOverrides)
}

func (conf *ResourceConfig) annotationValueType(annotation, strValue string, useDefault bool) string {
	var value string
	switch annotation {
	case k8s.ProxyImageAnnotation:
		if useDefault {
			return conf.proxyConfig.GetProxyImage().GetImageName()
		}
		value = strValue

	case k8s.ProxyImagePullPolicyAnnotation:
		if useDefault {
			return conf.proxyConfig.GetProxyImage().GetPullPolicy()
		}
		value = strValue

	case k8s.ProxyInitImageAnnotation:
		if useDefault {
			return conf.proxyConfig.GetProxyInitImage().GetImageName()
		}
		value = strValue

	case k8s.ProxyInitImagePullPolicyAnnotation:
		if useDefault {
			return conf.proxyConfig.GetProxyInitImage().GetPullPolicy()
		}
		value = strValue

	case k8s.ProxyIgnoreInboundPortsAnnotation:
		if useDefault && len(conf.proxyConfig.GetIgnoreInboundPorts()) > 0 {
			for _, port := range conf.proxyConfig.GetIgnoreInboundPorts() {
				portStr := strconv.FormatUint(uint64(port.GetPort()), 10)
				value += portStr + ","
			}
			return value[:len(value)-1]
		}
		value = strValue

	case k8s.ProxyIgnoreOutboundPortsAnnotation:
		if useDefault && len(conf.proxyConfig.GetIgnoreOutboundPorts()) > 0 {
			for _, port := range conf.proxyConfig.GetIgnoreOutboundPorts() {
				portStr := strconv.FormatUint(uint64(port.GetPort()), 10)
				value += portStr + ","
			}
			return value[:len(value)-1]
		}
		value = strValue

	case k8s.ProxyControlPortAnnotation:
		if useDefault {
			return strconv.FormatUint(uint64(conf.proxyConfig.GetControlPort().GetPort()), 10)
		}
		value = strValue

	case k8s.ProxyInboundPortAnnotation:
		if useDefault {
			return strconv.FormatUint(uint64(conf.proxyConfig.GetInboundPort().GetPort()), 10)
		}
		value = strValue

	case k8s.ProxyMetricsPortAnnotation:
		if useDefault {
			return strconv.FormatUint(uint64(conf.proxyConfig.GetMetricsPort().GetPort()), 10)
		}
		value = strValue

	case k8s.ProxyOutboundPortAnnotation:
		if useDefault {
			return strconv.FormatUint(uint64(conf.proxyConfig.GetOutboundPort().GetPort()), 10)
		}
		value = strValue

	case k8s.ProxyRequestCPUAnnotation:
		if useDefault {
			return conf.proxyConfig.GetResource().GetRequestCpu()
		}
		value = strValue

	case k8s.ProxyRequestMemoryAnnotation:
		if useDefault {
			return conf.proxyConfig.GetResource().GetRequestMemory()
		}
		value = strValue

	case k8s.ProxyLimitCPUAnnotation:
		if useDefault {
			return conf.proxyConfig.GetResource().GetLimitCpu()
		}
		value = strValue

	case k8s.ProxyLimitMemoryAnnotation:
		if useDefault {
			return conf.proxyConfig.GetResource().GetLimitMemory()
		}
		value = strValue

	case k8s.ProxyUIDAnnotation:
		if useDefault {
			return strconv.FormatInt(conf.proxyConfig.GetProxyUid(), 10)
		}
		value = strValue

	case k8s.ProxyLogLevelAnnotation:
		if useDefault {
			return conf.proxyConfig.GetLogLevel().GetLevel()
		}
		value = strValue

	case k8s.ProxyDisableExternalProfilesAnnotation:
		if useDefault {
			return strconv.FormatBool(conf.proxyConfig.GetDisableExternalProfiles())
		}
		value = strValue
	}

	return value
}

// ShouldInjectCLI is used by CLI inject to determine whether or not a given
// workload should be injected. It shouldn't if:
// - it contains any known sidecars; or
// - is on a HostNetwork; or
// - the pod is annotated with "linkerd.io/inject: disabled".
func ShouldInjectCLI(_ *ResourceConfig, r Report) bool {
	return r.Injectable()
}

// ShouldInjectWebhook determines whether or not the given workload should be
// injected. It shouldn't if:
// - it contains any known sidecars; or
// - is on a HostNetwork; or
// - the pod is annotated with "linkerd.io/inject: disabled".
// Additionally, a workload should be injected if:
// - the workload's namespace has the linkerd.io/inject annotation set to
//   "enabled", and the workload's pod spec does not have the
//   linkerd.io/inject annotation set to "disabled"; or
// - the workload's pod spec has the linkerd.io/inject annotation set to "enabled"
func ShouldInjectWebhook(conf *ResourceConfig, r Report) bool {
	if !r.Injectable() {
		return false
	}

	podAnnotation := conf.podMeta.Annotations[k8s.ProxyInjectAnnotation]
	nsAnnotation := conf.nsAnnotations[k8s.ProxyInjectAnnotation]
	if nsAnnotation == k8s.ProxyInjectEnabled && podAnnotation != k8s.ProxyInjectDisabled {
		return true
	}

	return podAnnotation == k8s.ProxyInjectEnabled
}
