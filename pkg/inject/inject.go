package inject

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	k8sResource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"

	"github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/k8s"
)

const (
	// localhostDNSOverride allows override of the destinationDNS. This
	// must be in absolute form for the proxy to special-case it.
	localhostDNSOverride = "localhost."

	controllerPodName = "linkerd-controller"

	// defaultKeepaliveMs is used in the proxy configuration for remote connections
	defaultKeepaliveMs = 10000

	defaultProfileSuffix  = "."
	internalProfileSuffix = "svc.cluster.local."

	envLog                = "LINKERD2_PROXY_LOG"
	envControlListenAddr  = "LINKERD2_PROXY_CONTROL_LISTEN_ADDR"
	envAdminListenAddr    = "LINKERD2_PROXY_ADMIN_LISTEN_ADDR"
	envOutboundListenAddr = "LINKERD2_PROXY_OUTBOUND_LISTEN_ADDR"
	envInboundListenAddr  = "LINKERD2_PROXY_INBOUND_LISTEN_ADDR"

	envInboundAcceptKeepAlive   = "LINKERD2_PROXY_INBOUND_ACCEPT_KEEPALIVE"
	envOutboundConnectKeepAlive = "LINKERD2_PROXY_OUTBOUND_CONNECT_KEEPALIVE"

	envDestinationContext         = "LINKERD2_PROXY_DESTINATION_CONTEXT"
	envDestinationProfileSuffixes = "LINKERD2_PROXY_DESTINATION_PROFILE_SUFFIXES"
	envDestinationSvcAddr         = "LINKERD2_PROXY_DESTINATION_SVC_ADDR"

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
	globalConfig           *config.Global
	proxyConfig            *config.Proxy
	nsAnnotations          map[string]string
	meta                   metav1.TypeMeta
	obj                    runtime.Object
	workLoadMeta           *metav1.ObjectMeta
	podMeta                objMeta
	podLabels              map[string]string
	podSpec                *v1.PodSpec
	destinationDNSOverride string
	proxyOutboundCapacity  map[string]uint
}

