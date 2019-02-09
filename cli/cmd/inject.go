package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	v1 "k8s.io/api/core/v1"
	k8sMeta "k8s.io/apimachinery/pkg/api/meta"
	k8sResource "k8s.io/apimachinery/pkg/api/resource"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
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

	// for inject reports

	hostNetworkDesc    = "pods do not use host networking"
	sidecarDesc        = "pods do not have a 3rd party proxy or initContainer already injected"
	injectDisabledDesc = "pods are not annotated to disable injection"
	unsupportedDesc    = "at least one resource injected"
	udpDesc            = "pod specs do not include UDP ports"
)

type injectOptions struct {
	*proxyConfigOptions
}

type resourceTransformerInject struct{}

// InjectYAML processes resource definitions and outputs them after injection in out
func InjectYAML(in io.Reader, out io.Writer, report io.Writer, options *injectOptions) error {
	return ProcessYAML(in, out, report, options, resourceTransformerInject{})
}

func runInjectCmd(inputs []io.Reader, errWriter, outWriter io.Writer, options *injectOptions) int {
	return transformInput(inputs, errWriter, outWriter, options, resourceTransformerInject{})
}

// objMeta provides a generic struct to parse the names of Kubernetes objects
type objMeta struct {
	metaV1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
}

func newInjectOptions() *injectOptions {
	return &injectOptions{
		proxyConfigOptions: newProxyConfigOptions(),
	}
}

func newCmdInject() *cobra.Command {
	options := newInjectOptions()

	cmd := &cobra.Command{
		Use:   "inject [flags] CONFIG-FILE",
		Short: "Add the Linkerd proxy to a Kubernetes config",
		Long: `Add the Linkerd proxy to a Kubernetes config.

You can inject resources contained in a single file, inside a folder and its
sub-folders, or coming from stdin.`,
		Example: `  # Inject all the deployments in the default namespace.
  kubectl get deploy -o yaml | linkerd inject - | kubectl apply -f -

  # Download a resource and inject it through stdin.
  curl http://url.to/yml | linkerd inject - | kubectl apply -f -

  # Inject all the resources inside a folder and its sub-folders.
  linkerd inject <folder> | kubectl apply -f -`,
		RunE: func(cmd *cobra.Command, args []string) error {

			if len(args) < 1 {
				return fmt.Errorf("please specify a kubernetes resource file")
			}

			if err := options.validate(); err != nil {
				return err
			}

			in, err := read(args[0])
			if err != nil {
				return err
			}

			exitCode := uninjectAndInject(in, stderr, stdout, options)
			os.Exit(exitCode)
			return nil
		},
	}

	addProxyConfigFlags(cmd, options.proxyConfigOptions)

	return cmd
}

func uninjectAndInject(inputs []io.Reader, errWriter, outWriter io.Writer, options *injectOptions) int {
	var out bytes.Buffer
	if exitCode := runUninjectSilentCmd(inputs, errWriter, &out, nil); exitCode != 0 {
		return exitCode
	}
	return runInjectCmd([]io.Reader{&out}, errWriter, outWriter, options)
}

/* Given a ObjectMeta, update ObjectMeta in place with the new labels and
 * annotations.
 */
func injectObjectMeta(t *metaV1.ObjectMeta, k8sLabels map[string]string, options *injectOptions, report *injectReport) bool {
	report.injectDisabled = injectDisabled(t)
	if report.injectDisabled {
		return false
	}

	if t.Annotations == nil {
		t.Annotations = make(map[string]string)
	}
	t.Annotations[k8s.CreatedByAnnotation] = k8s.CreatedByAnnotationValue()
	t.Annotations[k8s.ProxyVersionAnnotation] = options.linkerdVersion

	if t.Labels == nil {
		t.Labels = make(map[string]string)
	}
	t.Labels[k8s.ControllerNSLabel] = controlPlaneNamespace
	for k, v := range k8sLabels {
		t.Labels[k] = v
	}

	return true
}

/* Given a PodSpec, update the PodSpec in place with the sidecar
 * and init-container injected. If the pod is unsuitable for having them
 * injected, return false.
 */
