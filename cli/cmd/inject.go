package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"github.com/runconduit/conduit/pkg/k8s"
	"github.com/spf13/cobra"
	appsV1 "k8s.io/api/apps/v1"
	batchV1 "k8s.io/api/batch/v1"
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	yamlDecoder "k8s.io/apimachinery/pkg/util/yaml"
)

const (
	// LocalhostDNSNameOverride allows override of the controlPlaneDNS
	LocalhostDNSNameOverride = "localhost"
	// ControlPlanePodName default control plane pod name.
	ControlPlanePodName = "controller"
)

type injectOptions struct {
	inboundPort         uint
	outboundPort        uint
	ignoreInboundPorts  []uint
	ignoreOutboundPorts []uint
	*proxyConfigOptions
}

func newInjectOptions() *injectOptions {
	return &injectOptions{
		inboundPort:         4143,
		outboundPort:        4140,
		ignoreInboundPorts:  nil,
		ignoreOutboundPorts: nil,
		proxyConfigOptions:  newProxyConfigOptions(),
	}
}

func newCmdInject() *cobra.Command {
	options := newInjectOptions()

	cmd := &cobra.Command{
		Use:   "inject [flags] CONFIG-FILE",
		Short: "Add the Conduit proxy to a Kubernetes config",
		Long: `Add the Conduit proxy to a Kubernetes config.

You can use a config file from stdin by using the '-' argument
with 'conduit inject'. e.g. curl http://url.to/yml | conduit inject -
	`,
		RunE: func(cmd *cobra.Command, args []string) error {

			if len(args) < 1 {
				return fmt.Errorf("please specify a kubernetes resource file")
			}

			if _, err := time.ParseDuration(options.proxyBindTimeout); err != nil {
				return fmt.Errorf("Invalid duration '%s' for --proxy-bind-timeout flag", options.proxyBindTimeout)
			}

			var in io.Reader
			var err error

			if args[0] == "-" {
				in = os.Stdin
			} else {
				if in, err = os.Open(args[0]); err != nil {
					return err
				}
			}
			exitCode := runInjectCmd(in, os.Stderr, os.Stdout, options)
			os.Exit(exitCode)
			return nil
		},
	}

	addProxyConfigFlags(cmd, options.proxyConfigOptions)
	cmd.PersistentFlags().UintVar(&options.inboundPort, "inbound-port", options.inboundPort, "Proxy port to use for inbound traffic")
	cmd.PersistentFlags().UintVar(&options.outboundPort, "outbound-port", options.outboundPort, "Proxy port to use for outbound traffic")
	cmd.PersistentFlags().UintSliceVar(&options.ignoreInboundPorts, "skip-inbound-ports", options.ignoreInboundPorts, "Ports that should skip the proxy and send directly to the application")
	cmd.PersistentFlags().UintSliceVar(&options.ignoreOutboundPorts, "skip-outbound-ports", options.ignoreOutboundPorts, "Outbound ports that should skip the proxy")

	return cmd
}

// Returns the integer representation of os.Exit code; 0 on success and 1 on failure.
func runInjectCmd(input io.Reader, errWriter, outWriter io.Writer, options *injectOptions) int {
	postInjectBuf := &bytes.Buffer{}
	err := InjectYAML(input, postInjectBuf, options)
	if err != nil {
		fmt.Fprintf(errWriter, "Error injecting conduit proxy: %v\n", err)
		return 1
	}
	_, err = io.Copy(outWriter, postInjectBuf)
	if err != nil {
		fmt.Fprintf(errWriter, "Error printing YAML: %v\n", err)
		return 1
	}
	return 0
}

/* Given a PodTemplateSpec, return a new PodTemplateSpec with the sidecar
 * and init-container injected. If the pod is unsuitable for having them
 * injected, return null.
 */