// NewResourceConfig creates and initializes a ResourceConfig
func NewResourceConfig(globalConfig *config.Global, proxyConfig *config.Proxy) *ResourceConfig {
	return &ResourceConfig{
		globalConfig:          globalConfig,
		proxyConfig:           proxyConfig,
		podLabels:             map[string]string{k8s.ControllerNSLabel: globalConfig.GetLinkerdNamespace()},
		proxyOutboundCapacity: map[string]uint{},
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
	log.Infof("received %s/%s", strings.ToLower(conf.meta.Kind), report.Name)

	if err := conf.parse(bytes); err != nil {
		return nil, nil, err
	}

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
		report.update(conf)
		if shouldInject(conf, report) {
			conf.injectPodSpec(patch)
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

		if v.Name == controllerPodName && v.Namespace == conf.globalConfig.GetLinkerdNamespace() {
			conf.destinationDNSOverride = localhostDNSOverride
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
func (conf *ResourceConfig) injectPodSpec(patch *Patch) {
	if !conf.globalConfig.GetCniEnabled() {
		conf.injectProxyInit(patch)
	}

	proxyUID := conf.proxyUID()
	sidecar := v1.Container{
		Name:                     k8s.ProxyContainerName,
		Image:                    conf.taggedProxyImage(),
		ImagePullPolicy:          conf.proxyImagePullPolicy(),
		TerminationMessagePolicy: v1.TerminationMessageFallbackToLogsOnError,
		SecurityContext:          &v1.SecurityContext{RunAsUser: &proxyUID},
		Ports: []v1.ContainerPort{
			{Name: k8s.ProxyPortName, ContainerPort: conf.proxyInboundPort()},
			{Name: k8s.ProxyAdminPortName, ContainerPort: conf.proxyAdminPort()},
		},
		Resources: conf.proxyResourceRequirements(),
		Env: []v1.EnvVar{
			{
				Name:  envLog,
				Value: conf.proxyLogLevel(),
			},
			{
				Name:  envDestinationSvcAddr,
				Value: conf.proxyDestinationAddr(),
			},
			{
				Name:  envControlListenAddr,
				Value: conf.proxyControlListenAddr(),
			},
			{
				Name:  envAdminListenAddr,
				Value: conf.proxyAdminListenAddr(),
			},
			{
				Name:  envOutboundListenAddr,
				Value: conf.proxyOutboundListenAddr(),
			},
			{
				Name:  envInboundListenAddr,
				Value: conf.proxyInboundListenAddr(),
			},
			{
				Name:  envDestinationProfileSuffixes,
				Value: conf.proxyDestinationProfileSuffixes(),
			},
			{
				Name:  envInboundAcceptKeepAlive,
				Value: fmt.Sprintf("%dms", defaultKeepaliveMs),
			},
			{
				Name:  envOutboundConnectKeepAlive,
				Value: fmt.Sprintf("%dms", defaultKeepaliveMs),
			},
			{
				Name:      "K8S_NS",
				ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
			},
			{
				Name:  envDestinationContext,
				Value: "ns:$(K8S_NS)",
			},
		},
		ReadinessProbe: conf.proxyReadinessProbe(),
		LivenessProbe:  conf.proxyLivenessProbe(),
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

	sidecar.Env = append(sidecar.Env, v1.EnvVar{
		Name:  "LINKERD2_PROXY_IDENTITY_DISABLED",
		Value: "Identity configuration is unavailable",
	})
	if idctx := conf.globalConfig.GetIdentityContext(); idctx != nil {
		log.Warn("Ignoring Identity configuration.")
	}

	patch.addContainer(&sidecar)
}

func (conf *ResourceConfig) injectProxyInit(patch *Patch) {
	nonRoot := false
	runAsUser := int64(0)
	initContainer := &v1.Container{
		Name:                     k8s.InitContainerName,
		Image:                    conf.taggedProxyInitImage(),
		ImagePullPolicy:          conf.proxyInitImagePullPolicy(),
		TerminationMessagePolicy: v1.TerminationMessageFallbackToLogsOnError,
		Args:                     conf.proxyInitArgs(),
		SecurityContext: &v1.SecurityContext{
			Capabilities: &v1.Capabilities{
				Add: []v1.Capability{v1.Capability("NET_ADMIN")},
			},
			Privileged:   &nonRoot,
			RunAsNonRoot: &nonRoot,
			RunAsUser:    &runAsUser,
		},
	}
	if len(conf.podSpec.InitContainers) == 0 {
		patch.addInitContainerRoot()
	}
	patch.addInitContainer(initContainer)
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

func (conf *ResourceConfig) getOverride(annotation string) string {
	return conf.podMeta.Annotations[annotation]
}

func (conf *ResourceConfig) taggedProxyImage() string {
	return fmt.Sprintf("%s:%s", conf.proxyImage(), conf.globalConfig.GetVersion())
}

func (conf *ResourceConfig) taggedProxyInitImage() string {
	return fmt.Sprintf("%s:%s", conf.proxyInitImage(), conf.globalConfig.GetVersion())
}

func (conf *ResourceConfig) proxyImage() string {
	if override := conf.getOverride(k8s.ProxyImageAnnotation); override != "" {
		return override
	}
	return conf.proxyConfig.GetProxyImage().GetImageName()
}

func (conf *ResourceConfig) proxyImagePullPolicy() v1.PullPolicy {
	if override := conf.getOverride(k8s.ProxyImagePullPolicyAnnotation); override != "" {
		return v1.PullPolicy(override)
	}
	return v1.PullPolicy(conf.proxyConfig.GetProxyImage().GetPullPolicy())
}

func (conf *ResourceConfig) proxyControlPort() int32 {
	if override := conf.getOverride(k8s.ProxyControlPortAnnotation); override != "" {
		controlPort, err := strconv.ParseInt(override, 10, 32)
		if err == nil {
			return int32(controlPort)
		}
	}

	return int32(conf.proxyConfig.GetControlPort().GetPort())
}

func (conf *ResourceConfig) proxyInboundPort() int32 {
	if override := conf.getOverride(k8s.ProxyInboundPortAnnotation); override != "" {
		inboundPort, err := strconv.ParseInt(override, 10, 32)
		if err == nil {
			return int32(inboundPort)
		}
	}

	return int32(conf.proxyConfig.GetInboundPort().GetPort())
}

func (conf *ResourceConfig) proxyAdminPort() int32 {
	if override := conf.getOverride(k8s.ProxyAdminPortAnnotation); override != "" {
		adminPort, err := strconv.ParseInt(override, 10, 32)
		if err == nil {
			return int32(adminPort)
		}
	}
	return int32(conf.proxyConfig.GetAdminPort().GetPort())
}

func (conf *ResourceConfig) proxyOutboundPort() int32 {
	if override := conf.getOverride(k8s.ProxyOutboundPortAnnotation); override != "" {
		outboundPort, err := strconv.ParseInt(override, 10, 32)
		if err == nil {
			return int32(outboundPort)
		}
	}

	return int32(conf.proxyConfig.GetOutboundPort().GetPort())
}

func (conf *ResourceConfig) proxyLogLevel() string {
	if override := conf.getOverride(k8s.ProxyLogLevelAnnotation); override != "" {
		return override
	}

	return conf.proxyConfig.GetLogLevel().GetLevel()
}

func (conf *ResourceConfig) proxyResourceRequirements() v1.ResourceRequirements {
	resources := v1.ResourceRequirements{
		Requests: v1.ResourceList{},
		Limits:   v1.ResourceList{},
	}

	var (
		requestCPU    k8sResource.Quantity
		requestMemory k8sResource.Quantity
		limitCPU      k8sResource.Quantity
		limitMemory   k8sResource.Quantity
		err           error
	)

	if override := conf.getOverride(k8s.ProxyCPURequestAnnotation); override != "" {
		requestCPU, err = k8sResource.ParseQuantity(override)
	} else if defaultRequest := conf.proxyConfig.GetResource().GetRequestCpu(); defaultRequest != "" {
		requestCPU, err = k8sResource.ParseQuantity(defaultRequest)
	}
	if err != nil {
		log.Warnf("%s (%s)", err, k8s.ProxyCPURequestAnnotation)
	}
	if !requestCPU.IsZero() {
		resources.Requests["cpu"] = requestCPU
	}

	if override := conf.getOverride(k8s.ProxyMemoryRequestAnnotation); override != "" {
		requestMemory, err = k8sResource.ParseQuantity(override)
	} else if defaultRequest := conf.proxyConfig.GetResource().GetRequestMemory(); defaultRequest != "" {
		requestMemory, err = k8sResource.ParseQuantity(defaultRequest)
	}
	if err != nil {
		log.Warnf("%s (%s)", err, k8s.ProxyMemoryRequestAnnotation)
	}
	if !requestMemory.IsZero() {
		resources.Requests["memory"] = requestMemory
	}

	if override := conf.getOverride(k8s.ProxyCPULimitAnnotation); override != "" {
		limitCPU, err = k8sResource.ParseQuantity(override)
	} else if defaultLimit := conf.proxyConfig.GetResource().GetLimitCpu(); defaultLimit != "" {
		limitCPU, err = k8sResource.ParseQuantity(defaultLimit)
	}
	if err != nil {
		log.Warnf("%s (%s)", err, k8s.ProxyCPULimitAnnotation)
	}
	if !limitCPU.IsZero() {
		resources.Limits["cpu"] = limitCPU
	}

	if override := conf.getOverride(k8s.ProxyMemoryLimitAnnotation); override != "" {
		limitMemory, err = k8sResource.ParseQuantity(override)
	} else if defaultLimit := conf.proxyConfig.GetResource().GetLimitMemory(); defaultLimit != "" {
		limitMemory, err = k8sResource.ParseQuantity(defaultLimit)
	}
	if err != nil {
		log.Warnf("%s (%s)", err, k8s.ProxyMemoryLimitAnnotation)
	}
	if !limitMemory.IsZero() {
		resources.Limits["memory"] = limitMemory
	}

	return resources
}

func (conf *ResourceConfig) proxyDestinationAddr() string {
	dns := fmt.Sprintf("linkerd-destination.%s.svc.cluster.local", conf.globalConfig.GetLinkerdNamespace())
	if conf.destinationDNSOverride != "" {
		dns = conf.destinationDNSOverride
	}
	return fmt.Sprintf("%s:%d", dns, destinationAPIPort)
}

func (conf *ResourceConfig) proxyControlListenAddr() string {
	return fmt.Sprintf("0.0.0.0:%d", conf.proxyControlPort())
}

func (conf *ResourceConfig) proxyInboundListenAddr() string {
	return fmt.Sprintf("0.0.0.0:%d", conf.proxyInboundPort())
}

func (conf *ResourceConfig) proxyAdminListenAddr() string {
	return fmt.Sprintf("0.0.0.0:%d", conf.proxyAdminPort())
}

func (conf *ResourceConfig) proxyOutboundListenAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", conf.proxyOutboundPort())
}

func (conf *ResourceConfig) proxyUID() int64 {
	if overrides := conf.getOverride(k8s.ProxyUIDAnnotation); overrides != "" {
		v, err := strconv.ParseInt(overrides, 10, 64)
		if err == nil {
			return v
		}
	}

	return conf.proxyConfig.GetProxyUid()
}

func (conf *ResourceConfig) proxyReadinessProbe() *v1.Probe {
	return &v1.Probe{
		Handler: v1.Handler{
			HTTPGet: &v1.HTTPGetAction{
				Path: "/ready",
				Port: intstr.IntOrString{IntVal: conf.proxyAdminPort()},
			},
		},
		InitialDelaySeconds: 2,
	}
}

func (conf *ResourceConfig) proxyLivenessProbe() *v1.Probe {
	return &v1.Probe{
		Handler: v1.Handler{
			HTTPGet: &v1.HTTPGetAction{
				Path: "/metrics",
				Port: intstr.IntOrString{IntVal: conf.proxyAdminPort()},
			},
		},
		InitialDelaySeconds: 10,
	}
}

func (conf *ResourceConfig) proxyDestinationProfileSuffixes() string {
	if overrides := conf.getOverride(k8s.ProxyDisableExternalProfilesAnnotation); overrides != "" {
		disableExternalProfiles, err := strconv.ParseBool(overrides)
		if err == nil && disableExternalProfiles {
			return internalProfileSuffix
		}
	}

	return defaultProfileSuffix
}

func (conf *ResourceConfig) proxyInitImage() string {
	if override := conf.getOverride(k8s.ProxyInitImageAnnotation); override != "" {
		return override
	}
	return conf.proxyConfig.GetProxyInitImage().GetImageName()
}

func (conf *ResourceConfig) proxyInitImagePullPolicy() v1.PullPolicy {
	if override := conf.getOverride(k8s.ProxyImagePullPolicyAnnotation); override != "" {
		return v1.PullPolicy(override)
	}
	return v1.PullPolicy(conf.proxyConfig.GetProxyInitImage().GetPullPolicy())
}

func (conf *ResourceConfig) proxyInitArgs() []string {
	var (
		controlPort       = conf.proxyControlPort()
		adminPort         = conf.proxyAdminPort()
		inboundPort       = conf.proxyInboundPort()
		outboundPort      = conf.proxyOutboundPort()
		outboundSkipPorts = conf.proxyOutboundSkipPorts()
		proxyUID          = conf.proxyUID()
	)

	inboundSkipPorts := conf.proxyInboundSkipPorts()
	if len(inboundSkipPorts) > 0 {
		inboundSkipPorts += ","
	}
	inboundSkipPorts += fmt.Sprintf("%d,%d", controlPort, adminPort)

	initArgs := []string{
		"--incoming-proxy-port", fmt.Sprintf("%d", inboundPort),
		"--outgoing-proxy-port", fmt.Sprintf("%d", outboundPort),
		"--proxy-uid", fmt.Sprintf("%d", proxyUID),
	}
	initArgs = append(initArgs, "--inbound-ports-to-ignore", inboundSkipPorts)
	if len(outboundSkipPorts) > 0 {
		initArgs = append(initArgs, "--outbound-ports-to-ignore")
		initArgs = append(initArgs, outboundSkipPorts)
	}

	return initArgs
}

func (conf *ResourceConfig) proxyInboundSkipPorts() string {
	if override := conf.getOverride(k8s.ProxyIgnoreInboundPortsAnnotation); override != "" {
		return override
	}

	ports := []string{}
	for _, port := range conf.proxyConfig.GetIgnoreInboundPorts() {
		portStr := strconv.FormatUint(uint64(port.GetPort()), 10)
		ports = append(ports, portStr)
	}
	return strings.Join(ports, ",")
}

func (conf *ResourceConfig) proxyOutboundSkipPorts() string {
	if override := conf.getOverride(k8s.ProxyIgnoreOutboundPortsAnnotation); override != "" {
		return override
	}

	ports := []string{}
	for _, port := range conf.proxyConfig.GetIgnoreOutboundPorts() {
		portStr := strconv.FormatUint(uint64(port.GetPort()), 10)
		ports = append(ports, portStr)
	}
	return strings.Join(ports, ",")
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
