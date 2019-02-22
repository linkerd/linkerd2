package inject

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	pb "github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	k8sMeta "k8s.io/apimachinery/pkg/api/meta"
	k8sResource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

const (
	// LocalhostDNSNameOverride allows override of the controlPlaneDNS. This
	// must be in absolute form for the proxy to special-case it.
	LocalhostDNSNameOverride = "localhost."
	// ControlPlanePodName default control plane pod name.
	ControlPlanePodName = "linkerd-controller"
	// PodNamespaceEnvVarName is the name of the variable used to pass the pod's namespace.
	PodNamespaceEnvVarName = "LINKERD2_PROXY_POD_NAMESPACE"
	// PrometheusImage is the docker image and tag for the Prometheus instance used in the control plane
	PrometheusImage                 = "prom/prometheus:v2.7.1"
	prometheusProxyOutboundCapacity = 10000

	defaultDockerRegistry = "gcr.io/linkerd-io"
	defaultKeepaliveMs    = 10000
)

// objMeta provides a generic struct to parse the names of Kubernetes objects
type objMeta struct {
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
}

// ResourceConfig contains the parsed information for a given workload
type ResourceConfig struct {
	globalConfig    *pb.GlobalConfig
	proxyConfig     *pb.ProxyConfig
	obj             interface{}
	meta            metav1.TypeMeta
	podSpec         *v1.PodSpec
	objectMeta      *objMeta
	dnsNameOverride string
	k8sLabels       map[string]string
}

// NewResourceConfig creates and initializes a ResourceConfig
func NewResourceConfig(globalConfig *pb.GlobalConfig, proxyConfig *pb.ProxyConfig) *ResourceConfig {
	return &ResourceConfig{
		globalConfig: globalConfig,
		proxyConfig:  proxyConfig,
		k8sLabels:    map[string]string{},
	}
}

// YamlMarshalObj returns the yaml for the workload in conf
func (conf *ResourceConfig) YamlMarshalObj() ([]byte, error) {
	return yaml.Marshal(conf.obj)
}

// PatchForYaml parses bytes and (if pertinent) returns the json patch
// for injecting the init and proxy containers and all the necessary metadata.
// It also returns a Report with the results of the injection.
// Note that bytes should only contain a single YAML (not multiple separated by ---)
func (conf *ResourceConfig) PatchForYaml(bytes []byte) ([]byte, []Report, error) {
	if err := yaml.Unmarshal(bytes, &conf.meta); err != nil {
		return nil, nil, err
	}
	if err := yaml.Unmarshal(bytes, &conf.objectMeta); err != nil {
		return nil, nil, err
	}
	if conf.objectMeta == nil {
		return nil, nil, nil
	}
	return conf.getPatch(bytes)
}

// ParseMetaAndYaml fills conf fields both the metatada and the workload contents
func (conf *ResourceConfig) ParseMetaAndYaml(bytes []byte) error {
	if err := yaml.Unmarshal(bytes, &conf.meta); err != nil {
		return err
	}
	if err := yaml.Unmarshal(bytes, &conf.objectMeta); err != nil {
		return err
	}
	return conf.parse(bytes)
}

// PatchForAdmissionRequest parses request.ObjectRaw and (if pertinent) returns the json patch
// for injecting the init and proxy containers and all the necessary metadata.
func (conf *ResourceConfig) PatchForAdmissionRequest(request *admissionv1beta1.AdmissionRequest) ([]byte, error) {
	conf.meta = metav1.TypeMeta{Kind: request.Kind.Kind}
	conf.objectMeta = &objMeta{metaV1.ObjectMeta{Name: request.Name, Namespace: request.Namespace}}
	patch, _, err := conf.getPatch(request.Object.Raw)
	return patch, err
}

func (conf *ResourceConfig) getPatch(bytes []byte) ([]byte, []Report, error) {
	report := NewReport(conf)
	log.Infof("working on %s %s..", strings.ToLower(conf.meta.Kind), conf.objectMeta.Name)

	if err := conf.parse(bytes); err != nil {
		return nil, []Report{report}, err
	}

	patch := NewPatch()

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
			Namespace:           "$" + PodNamespaceEnvVarName,
			ControllerNamespace: conf.globalConfig.GetLinkerdNamespace(),
		}

		if conf.injectPodSpec(patch, identity, &report) {
			if err := conf.injectObjectMeta(conf.objectMeta, patch, &report); err != nil {
				return nil, nil, err
			}
		}
	} else {
		report.UnsupportedResource = true
	}

	patchJSON, err := json.Marshal(patch.patchOps)
	log.Debugf("patch: %v\n", string(patchJSON))
	if err != nil {
		return nil, nil, err
	}

	return patchJSON, []Report{report}, nil
}

