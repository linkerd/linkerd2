package inject

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	jsonfilter "github.com/clarketm/json"
	"github.com/linkerd/linkerd2/pkg/charts"
	"github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/charts/static"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/util"
	log "github.com/sirupsen/logrus"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8sResource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

var (
	rTrail = regexp.MustCompile(`\},\s*\]`)

	// ProxyAnnotations is the list of possible annotations that can be applied on a pod or namespace
	ProxyAnnotations = []string{
		k8s.ProxyAdminPortAnnotation,
		k8s.ProxyControlPortAnnotation,
		k8s.ProxyDisableIdentityAnnotation,
		k8s.ProxyDisableTapAnnotation,
		k8s.ProxyEnableDebugAnnotation,
		k8s.ProxyEnableExternalProfilesAnnotation,
		k8s.ProxyImagePullPolicyAnnotation,
		k8s.ProxyInboundPortAnnotation,
		k8s.ProxyInitImageAnnotation,
		k8s.ProxyInitImageVersionAnnotation,
		k8s.ProxyOutboundPortAnnotation,
		k8s.ProxyCPULimitAnnotation,
		k8s.ProxyCPURequestAnnotation,
		k8s.ProxyImageAnnotation,
		k8s.ProxyLogLevelAnnotation,
		k8s.ProxyMemoryLimitAnnotation,
		k8s.ProxyMemoryRequestAnnotation,
		k8s.ProxyUIDAnnotation,
		k8s.ProxyVersionOverrideAnnotation,
		k8s.ProxyRequireIdentityOnInboundPortsAnnotation,
		k8s.ProxyIgnoreInboundPortsAnnotation,
		k8s.ProxyOpaquePortsAnnotation,
		k8s.ProxyIgnoreOutboundPortsAnnotation,
		k8s.ProxyOutboundConnectTimeout,
		k8s.ProxyInboundConnectTimeout,
	}
)

// Origin defines where the input YAML comes from. Refer the ResourceConfig's
// 'origin' field
type Origin int