func injectPodSpec(t *v1.PodSpec, identity k8s.TLSIdentity, controlPlaneDNSNameOverride string, options *injectOptions, report *injectReport) bool {
	report.hostNetwork = t.HostNetwork
	report.sidecar = healthcheck.HasExistingSidecars(t)
	report.udp = checkUDPPorts(t)

	// Skip injection if:
	// 1) Pods with `hostNetwork: true` share a network namespace with the host.
	//    The init-container would destroy the iptables configuration on the host.
	// OR
	// 2) Known 3rd party sidecars already present.
	if report.hostNetwork || report.sidecar {
		return false
	}

	f := false
	inboundSkipPorts := append(options.ignoreInboundPorts, options.proxyControlPort, options.proxyMetricsPort)
	inboundSkipPortsStr := make([]string, len(inboundSkipPorts))
	for i, p := range inboundSkipPorts {
		inboundSkipPortsStr[i] = strconv.Itoa(int(p))
	}

	outboundSkipPortsStr := make([]string, len(options.ignoreOutboundPorts))
	for i, p := range options.ignoreOutboundPorts {
		outboundSkipPortsStr[i] = strconv.Itoa(int(p))
	}

	initArgs := []string{
		"--incoming-proxy-port", fmt.Sprintf("%d", options.inboundPort),
		"--outgoing-proxy-port", fmt.Sprintf("%d", options.outboundPort),
		"--proxy-uid", fmt.Sprintf("%d", options.proxyUID),
	}

	if len(inboundSkipPortsStr) > 0 {
		initArgs = append(initArgs, "--inbound-ports-to-ignore")
		initArgs = append(initArgs, strings.Join(inboundSkipPortsStr, ","))
	}

	if len(outboundSkipPortsStr) > 0 {
		initArgs = append(initArgs, "--outbound-ports-to-ignore")
		initArgs = append(initArgs, strings.Join(outboundSkipPortsStr, ","))
	}

	controlPlaneDNS := fmt.Sprintf("linkerd-proxy-api.%s.svc.cluster.local", controlPlaneNamespace)
	if controlPlaneDNSNameOverride != "" {
		controlPlaneDNS = controlPlaneDNSNameOverride
	}

	metricsPort := intstr.IntOrString{
		IntVal: int32(options.proxyMetricsPort),
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
	}

	if options.proxyCPURequest != "" {
		resources.Requests["cpu"] = k8sResource.MustParse(options.proxyCPURequest)
	}

	if options.proxyMemoryRequest != "" {
		resources.Requests["memory"] = k8sResource.MustParse(options.proxyMemoryRequest)
	}

	profileSuffixes := "."
	if options.disableExternalProfiles {
		profileSuffixes = "svc.cluster.local."
	}
	sidecar := v1.Container{
		Name:                     k8s.ProxyContainerName,
		Image:                    options.taggedProxyImage(),
		ImagePullPolicy:          v1.PullPolicy(options.imagePullPolicy),
		TerminationMessagePolicy: v1.TerminationMessageFallbackToLogsOnError,
		SecurityContext: &v1.SecurityContext{
			RunAsUser: &options.proxyUID,
		},
		Ports: []v1.ContainerPort{
			{
				Name:          "linkerd-proxy",
				ContainerPort: int32(options.inboundPort),
			},
			{
				Name:          "linkerd-metrics",
				ContainerPort: int32(options.proxyMetricsPort),
			},
		},
		Resources: resources,
		Env: []v1.EnvVar{
			{Name: "LINKERD2_PROXY_LOG", Value: options.proxyLogLevel},
			{
				Name:  "LINKERD2_PROXY_CONTROL_URL",
				Value: fmt.Sprintf("tcp://%s:%d", controlPlaneDNS, options.proxyAPIPort),
			},
			{Name: "LINKERD2_PROXY_CONTROL_LISTENER", Value: fmt.Sprintf("tcp://0.0.0.0:%d", options.proxyControlPort)},
			{Name: "LINKERD2_PROXY_METRICS_LISTENER", Value: fmt.Sprintf("tcp://0.0.0.0:%d", options.proxyMetricsPort)},
			{Name: "LINKERD2_PROXY_OUTBOUND_LISTENER", Value: fmt.Sprintf("tcp://127.0.0.1:%d", options.outboundPort)},
			{Name: "LINKERD2_PROXY_INBOUND_LISTENER", Value: fmt.Sprintf("tcp://0.0.0.0:%d", options.inboundPort)},
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
		if capacity, ok := options.proxyOutboundCapacity[container.Image]; ok {
			sidecar.Env = append(sidecar.Env,
				v1.EnvVar{
					Name:  "LINKERD2_PROXY_OUTBOUND_ROUTER_CAPACITY",
					Value: fmt.Sprintf("%d", capacity),
				},
			)
			break
		}
	}

	if options.enableTLS() {
		yes := true

		configMapVolume := v1.Volume{
			Name: k8s.TLSTrustAnchorVolumeName,
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: v1.LocalObjectReference{Name: k8s.TLSTrustAnchorConfigMapName},
					Optional:             &yes,
				},
			},
		}
		secretVolume := v1.Volume{
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
			{Name: "LINKERD2_PROXY_CONTROLLER_NAMESPACE", Value: controlPlaneNamespace},
			{Name: "LINKERD2_PROXY_TLS_CONTROLLER_IDENTITY", Value: identity.ToControllerIdentity().ToDNSName()},
		}

		sidecar.Env = append(sidecar.Env, tlsEnvVars...)
		sidecar.VolumeMounts = []v1.VolumeMount{
			{Name: configMapVolume.Name, MountPath: configMapBase, ReadOnly: true},
			{Name: secretVolume.Name, MountPath: secretBase, ReadOnly: true},
		}

		t.Volumes = append(t.Volumes, configMapVolume, secretVolume)
	}

	t.Containers = append(t.Containers, sidecar)
	if !options.noInitContainer {
		nonRoot := false
		runAsUser := int64(0)
		initContainer := v1.Container{
			Name:                     k8s.InitContainerName,
			Image:                    options.taggedProxyInitImage(),
			ImagePullPolicy:          v1.PullPolicy(options.imagePullPolicy),
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
		t.InitContainers = append(t.InitContainers, initContainer)
	}

	return true
}