// TODO: bytes can't be a List resource, but I'll fix that soon)
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

	switch conf.meta.Kind {
	case "Deployment":
		var deployment v1beta1.Deployment
		if err := yaml.Unmarshal(bytes, &deployment); err != nil {
			return err
		}

		if deployment.Name == ControlPlanePodName && deployment.Namespace == conf.globalConfig.GetLinkerdNamespace() {
			conf.dnsNameOverride = LocalhostDNSNameOverride
		}

		conf.obj = &deployment
		conf.k8sLabels[k8s.ProxyDeploymentLabel] = deployment.Name
		conf.complete(deployment.Spec.Template)

	case "ReplicationController":
		var rc v1.ReplicationController
		if err := yaml.Unmarshal(bytes, &rc); err != nil {
			return err
		}

		conf.obj = &rc
		conf.k8sLabels[k8s.ProxyReplicationControllerLabel] = rc.Name
		conf.complete(*rc.Spec.Template)

	case "ReplicaSet":
		var rs v1beta1.ReplicaSet
		if err := yaml.Unmarshal(bytes, &rs); err != nil {
			return err
		}

		conf.obj = &rs
		conf.k8sLabels[k8s.ProxyReplicaSetLabel] = rs.Name
		conf.complete(rs.Spec.Template)

	case "Job":
		var job batchv1.Job
		if err := yaml.Unmarshal(bytes, &job); err != nil {
			return err
		}

		conf.obj = &job
		conf.k8sLabels[k8s.ProxyJobLabel] = job.Name
		conf.complete(job.Spec.Template)

	case "DaemonSet":
		var ds v1beta1.DaemonSet
		if err := yaml.Unmarshal(bytes, &ds); err != nil {
			return err
		}

		conf.obj = &ds
		conf.k8sLabels[k8s.ProxyDaemonSetLabel] = ds.Name
		conf.complete(ds.Spec.Template)

	case "StatefulSet":
		var statefulset appsv1.StatefulSet
		if err := yaml.Unmarshal(bytes, &statefulset); err != nil {
			return err
		}

		conf.obj = &statefulset
		conf.k8sLabels[k8s.ProxyStatefulSetLabel] = statefulset.Name
		conf.complete(statefulset.Spec.Template)

	case "Pod":
		var pod v1.Pod
		if err := yaml.Unmarshal(bytes, &pod); err != nil {
			return err
		}

		conf.obj = &pod
		conf.podSpec = &pod.Spec
		conf.objectMeta = &objMeta{pod.ObjectMeta}
	}

	return nil
}

func (conf *ResourceConfig) complete(template v1.PodTemplateSpec) {
	conf.podSpec = &template.Spec
	conf.objectMeta = &objMeta{template.ObjectMeta}
}