func injectPodTemplateSpec(t *v1.PodTemplateSpec, controlPlaneDNSNameOverride string, k8sLabels map[string]string, options *injectOptions) bool {
	// Pods with `hostNetwork=true` share a network namespace with the host. The
	// init-container would destroy the iptables configuration on the host, so
	// skip the injection in this case.
	if t.Spec.HostNetwork {
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

	initContainer := v1.Container{
		Name:                     "conduit-init",
		Image:                    fmt.Sprintf("%s:%s", options.initImage, options.conduitVersion),
		ImagePullPolicy:          v1.PullPolicy(options.imagePullPolicy),
		TerminationMessagePolicy: v1.TerminationMessageFallbackToLogsOnError,
		Args: initArgs,
		SecurityContext: &v1.SecurityContext{
			Capabilities: &v1.Capabilities{
				Add: []v1.Capability{v1.Capability("NET_ADMIN")},
			},
			Privileged: &f,
		},
	}
	controlPlaneDNS := fmt.Sprintf("proxy-api.%s.svc.cluster.local", controlPlaneNamespace)
	if controlPlaneDNSNameOverride != "" {
		controlPlaneDNS = controlPlaneDNSNameOverride
	}

	sidecar := v1.Container{
		Name:                     "conduit-proxy",
		Image:                    fmt.Sprintf("%s:%s", options.proxyImage, options.conduitVersion),
		ImagePullPolicy:          v1.PullPolicy(options.imagePullPolicy),
		TerminationMessagePolicy: v1.TerminationMessageFallbackToLogsOnError,
		SecurityContext: &v1.SecurityContext{
			RunAsUser: &options.proxyUID,
		},
		Ports: []v1.ContainerPort{
			{
				Name:          "conduit-proxy",
				ContainerPort: int32(options.inboundPort),
			},
			{
				Name:          "conduit-metrics",
				ContainerPort: int32(options.proxyMetricsPort),
			},
		},
		Env: []v1.EnvVar{
			{Name: "CONDUIT_PROXY_LOG", Value: options.proxyLogLevel},
			{Name: "CONDUIT_PROXY_BIND_TIMEOUT", Value: options.proxyBindTimeout},
			{
				Name:  "CONDUIT_PROXY_CONTROL_URL",
				Value: fmt.Sprintf("tcp://%s:%d", controlPlaneDNS, options.proxyAPIPort),
			},
			{Name: "CONDUIT_PROXY_CONTROL_LISTENER", Value: fmt.Sprintf("tcp://0.0.0.0:%d", options.proxyControlPort)},
			{Name: "CONDUIT_PROXY_METRICS_LISTENER", Value: fmt.Sprintf("tcp://0.0.0.0:%d", options.proxyMetricsPort)},
			{Name: "CONDUIT_PROXY_PRIVATE_LISTENER", Value: fmt.Sprintf("tcp://127.0.0.1:%d", options.outboundPort)},
			{Name: "CONDUIT_PROXY_PUBLIC_LISTENER", Value: fmt.Sprintf("tcp://0.0.0.0:%d", options.inboundPort)},
			{
				Name:      "CONDUIT_PROXY_POD_NAMESPACE",
				ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
			},
		},
	}

	if t.Annotations == nil {
		t.Annotations = make(map[string]string)
	}
	t.Annotations[k8s.CreatedByAnnotation] = k8s.CreatedByAnnotationValue()
	t.Annotations[k8s.ProxyVersionAnnotation] = options.conduitVersion

	if t.Labels == nil {
		t.Labels = make(map[string]string)
	}
	t.Labels[k8s.ControllerNSLabel] = controlPlaneNamespace
	for k, v := range k8sLabels {
		t.Labels[k] = v
	}

	t.Spec.Containers = append(t.Spec.Containers, sidecar)
	t.Spec.InitContainers = append(t.Spec.InitContainers, initContainer)

	return true
}

// InjectYAML takes an input stream of YAML, outputting injected YAML to out.
func InjectYAML(in io.Reader, out io.Writer, options *injectOptions) error {
	reader := yamlDecoder.NewYAMLReader(bufio.NewReaderSize(in, 4096))

	// Iterate over all YAML objects in the input
	for {
		// Read a single YAML object
		bytes, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		result, err := injectResource(bytes, options)
		if err != nil {
			return err
		}

		out.Write(result)
		out.Write([]byte("---\n"))
	}

	return nil
}

func injectList(b []byte, options *injectOptions) ([]byte, error) {
	var sourceList v1.List
	if err := yaml.Unmarshal(b, &sourceList); err != nil {
		return nil, err
	}

	items := []runtime.RawExtension{}

	for _, item := range sourceList.Items {
		result, err := injectResource(item.Raw, options)
		if err != nil {
			return nil, err
		}

		// At this point, we have yaml. The kubernetes internal representation is
		// json. Because we're building a list from RawExtensions, the yaml needs
		// to be converted to json.
		injected, err := yaml.YAMLToJSON(result)
		if err != nil {
			return nil, err
		}

		items = append(items, runtime.RawExtension{Raw: injected})
	}

	sourceList.Items = items
	return yaml.Marshal(sourceList)
}

func injectResource(bytes []byte, options *injectOptions) ([]byte, error) {
	// The Kuberentes API is versioned and each version has an API modeled
	// with its own distinct Go types. If we tell `yaml.Unmarshal()` which
	// version we support then it will provide a representation of that
	// object using the given type if possible. However, it only allows us
	// to supply one object (of one type), so first we have to determine
	// what kind of object `bytes` represents so we can pass an object of
	// the correct type to `yaml.Unmarshal()`.
	// ---------------------------------------
	// Note: bytes is expected to be YAML and will only modify it when a
	// supported type is found. Otherwise, it is returned unmodified.

	// Unmarshal the object enough to read the Kind field
	var meta metaV1.TypeMeta
	if err := yaml.Unmarshal(bytes, &meta); err != nil {
		return nil, err
	}

	// obj and podTemplateSpec will reference zero or one the following
	// objects, depending on the type.
	var obj interface{}
	var podTemplateSpec *v1.PodTemplateSpec
	var DNSNameOverride string
	k8sLabels := map[string]string{}

	// When injecting the conduit proxy into a conduit controller pod. The conduit proxy's
	// CONDUIT_PROXY_CONTROL_URL variable must be set to localhost for the following reasons:
	//	1. According to https://github.com/kubernetes/minikube/issues/1568, minikube has an issue
	//     where pods are unable to connect to themselves through their associated service IP.
	//     Setting the CONDUIT_PROXY_CONTROL_URL to localhost allows the proxy to bypass kube DNS
	//     name resolution as a workaround to this issue.
	//  2. We avoid the TLS overhead in encrypting and decrypting intra-pod traffic i.e. traffic
	//     between containers in the same pod.
	//  3. Using a Service IP instead of localhost would mean intra-pod traffic would be load-balanced
	//     across all controller pod replicas. This is undesirable as we would want all traffic between
	//	   containers to be self contained.
	//  4. We skip recording telemetry for intra-pod traffic within the control plane.
	switch meta.Kind {
	case "Deployment":
		var deployment v1beta1.Deployment
		if err := yaml.Unmarshal(bytes, &deployment); err != nil {
			return nil, err
		}

		if deployment.Name == ControlPlanePodName && deployment.Namespace == controlPlaneNamespace {
			DNSNameOverride = LocalhostDNSNameOverride
		}

		obj = &deployment
		k8sLabels[k8s.ProxyDeploymentLabel] = deployment.Name
		podTemplateSpec = &deployment.Spec.Template
	case "ReplicationController":
		var rc v1.ReplicationController
		if err := yaml.Unmarshal(bytes, &rc); err != nil {
			return nil, err
		}

		obj = &rc
		k8sLabels[k8s.ProxyReplicationControllerLabel] = rc.Name
		podTemplateSpec = rc.Spec.Template
	case "ReplicaSet":
		var rs v1beta1.ReplicaSet
		if err := yaml.Unmarshal(bytes, &rs); err != nil {
			return nil, err
		}

		obj = &rs
		k8sLabels[k8s.ProxyReplicaSetLabel] = rs.Name
		podTemplateSpec = &rs.Spec.Template
	case "Job":
		var job batchV1.Job
		if err := yaml.Unmarshal(bytes, &job); err != nil {
			return nil, err
		}

		obj = &job
		k8sLabels[k8s.ProxyJobLabel] = job.Name
		podTemplateSpec = &job.Spec.Template
	case "DaemonSet":
		var ds v1beta1.DaemonSet
		if err := yaml.Unmarshal(bytes, &ds); err != nil {
			return nil, err
		}

		obj = &ds
		k8sLabels[k8s.ProxyDaemonSetLabel] = ds.Name
		podTemplateSpec = &ds.Spec.Template
	case "StatefulSet":
		var statefulset appsV1.StatefulSet
		if err := yaml.Unmarshal(bytes, &statefulset); err != nil {
			return nil, err
		}

		obj = &statefulset
		k8sLabels[k8s.ProxyStatefulSetLabel] = statefulset.Name
		podTemplateSpec = &statefulset.Spec.Template
	case "List":
		// Lists are a little different than the other types. There's no immediate
		// pod template. Because of this, we do a recursive call for each element
		// in the list (instead of just marshaling the injected pod template).
		return injectList(bytes, options)
	}

	// If we don't inject anything into the pod template then output the
	// original serialization of the original object. Otherwise, output the
	// serialization of the modified object.
	output := bytes
	if podTemplateSpec != nil && injectPodTemplateSpec(podTemplateSpec, DNSNameOverride, k8sLabels, options) {
		var err error
		output, err = yaml.Marshal(obj)
		if err != nil {
			return nil, err
		}
	}

	return output, nil
}
