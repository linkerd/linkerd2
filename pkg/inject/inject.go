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
	k8sResource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"
)

const (
	// localhostDNSOverride allows override of the destinationDNS. This
	// must be in absolute form for the proxy to special-case it.
	localhostDNSOverride = "localhost."

	controllerDeployName = "linkerd-controller"
	identityDeployName   = "linkerd-identity"

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
	envDestinationSvcName         = "LINKERD2_PROXY_DESTINATION_SVC_NAME"

	// destinationAPIPort is the port exposed by the linkerd-destination service
	destinationAPIPort = 8086

	envIdentityDisabled     = "LINKERD2_PROXY_IDENTITY_DISABLED"
	envIdentityDir          = "LINKERD2_PROXY_IDENTITY_DIR"
	envIdentityLocalName    = "LINKERD2_PROXY_IDENTITY_LOCAL_NAME"
	envIdentitySvcAddr      = "LINKERD2_PROXY_IDENTITY_SVC_ADDR"
	envIdentitySvcName      = "LINKERD2_PROXY_IDENTITY_SVC_NAME"
	envIdentityTokenFile    = "LINKERD2_PROXY_IDENTITY_TOKEN_FILE"
	envIdentityTrustAnchors = "LINKERD2_PROXY_IDENTITY_TRUST_ANCHORS"

	identityAPIPort     = 8080
	identityDisabledMsg = "Identity is not yet available"

	// OriginCLI is the value of the ResourceConfig's 'origin' field if the input
	// YAML comes from the CLI
	OriginCLI Origin = iota

	// OriginWebhook is the value of the ResourceConfig's 'origin' field if the input
	// YAML comes from the CLI
	OriginWebhook

	// OriginUnknown is the value of the ResourceConfig's 'origin' field if the
	// input YAML comes from an unknown source
	OriginUnknown
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

// Origin defines where the input YAML comes from. Refer the ResourceConfig's
// 'origin' field
type Origin int

// OwnerRetrieverFunc is a function that returns a pod's owner reference
// kind and name
type OwnerRetrieverFunc func(*v1.Pod) (string, string)

// ResourceConfig contains the parsed information for a given workload
type ResourceConfig struct {
	configs                *config.All
	nsAnnotations          map[string]string
	destinationDNSOverride string
	identityDNSOverride    string
	proxyOutboundCapacity  map[string]uint
	ownerRetriever         OwnerRetrieverFunc
	origin                 Origin

	workload struct {
		obj      runtime.Object
		metaType metav1.TypeMeta

		// Meta is the workload's metadata. It's exported so that metadata of
		// non-workload resources can be unmarshalled by the YAML parser
		Meta *metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	}

	pod struct {
		meta        *metav1.ObjectMeta
		labels      map[string]string
		annotations map[string]string
		spec        *v1.PodSpec
	}
}

// NewResourceConfig creates and initializes a ResourceConfig
func NewResourceConfig(configs *config.All, origin Origin) *ResourceConfig {
	config := &ResourceConfig{
		configs:               configs,
		proxyOutboundCapacity: map[string]uint{},
		origin:                origin,
	}

	config.pod.meta = &metav1.ObjectMeta{}
	config.pod.labels = map[string]string{k8s.ControllerNSLabel: configs.GetGlobal().GetLinkerdNamespace()}
	config.pod.annotations = map[string]string{}
	return config
}

// WithKind enriches ResourceConfig with the workload kind
func (conf *ResourceConfig) WithKind(kind string) *ResourceConfig {
	conf.workload.metaType = metav1.TypeMeta{Kind: kind}
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

// WithOwnerRetriever enriches ResourceConfig with a function that allows to retrieve
// the kind and name of the workload's owner reference
func (conf *ResourceConfig) WithOwnerRetriever(f OwnerRetrieverFunc) *ResourceConfig {
	conf.ownerRetriever = f
	return conf
}

// AppendPodAnnotations appends the given annotations to the pod spec in conf
func (conf *ResourceConfig) AppendPodAnnotations(annotations map[string]string) {
	for annotation, value := range annotations {
		conf.pod.annotations[annotation] = value
	}
}

// CreatedByCLI returns true if the pod is annotated with linkerd.io/created-by: linkerd/cli
func (conf *ResourceConfig) CreatedByCLI() bool {
	if conf.pod.annotations == nil {
		return false
	}
	return strings.Contains(conf.pod.annotations[k8s.CreatedByAnnotation], "")
}

// YamlMarshalObj returns the yaml for the workload in conf
func (conf *ResourceConfig) YamlMarshalObj() ([]byte, error) {
	return yaml.Marshal(conf.workload.obj)
}

// ParseMetaAndYAML extracts the workload metadata and pod specs from the given
// input bytes. The results are stored in the conf's fields.
func (conf *ResourceConfig) ParseMetaAndYAML(bytes []byte) (*Report, error) {
	if err := conf.parse(bytes); err != nil {
		return nil, err
	}

	return newReport(conf), nil
}

// GetPatch returns the JSON patch containing the proxy and init containers specs, if any
func (conf *ResourceConfig) GetPatch(bytes []byte) (*Patch, error) {
	patch := NewPatch(conf.workload.metaType.Kind)
	if conf.pod.spec != nil {
		conf.injectObjectMeta(patch)
		conf.injectPodSpec(patch)
	}

	return patch, nil
}

// KindInjectable returns true if the resource in conf can be injected with a proxy
func (conf *ResourceConfig) KindInjectable() bool {
	for _, kind := range injectableKinds {
		if strings.ToLower(conf.workload.metaType.Kind) == kind {
			return true
		}
	}
	return false
}

// Note this switch must be kept in sync with injectableKinds (declared above)
func (conf *ResourceConfig) getFreshWorkloadObj() runtime.Object {
	switch strings.ToLower(conf.workload.metaType.Kind) {
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

// parse parses the bytes payload, filling the gaps in ResourceConfig
// depending on the workload kind
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
	// LINKERD2_PROXY_DESTINATION_SVC_ADDR variable must be set to localhost for
	// the following reasons:
	//	1. According to https://github.com/kubernetes/minikube/issues/1568, minikube has an issue
	//     where pods are unable to connect to themselves through their associated service IP.
	//     Setting the LINKERD2_PROXY_DESTINATION_SVC_ADDR to localhost allows the
	//     proxy to bypass kube DNS name resolution as a workaround to this issue.
	//  2. We avoid the TLS overhead in encrypting and decrypting intra-pod traffic i.e. traffic
	//     between containers in the same pod.
	//  3. Using a Service IP instead of localhost would mean intra-pod traffic would be load-balanced
	//     across all controller pod replicas. This is undesirable as we would want all traffic between
	//	   containers to be self contained.
	//  4. We skip recording telemetry for intra-pod traffic within the control plane.

	if err := yaml.Unmarshal(bytes, &conf.workload.metaType); err != nil {
		return err
	}
	obj := conf.getFreshWorkloadObj()

	switch v := obj.(type) {
	case *v1beta1.Deployment:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		if v.Namespace == conf.configs.GetGlobal().GetLinkerdNamespace() {
			switch v.Name {
			case controllerDeployName:
				conf.destinationDNSOverride = localhostDNSOverride
			case identityDeployName:
				conf.identityDNSOverride = localhostDNSOverride
			}
		}

		conf.workload.obj = v
		conf.workload.Meta = &v.ObjectMeta
		conf.pod.labels[k8s.ProxyDeploymentLabel] = v.Name
		conf.complete(&v.Spec.Template)

	case *v1.ReplicationController:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		conf.workload.obj = v
		conf.workload.Meta = &v.ObjectMeta
		conf.pod.labels[k8s.ProxyReplicationControllerLabel] = v.Name
		conf.complete(v.Spec.Template)

	case *v1beta1.ReplicaSet:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		conf.workload.obj = v
		conf.workload.Meta = &v.ObjectMeta
		conf.pod.labels[k8s.ProxyReplicaSetLabel] = v.Name
		conf.complete(&v.Spec.Template)

	case *batchv1.Job:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		conf.workload.obj = v
		conf.workload.Meta = &v.ObjectMeta
		conf.pod.labels[k8s.ProxyJobLabel] = v.Name
		conf.complete(&v.Spec.Template)

	case *v1beta1.DaemonSet:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		conf.workload.obj = v
		conf.workload.Meta = &v.ObjectMeta
		conf.pod.labels[k8s.ProxyDaemonSetLabel] = v.Name
		conf.complete(&v.Spec.Template)

	case *appsv1.StatefulSet:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		conf.workload.obj = v
		conf.workload.Meta = &v.ObjectMeta
		conf.pod.labels[k8s.ProxyStatefulSetLabel] = v.Name
		conf.complete(&v.Spec.Template)

	case *v1.Pod:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		conf.workload.obj = v
		conf.pod.spec = &v.Spec
		conf.pod.meta = &v.ObjectMeta

		if conf.ownerRetriever != nil {
			kind, name := conf.ownerRetriever(v)
			switch kind {
			case k8s.Deployment:
				conf.pod.labels[k8s.ProxyDeploymentLabel] = name
			case k8s.ReplicationController:
				conf.pod.labels[k8s.ProxyReplicationControllerLabel] = name
			case k8s.ReplicaSet:
				conf.pod.labels[k8s.ProxyReplicaSetLabel] = name
			case k8s.Job:
				conf.pod.labels[k8s.ProxyJobLabel] = name
			case k8s.DaemonSet:
				conf.pod.labels[k8s.ProxyDaemonSetLabel] = name
			case k8s.StatefulSet:
				conf.pod.labels[k8s.ProxyStatefulSetLabel] = name
			}
		}

	default:
		// unmarshal the metadata of other resource kinds like namespace, secret,
		// config map etc. to be used in the report struct
		if err := yaml.Unmarshal(bytes, &conf.workload); err != nil {
			return err
		}
	}

	if conf.pod.meta.Annotations == nil {
		conf.pod.meta.Annotations = map[string]string{}
	}

	return nil
}

func (conf *ResourceConfig) complete(template *v1.PodTemplateSpec) {
	conf.pod.spec = &template.Spec
	conf.pod.meta = &template.ObjectMeta
}

// injectPodSpec adds linkerd sidecars to the provided PodSpec.
func (conf *ResourceConfig) injectPodSpec(patch *Patch) {
	saVolumeMount := conf.serviceAccountVolumeMount()
	if !conf.configs.GetGlobal().GetCniEnabled() {
		conf.injectProxyInit(patch, saVolumeMount)
	}

	proxyUID := conf.proxyUID()
	sidecar := v1.Container{
		Name:                     k8s.ProxyContainerName,
		Image:                    conf.taggedProxyImage(),
		ImagePullPolicy:          conf.proxyImagePullPolicy(),
		TerminationMessagePolicy: v1.TerminationMessageFallbackToLogsOnError,
		SecurityContext:          &v1.SecurityContext{RunAsUser: &proxyUID},
		Ports: []v1.ContainerPort{
			{
				Name:          k8s.ProxyPortName,
				ContainerPort: conf.proxyInboundPort(),
			},
			{
				Name:          k8s.ProxyAdminPortName,
				ContainerPort: conf.proxyAdminPort(),
			},
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
				Name:      "_pod_ns",
				ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
			},
			{
				Name:  envDestinationContext,
				Value: "ns:$(_pod_ns)",
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
	// Currently this will be set on any proxy that gets injected into a Prometheus pod,
	// not just the one in Linkerd's Control Plane.
	for _, container := range conf.pod.spec.Containers {
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

	if saVolumeMount != nil {
		sidecar.VolumeMounts = []v1.VolumeMount{*saVolumeMount}
	}

	idctx := conf.configs.GetGlobal().GetIdentityContext()
	if idctx == nil {
		sidecar.Env = append(sidecar.Env, v1.EnvVar{
			Name:  envIdentityDisabled,
			Value: identityDisabledMsg,
		})
		patch.addContainer(&sidecar)
		return
	}

	sidecar.Env = append(sidecar.Env, []v1.EnvVar{
		{
			Name:  envIdentityDir,
			Value: k8s.MountPathEndEntity,
		},
		{
			Name:  envIdentityTrustAnchors,
			Value: idctx.GetTrustAnchorsPem(),
		},
		{
			Name:  envIdentityTokenFile,
			Value: k8s.IdentityServiceAccountTokenPath,
		},
		{
			Name:  envIdentitySvcAddr,
			Value: conf.proxyIdentityAddr(),
		},
		{
			Name:      "_pod_sa",
			ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{FieldPath: "spec.serviceAccountName"}},
		},
		{
			Name:  "_l5d_ns",
			Value: conf.configs.GetGlobal().GetLinkerdNamespace(),
		},
		{
			Name:  "_l5d_trustdomain",
			Value: idctx.GetTrustDomain(),
		},
		{
			Name:  envIdentityLocalName,
			Value: "$(_pod_sa).$(_pod_ns).serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)",
		},
		{
			Name:  envIdentitySvcName,
			Value: "linkerd-identity.$(_l5d_ns).serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)",
		},
		{
			Name:  envDestinationSvcName,
			Value: "linkerd-controller.$(_l5d_ns).serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)",
		},
	}...)

	if len(conf.pod.spec.Volumes) == 0 {
		patch.addVolumeRoot()
	}
	patch.addVolume(&v1.Volume{
		Name: k8s.IdentityEndEntityVolumeName,
		VolumeSource: v1.VolumeSource{
			EmptyDir: &v1.EmptyDirVolumeSource{
				Medium: "Memory",
			},
		},
	})
	sidecar.VolumeMounts = append(sidecar.VolumeMounts, v1.VolumeMount{
		Name:      k8s.IdentityEndEntityVolumeName,
		MountPath: k8s.MountPathEndEntity,
		ReadOnly:  false,
	})
	patch.addContainer(&sidecar)
}

func (conf *ResourceConfig) injectProxyInit(patch *Patch, saVolumeMount *v1.VolumeMount) {
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
	if saVolumeMount != nil {
		initContainer.VolumeMounts = []v1.VolumeMount{*saVolumeMount}
	}
	if len(conf.pod.spec.InitContainers) == 0 {
		patch.addInitContainerRoot()
	}
	patch.addInitContainer(initContainer)
}

func (conf *ResourceConfig) serviceAccountVolumeMount() *v1.VolumeMount {
	// Probably always true, but wanna be super-safe
	if containers := conf.pod.spec.Containers; len(containers) > 0 {
		for _, vm := range containers[0].VolumeMounts {
			if vm.MountPath == k8s.MountPathServiceAccount {
				vm := vm // pin
				return &vm
			}
		}
	}
	return nil
}

// Given a ObjectMeta, update ObjectMeta in place with the new labels and
// annotations.
func (conf *ResourceConfig) injectObjectMeta(patch *Patch) {
	if len(conf.pod.meta.Annotations) == 0 {
		patch.addPodAnnotationsRoot()
	}
	patch.addPodAnnotation(k8s.ProxyVersionAnnotation, conf.configs.GetGlobal().GetVersion())

	if conf.configs.GetGlobal().GetIdentityContext() != nil {
		patch.addPodAnnotation(k8s.IdentityModeAnnotation, k8s.IdentityModeDefault)
	} else {
		patch.addPodAnnotation(k8s.IdentityModeAnnotation, k8s.IdentityModeDisabled)
	}

	if len(conf.pod.labels) > 0 {
		if len(conf.pod.meta.Labels) == 0 {
			patch.addPodLabelsRoot()
		}
		for k, v := range conf.pod.labels {
			patch.addPodLabel(k, v)
		}
	}

	for k, v := range conf.pod.annotations {
		patch.addPodAnnotation(k, v)

		// append any additional pod annotations to the pod's meta.
		// for e.g., annotations that were converted from CLI inject options.
		conf.pod.meta.Annotations[k] = v
	}
}

func (conf *ResourceConfig) getOverride(annotation string) string {
	return conf.pod.meta.Annotations[annotation]
}

func (conf *ResourceConfig) taggedProxyImage() string {
	return fmt.Sprintf("%s:%s", conf.proxyImage(), conf.configs.GetGlobal().GetVersion())
}

func (conf *ResourceConfig) taggedProxyInitImage() string {
	return fmt.Sprintf("%s:%s", conf.proxyInitImage(), conf.configs.GetGlobal().GetVersion())
}

func (conf *ResourceConfig) proxyImage() string {
	if override := conf.getOverride(k8s.ProxyImageAnnotation); override != "" {
		return override
	}
	return conf.configs.GetProxy().GetProxyImage().GetImageName()
}

func (conf *ResourceConfig) proxyImagePullPolicy() v1.PullPolicy {
	if override := conf.getOverride(k8s.ProxyImagePullPolicyAnnotation); override != "" {
		return v1.PullPolicy(override)
	}
	return v1.PullPolicy(conf.configs.GetProxy().GetProxyImage().GetPullPolicy())
}

func (conf *ResourceConfig) proxyControlPort() int32 {
	if override := conf.getOverride(k8s.ProxyControlPortAnnotation); override != "" {
		controlPort, err := strconv.ParseInt(override, 10, 32)
		if err == nil {
			return int32(controlPort)
		}
	}

	return int32(conf.configs.GetProxy().GetControlPort().GetPort())
}

func (conf *ResourceConfig) proxyInboundPort() int32 {
	if override := conf.getOverride(k8s.ProxyInboundPortAnnotation); override != "" {
		inboundPort, err := strconv.ParseInt(override, 10, 32)
		if err == nil {
			return int32(inboundPort)
		}
	}

	return int32(conf.configs.GetProxy().GetInboundPort().GetPort())
}

func (conf *ResourceConfig) proxyAdminPort() int32 {
	if override := conf.getOverride(k8s.ProxyAdminPortAnnotation); override != "" {
		adminPort, err := strconv.ParseInt(override, 10, 32)
		if err == nil {
			return int32(adminPort)
		}
	}
	return int32(conf.configs.GetProxy().GetAdminPort().GetPort())
}

func (conf *ResourceConfig) proxyOutboundPort() int32 {
	if override := conf.getOverride(k8s.ProxyOutboundPortAnnotation); override != "" {
		outboundPort, err := strconv.ParseInt(override, 10, 32)
		if err == nil {
			return int32(outboundPort)
		}
	}

	return int32(conf.configs.GetProxy().GetOutboundPort().GetPort())
}

func (conf *ResourceConfig) proxyLogLevel() string {
	if override := conf.getOverride(k8s.ProxyLogLevelAnnotation); override != "" {
		return override
	}

	return conf.configs.GetProxy().GetLogLevel().GetLevel()
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
	} else if defaultRequest := conf.configs.GetProxy().GetResource().GetRequestCpu(); defaultRequest != "" {
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
	} else if defaultRequest := conf.configs.GetProxy().GetResource().GetRequestMemory(); defaultRequest != "" {
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
	} else if defaultLimit := conf.configs.GetProxy().GetResource().GetLimitCpu(); defaultLimit != "" {
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
	} else if defaultLimit := conf.configs.GetProxy().GetResource().GetLimitMemory(); defaultLimit != "" {
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
	ns := conf.configs.GetGlobal().GetLinkerdNamespace()
	dns := fmt.Sprintf("linkerd-destination.%s.svc.cluster.local", ns)
	if conf.destinationDNSOverride != "" {
		dns = conf.destinationDNSOverride
	}
	return fmt.Sprintf("%s:%d", dns, destinationAPIPort)
}

func (conf *ResourceConfig) proxyIdentityAddr() string {
	dns := fmt.Sprintf("linkerd-identity.%s.svc.cluster.local", conf.configs.GetGlobal().GetLinkerdNamespace())
	if conf.identityDNSOverride != "" {
		dns = conf.identityDNSOverride
	}
	return fmt.Sprintf("%s:%d", dns, identityAPIPort)
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

	return conf.configs.GetProxy().GetProxyUid()
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
	disableExternalProfiles := conf.configs.GetProxy().GetDisableExternalProfiles()
	if override := conf.getOverride(k8s.ProxyDisableExternalProfilesAnnotation); override != "" {
		value, err := strconv.ParseBool(override)
		if err == nil {
			disableExternalProfiles = value
		}
	}

	if disableExternalProfiles {
		return internalProfileSuffix
	}

	return defaultProfileSuffix
}

func (conf *ResourceConfig) proxyInitImage() string {
	if override := conf.getOverride(k8s.ProxyInitImageAnnotation); override != "" {
		return override
	}
	return conf.configs.GetProxy().GetProxyInitImage().GetImageName()
}

func (conf *ResourceConfig) proxyInitImagePullPolicy() v1.PullPolicy {
	if override := conf.getOverride(k8s.ProxyImagePullPolicyAnnotation); override != "" {
		return v1.PullPolicy(override)
	}
	return v1.PullPolicy(conf.configs.GetProxy().GetProxyInitImage().GetPullPolicy())
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
	for _, port := range conf.configs.GetProxy().GetIgnoreInboundPorts() {
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
	for _, port := range conf.configs.GetProxy().GetIgnoreOutboundPorts() {
		portStr := strconv.FormatUint(uint64(port.GetPort()), 10)
		ports = append(ports, portStr)
	}
	return strings.Join(ports, ",")
}