// Given a PodSpec, update the PodSpec in place with the sidecar
// and init-container injected. If the pod is unsuitable for having them
// injected, return false.
func (conf *ResourceConfig) injectPodSpec(patch *Patch, identity k8s.TLSIdentity, report *Report) bool {
	t := conf.podSpec
	report.HostNetwork = t.HostNetwork
	report.Sidecar = healthcheck.HasExistingSidecars(t)
	report.Udp = checkUDPPorts(t)

	// Skip injection if:
	// 1) Pods with `hostNetwork: true` share a network namespace with the host.
	//    The init-container would destroy the iptables configuration on the host.
	// OR
	// 2) Known 3rd party sidecars already present.
	if report.HostNetwork || report.Sidecar {
		return false
	}

	f := false
	inboundSkipPorts := append(conf.proxyConfig.GetIgnoreInboundPorts(), conf.proxyConfig.GetControlPort(), conf.proxyConfig.GetMetricsPort())
	inboundSkipPortsStr := make([]string, len(inboundSkipPorts))
	for i, p := range inboundSkipPorts {
		inboundSkipPortsStr[i] = strconv.Itoa(int(p.GetPort()))
	}

	outboundSkipPortsStr := make([]string, len(conf.proxyConfig.GetIgnoreOutboundPorts()))
	for i, p := range conf.proxyConfig.GetIgnoreOutboundPorts() {
		outboundSkipPortsStr[i] = strconv.Itoa(int(p.GetPort()))
	}

	initArgs := []string{
		"--incoming-proxy-port", fmt.Sprintf("%d", conf.proxyConfig.GetInboundPort().GetPort()),
		"--outgoing-proxy-port", fmt.Sprintf("%d", conf.proxyConfig.GetOutboundPort().GetPort()),
		"--proxy-uid", fmt.Sprintf("%d", conf.proxyConfig.GetProxyUid()),
	}

	if len(inboundSkipPortsStr) > 0 {
		initArgs = append(initArgs, "--inbound-ports-to-ignore")
		initArgs = append(initArgs, strings.Join(inboundSkipPortsStr, ","))
	}

	if len(outboundSkipPortsStr) > 0 {
		initArgs = append(initArgs, "--outbound-ports-to-ignore")
		initArgs = append(initArgs, strings.Join(outboundSkipPortsStr, ","))
	}

	controlPlaneDNS := fmt.Sprintf("linkerd-destination.%s.svc.cluster.local", conf.globalConfig.GetLinkerdNamespace())
	if conf.dnsNameOverride != "" {
		controlPlaneDNS = conf.dnsNameOverride
	}

	metricsPort := intstr.IntOrString{
		IntVal: int32(conf.proxyConfig.GetMetricsPort().GetPort()),
	}

	proxyProbe := v1.Probe{
		Handler: v1.Handler{
			HTTPGet: &v1.HTTPGetAction{
				Path: "/metrics",
				Port: metricsPort,
			},
		},
		InitialDelaySeconds: 10,
	}

	resources := v1.ResourceRequirements{
		Requests: v1.ResourceList{},
		Limits:   v1.ResourceList{},
	}

	if request := conf.proxyConfig.GetResource().GetRequestCpu(); request != "" {
		resources.Requests["cpu"] = k8sResource.MustParse(request)
	}

	if request := conf.proxyConfig.GetResource().GetRequestMemory(); request != "" {
		resources.Requests["memory"] = k8sResource.MustParse(request)
	}

	if limit := conf.proxyConfig.GetResource().GetLimitCpu(); limit != "" {
		resources.Limits["cpu"] = k8sResource.MustParse(limit)
	}

	if limit := conf.proxyConfig.GetResource().GetLimitMemory(); limit != "" {
		resources.Limits["memory"] = k8sResource.MustParse(limit)
	}

	profileSuffixes := "."
	if conf.proxyConfig.GetDisableExternalProfiles() {
		profileSuffixes = "svc.cluster.local."
	}
	proxyUid := conf.proxyConfig.GetProxyUid()
	sidecar := v1.Container{
		Name:                     k8s.ProxyContainerName,
		Image:                    conf.taggedProxyImage(),
		ImagePullPolicy:          v1.PullPolicy(conf.proxyConfig.GetProxyImage().GetPullPolicy()),
		TerminationMessagePolicy: v1.TerminationMessageFallbackToLogsOnError,
		SecurityContext: &v1.SecurityContext{
			RunAsUser: &proxyUid,
		},
		Ports: []v1.ContainerPort{
			{
				Name:          "linkerd-proxy",
				ContainerPort: int32(conf.proxyConfig.GetInboundPort().GetPort()),
			},
			{
				Name:          "linkerd-metrics",
				ContainerPort: int32(conf.proxyConfig.GetMetricsPort().GetPort()),
			},
		},
		Resources: resources,
		Env: []v1.EnvVar{
			{Name: "LINKERD2_PROXY_LOG", Value: conf.proxyConfig.GetLogLevel().GetLevel()},
			{
				Name:  "LINKERD2_PROXY_CONTROL_URL",
				Value: fmt.Sprintf("tcp://%s:%d", controlPlaneDNS, conf.proxyConfig.GetApiPort().GetPort()),
			},
			{Name: "LINKERD2_PROXY_CONTROL_LISTENER", Value: fmt.Sprintf("tcp://0.0.0.0:%d", conf.proxyConfig.GetControlPort().GetPort())},
			{Name: "LINKERD2_PROXY_METRICS_LISTENER", Value: fmt.Sprintf("tcp://0.0.0.0:%d", conf.proxyConfig.GetMetricsPort().GetPort())},
			{Name: "LINKERD2_PROXY_OUTBOUND_LISTENER", Value: fmt.Sprintf("tcp://127.0.0.1:%d", conf.proxyConfig.GetOutboundPort().GetPort())},
			{Name: "LINKERD2_PROXY_INBOUND_LISTENER", Value: fmt.Sprintf("tcp://0.0.0.0:%d", conf.proxyConfig.GetInboundPort().GetPort())},
			{Name: "LINKERD2_PROXY_DESTINATION_PROFILE_SUFFIXES", Value: profileSuffixes},
			{
				Name:      PodNamespaceEnvVarName,
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
	// This is currently only used by the Prometheus pod in the control-plane.
	for _, container := range t.Containers {
		if container.Image == PrometheusImage {
			sidecar.Env = append(sidecar.Env,
				v1.EnvVar{
					Name:  "LINKERD2_PROXY_OUTBOUND_ROUTER_CAPACITY",
					Value: fmt.Sprintf("%d", prometheusProxyOutboundCapacity),
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

		if len(t.Volumes) == 0 {
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
			ImagePullPolicy:          v1.PullPolicy(conf.proxyConfig.GetProxyInitImage().GetPullPolicy()),
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
		if len(t.InitContainers) == 0 {
			patch.addInitContainerRoot()
		}
		patch.addInitContainer(initContainer)
	}

	return true
}

// Given a ObjectMeta, update ObjectMeta in place with the new labels and
// annotations.
func (conf *ResourceConfig) injectObjectMeta(t *objMeta, patch *Patch, report *Report) error {
	res, err := conf.shouldInject()
	if err != nil {
		return err
	}
	report.InjectDisabled = res
	if report.InjectDisabled {
		log.Infof("skipping workload %s", conf.objectMeta.Name)
		return nil
	}

	if len(t.Annotations) == 0 {
		patch.addPodAnnotationsRoot()
	}
	patch.addPodAnnotation(k8s.CreatedByAnnotation, k8s.CreatedByAnnotationValue())
	patch.addPodAnnotation(k8s.ProxyVersionAnnotation, version.Version)

	if globalConfig.GetIdentityContext() != nil {
		patch.addPodAnnotation(k8s.IdentityModeAnnotation, k8s.IdentityModeOptional)
	} else {
		patch.addPodAnnotation(k8s.IdentityModeAnnotation, k8s.IdentityModeDisabled)
	}

	if len(t.Labels) == 0 {
		patch.addPodLabelsRoot()
	}
	patch.addPodLabel(k8s.ControllerNSLabel, conf.globalConfig.GetLinkerdNamespace())
	for k, v := range conf.k8sLabels {
		patch.addPodLabel(k, v)
	}

	return nil
}

func checkUDPPorts(t *v1.PodSpec) bool {
	// Check for ports with `protocol: UDP`, which will not be routed by Linkerd
	for _, container := range t.Containers {
		for _, port := range container.Ports {
			if port.Protocol == v1.ProtocolUDP {
				return true
			}
		}
	}
	return false
}

func (conf *ResourceConfig) taggedProxyImage() string {
	name := conf.proxyConfig.GetProxyImage().GetImageName()
	reg := conf.proxyConfig.GetProxyImage().GetRegistry()
	image := strings.Replace(name, defaultDockerRegistry, reg, 1)
	return fmt.Sprintf("%s:%s", image, version.Version)
}

func (conf *ResourceConfig) taggedProxyInitImage() string {
	name := conf.proxyConfig.GetProxyInitImage().GetImageName()
	reg := conf.proxyConfig.GetProxyInitImage().GetRegistry()
	image := strings.Replace(name, defaultDockerRegistry, reg, 1)
	return fmt.Sprintf("%s:%s", image, version.Version)
}

// shouldInject determines whether or not the given deployment should be
// injected. A deployment should be injected if it does not already contain
// any known sidecars, and:
// - the deployment's namespace has the linkerd.io/inject annotation set to
//   "enabled", and the deployment's pod spec does not have the
//   linkerd.io/inject annotation set to "disabled"; or
// - the deployment's pod spec has the linkerd.io/inject annotation set to
//   "enabled"
func (conf *ResourceConfig) shouldInject() (bool, error) {
	if healthcheck.HasExistingSidecars(conf.podSpec) {
		return false, nil
	}

	kubeAPI, err := k8s.NewAPI("", "")
	if err != nil {
		return false, err
	}
	clientset, err := kubernetes.NewForConfig(kubeAPI.Config)
	if err != nil {
		return false, fmt.Errorf("failed to initialize Kubernetes client: %s", err)
	}

	ns := conf.objectMeta.Namespace
	if ns == "" {
		ns = v1.NamespaceDefault
	}
	log.Infof("resource namespace: %s", ns)

	namespace, err := clientset.CoreV1().Namespaces().Get(ns, metav1.GetOptions{})
	if err != nil {
		return false, err
	}

	nsAnnotation := namespace.GetAnnotations()[k8s.ProxyInjectAnnotation]
	podAnnotation := conf.objectMeta.Annotations[k8s.ProxyInjectAnnotation]

	if nsAnnotation == k8s.ProxyInjectEnabled && podAnnotation != k8s.ProxyInjectDisabled {
		return true, nil
	}

	return podAnnotation == k8s.ProxyInjectEnabled, nil
}