func (rt resourceTransformerInject) transform(bytes []byte, options *injectOptions) ([]byte, []injectReport, error) {
	conf := &resourceConfig{}
	output, reports, err := conf.parse(bytes, options, rt)
	if output != nil || err != nil {
		return output, reports, err
	}

	report := injectReport{
		kind: strings.ToLower(conf.meta.Kind),
		name: conf.om.Name,
	}

	// If we don't inject anything into the pod template then output the
	// original serialization of the original object. Otherwise, output the
	// serialization of the modified object.
	output = bytes
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
			ControllerNamespace: controlPlaneNamespace,
		}

		if injectPodSpec(conf.podSpec, identity, conf.dnsNameOverride, options, &report) &&
			injectObjectMeta(conf.objectMeta, conf.k8sLabels, options, &report) {
			var err error
			output, err = yaml.Marshal(conf.obj)
			if err != nil {
				return nil, nil, err
			}
		}
	} else {
		report.unsupportedResource = true
	}

	return output, []injectReport{report}, nil
}

func (resourceTransformerInject) generateReport(injectReports []injectReport, output io.Writer) {
	injected := []injectReport{}
	hostNetwork := []string{}
	sidecar := []string{}
	udp := []string{}
	injectDisabled := []string{}
	warningsPrinted := verbose

	for _, r := range injectReports {
		if !r.hostNetwork && !r.sidecar && !r.unsupportedResource && !r.injectDisabled {
			injected = append(injected, r)
		}

		if r.hostNetwork {
			hostNetwork = append(hostNetwork, r.resName())
			warningsPrinted = true
		}

		if r.sidecar {
			sidecar = append(sidecar, r.resName())
			warningsPrinted = true
		}

		if r.udp {
			udp = append(udp, r.resName())
			warningsPrinted = true
		}

		if r.injectDisabled {
			injectDisabled = append(injectDisabled, r.resName())
			warningsPrinted = true
		}
	}

	//
	// Warnings
	//

	// leading newline to separate from yaml output on stdout
	output.Write([]byte("\n"))

	if len(hostNetwork) > 0 {
		output.Write([]byte(fmt.Sprintf("%s \"hostNetwork: true\" detected in %s\n", warnStatus, strings.Join(hostNetwork, ", "))))
	} else if verbose {
		output.Write([]byte(fmt.Sprintf("%s %s\n", okStatus, hostNetworkDesc)))
	}

	if len(sidecar) > 0 {
		output.Write([]byte(fmt.Sprintf("%s known 3rd party sidecar detected in %s\n", warnStatus, strings.Join(sidecar, ", "))))
	} else if verbose {
		output.Write([]byte(fmt.Sprintf("%s %s\n", okStatus, sidecarDesc)))
	}

	if len(injectDisabled) > 0 {
		output.Write([]byte(fmt.Sprintf("%s \"%s: %s\" annotation set on %s\n",
			warnStatus, k8s.ProxyInjectAnnotation, k8s.ProxyInjectDisabled, strings.Join(injectDisabled, ", "))))
	} else if verbose {
		output.Write([]byte(fmt.Sprintf("%s %s\n", okStatus, injectDisabledDesc)))
	}

	if len(injected) == 0 {
		output.Write([]byte(fmt.Sprintf("%s no supported objects found\n", warnStatus)))
		warningsPrinted = true
	} else if verbose {
		output.Write([]byte(fmt.Sprintf("%s %s\n", okStatus, unsupportedDesc)))
	}

	if len(udp) > 0 {
		verb := "uses"
		if len(udp) > 1 {
			verb = "use"
		}
		output.Write([]byte(fmt.Sprintf("%s %s %s \"protocol: UDP\"\n", warnStatus, strings.Join(udp, ", "), verb)))
	} else if verbose {
		output.Write([]byte(fmt.Sprintf("%s %s\n", okStatus, udpDesc)))
	}

	//
	// Summary
	//
	if warningsPrinted {
		output.Write([]byte("\n"))
	}

	for _, r := range injectReports {
		if !r.hostNetwork && !r.sidecar && !r.unsupportedResource && !r.injectDisabled {
			output.Write([]byte(fmt.Sprintf("%s \"%s\" injected\n", r.kind, r.name)))
		} else {
			output.Write([]byte(fmt.Sprintf("%s \"%s\" skipped\n", r.kind, r.name)))
		}
	}

	// trailing newline to separate from kubectl output if piping
	output.Write([]byte("\n"))
}

func checkUDPPorts(t *v1.PodSpec) bool {
	// check for ports with `protocol: UDP`, which will not be routed by Linkerd
	for _, container := range t.Containers {
		for _, port := range container.Ports {
			if port.Protocol == v1.ProtocolUDP {
				return true
			}
		}
	}
	return false
}

func injectDisabled(t *metaV1.ObjectMeta) bool {
	return t.GetAnnotations()[k8s.ProxyInjectAnnotation] == k8s.ProxyInjectDisabled
}