const (
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

// OwnerRetrieverFunc is a function that returns a pod's owner reference
// kind and name
type OwnerRetrieverFunc func(*corev1.Pod) (string, string)

// ResourceConfig contains the parsed information for a given workload
type ResourceConfig struct {
	// These values used for the rendering of the patch may be further overridden
	// by the annotations on the resource or the resource's namespace.
	values *l5dcharts.Values
	// These annotations from the resources's namespace are used as a base.
	// The resources's annotations will be applied on top of these, which allows
	// the nsAnnotations to act as a default.
	nsAnnotations  map[string]string
	ownerRetriever OwnerRetrieverFunc
	origin         Origin

	workload struct {
		obj      runtime.Object
		metaType metav1.TypeMeta

		// Meta is the workload's metadata. It's exported so that metadata of
		// non-workload resources can be unmarshalled by the YAML parser
		Meta *metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

		ownerRef *metav1.OwnerReference
	}

	pod struct {
		meta *metav1.ObjectMeta
		// This fields hold labels and annotations which are to be added to the
		// injected resource. This is different from meta.Labels and
		// meta.Annotationswhich are the labels and annotations on the original
		// resource before injection.
		labels      map[string]string
		annotations map[string]string
		spec        *corev1.PodSpec
	}
}

type patch struct {
	l5dcharts.Values
	PathPrefix            string                    `json:"pathPrefix"`
	AddRootMetadata       bool                      `json:"addRootMetadata"`
	AddRootAnnotations    bool                      `json:"addRootAnnotations"`
	Annotations           map[string]string         `json:"annotations"`
	AddRootLabels         bool                      `json:"addRootLabels"`
	AddRootInitContainers bool                      `json:"addRootInitContainers"`
	AddRootVolumes        bool                      `json:"addRootVolumes"`
	Labels                map[string]string         `json:"labels"`
	DebugContainer        *l5dcharts.DebugContainer `json:"debugContainer"`
}

// NewResourceConfig creates and initializes a ResourceConfig
func NewResourceConfig(values *l5dcharts.Values, origin Origin) *ResourceConfig {
	config := &ResourceConfig{
		nsAnnotations: make(map[string]string),
		values:        values,
		origin:        origin,
	}

	config.pod.meta = &metav1.ObjectMeta{}

	// Values can be nil for commands like Uninject
	var ns string
	if values != nil {
		ns = values.GetGlobal().Namespace
	}
	config.pod.labels = map[string]string{k8s.ControllerNSLabel: ns}
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

// WithOwnerRetriever enriches ResourceConfig with a function that allows to retrieve
// the kind and name of the workload's owner reference
func (conf *ResourceConfig) WithOwnerRetriever(f OwnerRetrieverFunc) *ResourceConfig {
	conf.ownerRetriever = f
	return conf
}

// GetOwnerRef returns a reference to the resource's owner resource, if any
func (conf *ResourceConfig) GetOwnerRef() *metav1.OwnerReference {
	return conf.workload.ownerRef
}

// AppendPodAnnotations appends the given annotations to the pod spec in conf
func (conf *ResourceConfig) AppendPodAnnotations(annotations map[string]string) {
	for annotation, value := range annotations {
		conf.pod.annotations[annotation] = value
	}
}

// AppendPodAnnotation appends the given single annotation to the pod spec in conf
func (conf *ResourceConfig) AppendPodAnnotation(k, v string) {
	conf.pod.annotations[k] = v
}

// YamlMarshalObj returns the yaml for the workload in conf
func (conf *ResourceConfig) YamlMarshalObj() ([]byte, error) {
	j, err := getFilteredJSON(conf.workload.obj)
	if err != nil {
		return nil, err
	}
	return yaml.JSONToYAML(j)
}

// ParseMetaAndYAML extracts the workload metadata and pod specs from the given
// input bytes. The results are stored in the conf's fields.
func (conf *ResourceConfig) ParseMetaAndYAML(bytes []byte) (*Report, error) {
	if err := conf.parse(bytes); err != nil {
		return nil, err
	}

	return newReport(conf), nil
}

// GetOverriddenValues returns the final Values struct which is created
// by overiding annoatated configuration on top of default Values
func (conf *ResourceConfig) GetOverriddenValues() (*linkerd2.Values, error) {

	// Make a copy of Values and mutate that
	copyValues, err := conf.values.DeepCopy()
	if err != nil {
		return nil, err
	}

	conf.applyAnnotationOverrides(copyValues)
	return copyValues, nil
}

// GetPatch returns the JSON patch containing the proxy and init containers specs, if any.
// If injectProxy is false, only the config.linkerd.io annotations are set.
func (conf *ResourceConfig) GetPatch(injectProxy bool) ([]byte, error) {

	values, err := conf.GetOverriddenValues()
	if err != nil {
		return nil, fmt.Errorf("could not generate Overridden Values: %s", err)
	}

	if values.GetGlobal().Proxy.RequireIdentityOnInboundPorts != "" && values.GetGlobal().Proxy.DisableIdentity {
		return nil, fmt.Errorf("%s cannot be set when identity is disabled", k8s.ProxyRequireIdentityOnInboundPortsAnnotation)
	}

	if values.GetGlobal().ClusterNetworks != "" {
		for _, network := range strings.Split(strings.Trim(values.GetGlobal().ClusterNetworks, ","), ",") {
			if _, _, err := net.ParseCIDR(network); err != nil {
				return nil, fmt.Errorf("cannot parse destination get networks: %s", err)
			}
		}
	}

	patch := &patch{
		Values:      *values,
		Annotations: map[string]string{},
		Labels:      map[string]string{},
	}
	switch strings.ToLower(conf.workload.metaType.Kind) {
	case k8s.Pod:
	case k8s.CronJob:
		patch.PathPrefix = "/spec/jobTemplate/spec/template"
	default:
		patch.PathPrefix = "/spec/template"
	}

	if conf.pod.spec != nil {
		conf.injectPodAnnotations(patch)
		if injectProxy {
			conf.injectObjectMeta(patch)
			conf.injectPodSpec(patch)
		} else {
			patch.GetGlobal().Proxy = nil
			patch.GetGlobal().ProxyInit = nil
		}
	}

	rawValues, err := yaml.Marshal(patch)
	if err != nil {
		return nil, err
	}

	files := []*loader.BufferedFile{
		{Name: chartutil.ChartfileName},
		{Name: "requirements.yaml"},
		{Name: "templates/patch.json"},
	}

	chart := &charts.Chart{
		Name:      "patch",
		Dir:       "patch",
		Namespace: conf.values.GetGlobal().Namespace,
		RawValues: rawValues,
		Files:     files,
		Fs:        static.Templates,
	}
	buf, err := chart.Render()
	if err != nil {
		return nil, err
	}

	// Get rid of invalid trailing commas
	res := rTrail.ReplaceAll(buf.Bytes(), []byte("}\n]"))

	return res, nil
}

// Note this switch also defines what kinds are injectable
func (conf *ResourceConfig) getFreshWorkloadObj() runtime.Object {
	switch strings.ToLower(conf.workload.metaType.Kind) {
	case k8s.Deployment:
		return &appsv1.Deployment{}
	case k8s.ReplicationController:
		return &corev1.ReplicationController{}
	case k8s.ReplicaSet:
		return &appsv1.ReplicaSet{}
	case k8s.Job:
		return &batchv1.Job{}
	case k8s.DaemonSet:
		return &appsv1.DaemonSet{}
	case k8s.StatefulSet:
		return &appsv1.StatefulSet{}
	case k8s.Pod:
		return &corev1.Pod{}
	case k8s.Namespace:
		return &corev1.Namespace{}
	case k8s.CronJob:
		return &batchv1beta1.CronJob{}
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

	j, err := getFilteredJSON(obj)
	if err != nil {
		return nil, err
	}
	return yaml.JSONToYAML(j)
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
	case *appsv1.Deployment:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		conf.workload.obj = v
		conf.workload.Meta = &v.ObjectMeta
		conf.pod.labels[k8s.ProxyDeploymentLabel] = v.Name
		conf.pod.labels[k8s.WorkloadNamespaceLabel] = v.Namespace
		conf.complete(&v.Spec.Template)

	case *corev1.ReplicationController:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		conf.workload.obj = v
		conf.workload.Meta = &v.ObjectMeta
		conf.pod.labels[k8s.ProxyReplicationControllerLabel] = v.Name
		conf.pod.labels[k8s.WorkloadNamespaceLabel] = v.Namespace
		conf.complete(v.Spec.Template)

	case *appsv1.ReplicaSet:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		conf.workload.obj = v
		conf.workload.Meta = &v.ObjectMeta
		conf.pod.labels[k8s.ProxyReplicaSetLabel] = v.Name
		conf.pod.labels[k8s.WorkloadNamespaceLabel] = v.Namespace
		conf.complete(&v.Spec.Template)

	case *batchv1.Job:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		conf.workload.obj = v
		conf.workload.Meta = &v.ObjectMeta
		conf.pod.labels[k8s.ProxyJobLabel] = v.Name
		conf.pod.labels[k8s.WorkloadNamespaceLabel] = v.Namespace
		conf.complete(&v.Spec.Template)

	case *appsv1.DaemonSet:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		conf.workload.obj = v
		conf.workload.Meta = &v.ObjectMeta
		conf.pod.labels[k8s.ProxyDaemonSetLabel] = v.Name
		conf.pod.labels[k8s.WorkloadNamespaceLabel] = v.Namespace
		conf.complete(&v.Spec.Template)

	case *appsv1.StatefulSet:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		conf.workload.obj = v
		conf.workload.Meta = &v.ObjectMeta
		conf.pod.labels[k8s.ProxyStatefulSetLabel] = v.Name
		conf.pod.labels[k8s.WorkloadNamespaceLabel] = v.Namespace
		conf.complete(&v.Spec.Template)

	case *corev1.Namespace:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}
		conf.workload.obj = v
		conf.workload.Meta = &v.ObjectMeta
		// If annotations not present previously
		if conf.workload.Meta.Annotations == nil {
			conf.workload.Meta.Annotations = map[string]string{}
		}

	case *batchv1beta1.CronJob:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		conf.workload.obj = v
		conf.workload.Meta = &v.ObjectMeta
		conf.pod.labels[k8s.ProxyCronJobLabel] = v.Name
		conf.pod.labels[k8s.WorkloadNamespaceLabel] = v.Namespace
		conf.complete(&v.Spec.JobTemplate.Spec.Template)

	case *corev1.Pod:
		if err := yaml.Unmarshal(bytes, v); err != nil {
			return err
		}

		conf.workload.obj = v
		conf.pod.spec = &v.Spec
		conf.pod.meta = &v.ObjectMeta

		if conf.ownerRetriever != nil {
			kind, name := conf.ownerRetriever(v)
			conf.workload.ownerRef = &metav1.OwnerReference{Kind: kind, Name: name}
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
		conf.pod.labels[k8s.WorkloadNamespaceLabel] = v.Namespace
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

func (conf *ResourceConfig) complete(template *corev1.PodTemplateSpec) {
	conf.pod.spec = &template.Spec
	conf.pod.meta = &template.ObjectMeta
}

// injectPodSpec adds linkerd sidecars to the provided PodSpec.
func (conf *ResourceConfig) injectPodSpec(values *patch) {
	saVolumeMount := conf.serviceAccountVolumeMount()

	// use the primary container's capabilities to ensure psp compliance, if
	// enabled
	if conf.pod.spec.Containers != nil && len(conf.pod.spec.Containers) > 0 {
		if sc := conf.pod.spec.Containers[0].SecurityContext; sc != nil && sc.Capabilities != nil {
			values.GetGlobal().Proxy.Capabilities = &l5dcharts.Capabilities{
				Add:  []string{},
				Drop: []string{},
			}
			for _, add := range sc.Capabilities.Add {
				values.GetGlobal().Proxy.Capabilities.Add = append(values.GetGlobal().Proxy.Capabilities.Add, string(add))
			}
			for _, drop := range sc.Capabilities.Drop {
				values.GetGlobal().Proxy.Capabilities.Drop = append(values.GetGlobal().Proxy.Capabilities.Drop, string(drop))
			}
		}
	}

	if saVolumeMount != nil {
		values.GetGlobal().Proxy.SAMountPath = &l5dcharts.VolumeMountPath{
			Name:      saVolumeMount.Name,
			MountPath: saVolumeMount.MountPath,
			ReadOnly:  saVolumeMount.ReadOnly,
		}
	}

	if v := conf.pod.meta.Annotations[k8s.ProxyEnableDebugAnnotation]; v != "" {
		debug, err := strconv.ParseBool(v)
		if err != nil {
			log.Warnf("unrecognized value used for the %s annotation: %s", k8s.ProxyEnableDebugAnnotation, v)
			debug = false
		}

		if debug {
			log.Infof("inject debug container")
			values.DebugContainer = &l5dcharts.DebugContainer{
				Image: &l5dcharts.Image{
					Name:       conf.values.DebugContainer.Image.Name,
					Version:    conf.values.DebugContainer.Image.Version,
					PullPolicy: conf.values.DebugContainer.Image.PullPolicy,
				},
			}
		}
	}

	conf.injectProxyInit(values)
	values.AddRootVolumes = len(conf.pod.spec.Volumes) == 0
}

func (conf *ResourceConfig) injectProxyInit(values *patch) {

	// Fill common fields from Proxy into ProxyInit
	values.GetGlobal().ProxyInit.Capabilities = values.GetGlobal().Proxy.Capabilities
	values.GetGlobal().ProxyInit.SAMountPath = values.GetGlobal().Proxy.SAMountPath

	if v := conf.pod.meta.Annotations[k8s.CloseWaitTimeoutAnnotation]; v != "" {
		closeWait, err := time.ParseDuration(v)
		if err != nil {
			log.Warnf("invalid duration value used for the %s annotation: %s", k8s.CloseWaitTimeoutAnnotation, v)
		} else {
			values.GetGlobal().ProxyInit.CloseWaitTimeoutSecs = int64(closeWait.Seconds())
		}
	}

	values.AddRootInitContainers = len(conf.pod.spec.InitContainers) == 0

}

func (conf *ResourceConfig) serviceAccountVolumeMount() *corev1.VolumeMount {
	// Probably always true, but want to be super-safe
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
func (conf *ResourceConfig) injectObjectMeta(values *patch) {

	values.Annotations[k8s.ProxyVersionAnnotation] = values.GetGlobal().Proxy.Image.Version

	if values.Identity == nil || values.GetGlobal().Proxy.DisableIdentity {
		values.Annotations[k8s.IdentityModeAnnotation] = k8s.IdentityModeDisabled
	} else {
		values.Annotations[k8s.IdentityModeAnnotation] = k8s.IdentityModeDefault
	}

	if len(conf.pod.labels) > 0 {
		values.AddRootLabels = len(conf.pod.meta.Labels) == 0
		for _, k := range sortedKeys(conf.pod.labels) {
			values.Labels[k] = conf.pod.labels[k]
		}
	}
}

func (conf *ResourceConfig) injectPodAnnotations(values *patch) {
	// ObjectMetaAnnotations.Annotations is nil for new empty structs, but we always initialize
	// it to an empty map in parse() above, so we follow suit here.
	emptyMeta := &metav1.ObjectMeta{Annotations: map[string]string{}}
	// Cronjobs in batch/v1beta1 might have an empty `spec.jobTemplate.spec.template.metadata`
	// field so we make sure to create it if needed, before attempting adding annotations
	values.AddRootMetadata = reflect.DeepEqual(conf.pod.meta, emptyMeta)
	values.AddRootAnnotations = len(conf.pod.meta.Annotations) == 0

	for _, k := range sortedKeys(conf.pod.annotations) {
		values.Annotations[k] = conf.pod.annotations[k]

		// append any additional pod annotations to the pod's meta.
		// for e.g., annotations that were converted from CLI inject options.
		conf.pod.meta.Annotations[k] = conf.pod.annotations[k]
	}
}

func (conf *ResourceConfig) applyAnnotationOverrides(values *l5dcharts.Values) {
	annotations := make(map[string]string)
	for k, v := range conf.nsAnnotations {
		annotations[k] = v
	}
	for k, v := range conf.pod.meta.Annotations {
		annotations[k] = v
	}

	if override, ok := annotations[k8s.ProxyInjectAnnotation]; ok {
		if override == k8s.ProxyInjectIngress {
			values.GetGlobal().Proxy.IsIngress = true
		}
	}

	if override, ok := annotations[k8s.ProxyImageAnnotation]; ok {
		values.GetGlobal().Proxy.Image.Name = override
	}

	if override, ok := annotations[k8s.ProxyVersionOverrideAnnotation]; ok {
		values.GetGlobal().Proxy.Image.Version = override
	}

	if override, ok := annotations[k8s.ProxyImagePullPolicyAnnotation]; ok {
		values.GetGlobal().Proxy.Image.PullPolicy = override
	}

	if override, ok := annotations[k8s.ProxyInitImageVersionAnnotation]; ok {
		values.GetGlobal().ProxyInit.Image.Version = override
	}

	if override, ok := annotations[k8s.ProxyControlPortAnnotation]; ok {
		controlPort, err := strconv.ParseInt(override, 10, 32)
		if err == nil {
			values.GetGlobal().Proxy.Ports.Control = int32(controlPort)
		}
	}

	if override, ok := annotations[k8s.ProxyInboundPortAnnotation]; ok {
		inboundPort, err := strconv.ParseInt(override, 10, 32)
		if err == nil {
			values.GetGlobal().Proxy.Ports.Inbound = int32(inboundPort)
		}
	}

	if override, ok := annotations[k8s.ProxyAdminPortAnnotation]; ok {
		adminPort, err := strconv.ParseInt(override, 10, 32)
		if err == nil {
			values.GetGlobal().Proxy.Ports.Admin = int32(adminPort)
		}
	}

	if override, ok := annotations[k8s.ProxyOutboundPortAnnotation]; ok {
		outboundPort, err := strconv.ParseInt(override, 10, 32)
		if err == nil {
			values.GetGlobal().Proxy.Ports.Outbound = int32(outboundPort)
		}
	}

	if override, ok := annotations[k8s.ProxyLogLevelAnnotation]; ok {
		values.GetGlobal().Proxy.LogLevel = override
	}

	if override, ok := annotations[k8s.ProxyLogFormatAnnotation]; ok {
		values.GetGlobal().Proxy.LogFormat = override
	}

	if override, ok := annotations[k8s.ProxyDisableIdentityAnnotation]; ok {
		value, err := strconv.ParseBool(override)
		if err == nil {
			values.GetGlobal().Proxy.DisableIdentity = value
		}
	}

	if override, ok := annotations[k8s.ProxyDisableTapAnnotation]; ok {
		value, err := strconv.ParseBool(override)
		if err == nil {
			values.GetGlobal().Proxy.DisableTap = value
		}
	}

	if override, ok := annotations[k8s.ProxyRequireIdentityOnInboundPortsAnnotation]; ok {
		values.GetGlobal().Proxy.RequireIdentityOnInboundPorts = override
	}

	if override, ok := annotations[k8s.ProxyOutboundConnectTimeout]; ok {
		duration, err := time.ParseDuration(override)
		if err != nil {
			log.Warnf("unrecognized proxy-outbound-connect-timeout duration value found on pod annotation: %s", err.Error())
		} else {
			values.GetGlobal().Proxy.OutboundConnectTimeout = fmt.Sprintf("%dms", int(duration.Seconds()*1000))
		}
	}

	if override, ok := annotations[k8s.ProxyInboundConnectTimeout]; ok {
		duration, err := time.ParseDuration(override)
		if err != nil {
			log.Warnf("unrecognized proxy-inbound-connect-timeout duration value found on pod annotation: %s", err.Error())
		} else {
			values.GetGlobal().Proxy.InboundConnectTimeout = fmt.Sprintf("%dms", int(duration.Seconds()*1000))
		}
	}

	if override, ok := annotations[k8s.ProxyEnableGatewayAnnotation]; ok {
		value, err := strconv.ParseBool(override)
		if err == nil {
			values.GetGlobal().Proxy.IsGateway = value
		}
	}

	if override, ok := annotations[k8s.ProxyWaitBeforeExitSecondsAnnotation]; ok {
		waitBeforeExitSeconds, err := strconv.ParseUint(override, 10, 64)
		if nil != err {
			log.Warnf("unrecognized value used for the %s annotation, uint64 is expected: %s",
				k8s.ProxyWaitBeforeExitSecondsAnnotation, override)
		} else {
			values.GetGlobal().Proxy.WaitBeforeExitSeconds = waitBeforeExitSeconds
		}
	}

	if override, ok := annotations[k8s.ProxyCPURequestAnnotation]; ok {
		_, err := k8sResource.ParseQuantity(override)
		if err != nil {
			log.Warnf("%s (%s)", err, k8s.ProxyCPURequestAnnotation)
		} else {
			values.GetGlobal().Proxy.Resources.CPU.Request = override
		}
	}

	if override, ok := annotations[k8s.ProxyMemoryRequestAnnotation]; ok {
		_, err := k8sResource.ParseQuantity(override)
		if err != nil {
			log.Warnf("%s (%s)", err, k8s.ProxyMemoryRequestAnnotation)
		} else {
			values.GetGlobal().Proxy.Resources.Memory.Request = override
		}
	}

	if override, ok := annotations[k8s.ProxyCPULimitAnnotation]; ok {
		q, err := k8sResource.ParseQuantity(override)
		if err != nil {
			log.Warnf("%s (%s)", err, k8s.ProxyCPULimitAnnotation)
		} else {
			values.GetGlobal().Proxy.Resources.CPU.Limit = override

			n, err := ToWholeCPUCores(q)
			if err != nil {
				log.Warnf("%s (%s)", err, k8s.ProxyCPULimitAnnotation)
			}
			values.GetGlobal().Proxy.Cores = n
		}
	}

	if override, ok := annotations[k8s.ProxyMemoryLimitAnnotation]; ok {
		_, err := k8sResource.ParseQuantity(override)
		if err != nil {
			log.Warnf("%s (%s)", err, k8s.ProxyMemoryLimitAnnotation)
		} else {
			values.GetGlobal().Proxy.Resources.Memory.Limit = override
		}
	}

	if override, ok := annotations[k8s.ProxyUIDAnnotation]; ok {
		v, err := strconv.ParseInt(override, 10, 64)
		if err == nil {
			values.GetGlobal().Proxy.UID = v
		}
	}

	if override, ok := annotations[k8s.ProxyEnableExternalProfilesAnnotation]; ok {
		value, err := strconv.ParseBool(override)
		if err == nil {
			values.GetGlobal().Proxy.EnableExternalProfiles = value
		}
	}

	if override, ok := annotations[k8s.ProxyInitImageAnnotation]; ok {
		values.GetGlobal().ProxyInit.Image.Name = override
	}

	if override, ok := annotations[k8s.ProxyImagePullPolicyAnnotation]; ok {
		values.GetGlobal().ProxyInit.Image.PullPolicy = override
	}

	if override, ok := annotations[k8s.ProxyIgnoreInboundPortsAnnotation]; ok {
		values.GetGlobal().ProxyInit.IgnoreInboundPorts = override
	}

	if override, ok := annotations[k8s.ProxyIgnoreOutboundPortsAnnotation]; ok {
		values.GetGlobal().ProxyInit.IgnoreOutboundPorts = override
	}

	if override, ok := annotations[k8s.ProxyOpaquePortsAnnotation]; ok {
		opaquePortsStrs := util.ParseContainerOpaquePorts(override, conf.pod.spec.Containers)
		values.GetGlobal().Proxy.OpaquePorts = strings.Join(opaquePortsStrs, ",")
	}

	if override, ok := annotations[k8s.DebugImageAnnotation]; ok {
		values.DebugContainer.Image.Name = override
	}

	if override, ok := annotations[k8s.DebugImageVersionAnnotation]; ok {
		values.DebugContainer.Image.Version = override
	}

	if override, ok := annotations[k8s.DebugImagePullPolicyAnnotation]; ok {
		values.DebugContainer.Image.PullPolicy = override
	}
}

// GetOverriddenConfiguration returns a map of the overridden proxy annotations
func (conf *ResourceConfig) GetOverriddenConfiguration() map[string]string {
	proxyOverrideConfig := map[string]string{}
	for _, annotation := range ProxyAnnotations {
		proxyOverrideConfig[annotation] = conf.pod.meta.Annotations[annotation]
	}

	return proxyOverrideConfig
}

// IsControlPlaneComponent returns true if the component is part of linkerd control plane
func (conf *ResourceConfig) IsControlPlaneComponent() bool {
	_, b := conf.pod.meta.Labels[k8s.ControllerComponentLabel]
	return b
}

func sortedKeys(m map[string]string) []string {
	keys := []string{}
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

//IsNamespace checks if a given config is a workload of Kind namespace
func (conf *ResourceConfig) IsNamespace() bool {
	return strings.ToLower(conf.workload.metaType.Kind) == k8s.Namespace
}

//InjectNamespace annotates any given Namespace config
func (conf *ResourceConfig) InjectNamespace(annotations map[string]string) ([]byte, error) {
	ns, ok := conf.workload.obj.(*corev1.Namespace)
	if !ok {
		return nil, errors.New("can't inject namespace. Type assertion failed")
	}
	ns.Annotations[k8s.ProxyInjectAnnotation] = k8s.ProxyInjectEnabled
	//For overriding annotations
	if len(annotations) > 0 {
		for annotation, value := range annotations {
			ns.Annotations[annotation] = value
		}
	}

	j, err := getFilteredJSON(ns)
	if err != nil {
		return nil, err
	}
	return yaml.JSONToYAML(j)
}

//getFilteredJSON method performs JSON marshaling such that zero values of
//empty structs are respected by `omitempty` tags. We make use of a drop-in
//replacement of the standard json/encoding library, without which empty struct values
//present in workload objects would make it into the marshaled JSON.
func getFilteredJSON(conf runtime.Object) ([]byte, error) {
	return jsonfilter.Marshal(&conf)
}

// ToWholeCPUCores coerces a k8s resource value to a whole integer value, rounding up.
func ToWholeCPUCores(q k8sResource.Quantity) (int64, error) {
	q.RoundUp(0)
	if n, ok := q.AsInt64(); ok {
		return n, nil
	}
	return 0, fmt.Errorf("Could not parse cores: %s", q.String())
}
