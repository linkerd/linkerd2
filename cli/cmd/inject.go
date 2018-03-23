package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/runconduit/conduit/pkg/k8s"
	"github.com/runconduit/conduit/pkg/version"
	"github.com/spf13/cobra"
	batchV1 "k8s.io/api/batch/v1"
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	yamlDecoder "k8s.io/apimachinery/pkg/util/yaml"
)

const (
	LocalhostDNSNameOverride = "localhost"
	ControlPlanePodName      = "controller"
)

var (
	initImage           string
	proxyImage          string
	proxyUID            int64
	inboundPort         uint
	outboundPort        uint
	ignoreInboundPorts  []uint
	ignoreOutboundPorts []uint
	proxyControlPort    uint
	proxyMetricsPort    uint
	proxyAPIPort        uint
	proxyLogLevel       string
)

var injectCmd = &cobra.Command{
	Use:   "inject [flags] CONFIG-FILE",
	Short: "Add the Conduit proxy to a Kubernetes config",
	Long: `Add the Conduit proxy to a Kubernetes config.

You can use a config file from stdin by using the '-' argument
with 'conduit inject'. e.g. curl http://url.to/yml | conduit inject -
	`,
	RunE: func(cmd *cobra.Command, args []string) error {

		if len(args) < 1 {
			return fmt.Errorf("please specify a deployment file")
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
		exitCode := runInjectCmd(in, os.Stderr, os.Stdout, conduitVersion)
		os.Exit(exitCode)
		return nil
	},
}

// Returns the integer representation of os.Exit code; 0 on success and 1 on failure.
func runInjectCmd(input io.Reader, errWriter, outWriter io.Writer, version string) int {
	postInjectBuf := &bytes.Buffer{}
	err := InjectYAML(input, postInjectBuf, version)
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
func injectPodTemplateSpec(t *v1.PodTemplateSpec, controlPlaneDNSNameOverride, version string, k8sLabels map[string]string) bool {
	// Pods with `hostNetwork=true` share a network namespace with the host. The
	// init-container would destroy the iptables configuration on the host, so
	// skip the injection in this case.
	if t.Spec.HostNetwork {
		return false
	}

	f := false
	inboundSkipPorts := append(ignoreInboundPorts, proxyControlPort)
	inboundSkipPortsStr := make([]string, len(inboundSkipPorts))
	for i, p := range inboundSkipPorts {
		inboundSkipPortsStr[i] = strconv.Itoa(int(p))
	}

	outboundSkipPortsStr := make([]string, len(ignoreOutboundPorts))
	for i, p := range ignoreOutboundPorts {
		outboundSkipPortsStr[i] = strconv.Itoa(int(p))
	}

	initArgs := []string{
		"--incoming-proxy-port", fmt.Sprintf("%d", inboundPort),
		"--outgoing-proxy-port", fmt.Sprintf("%d", outboundPort),
		"--proxy-uid", fmt.Sprintf("%d", proxyUID),
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
		Name:            "conduit-init",
		Image:           fmt.Sprintf("%s:%s", initImage, version),
		ImagePullPolicy: v1.PullPolicy(imagePullPolicy),
		Args:            initArgs,
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
		Name:            "conduit-proxy",
		Image:           fmt.Sprintf("%s:%s", proxyImage, version),
		ImagePullPolicy: v1.PullPolicy(imagePullPolicy),
		SecurityContext: &v1.SecurityContext{
			RunAsUser: &proxyUID,
		},
		Ports: []v1.ContainerPort{
			v1.ContainerPort{
				Name:          "conduit-proxy",
				ContainerPort: int32(inboundPort),
			},
			v1.ContainerPort{
				Name:          "conduit-metrics",
				ContainerPort: int32(proxyMetricsPort),
			},
		},
		Env: []v1.EnvVar{
			v1.EnvVar{Name: "CONDUIT_PROXY_LOG", Value: proxyLogLevel},
			v1.EnvVar{
				Name:  "CONDUIT_PROXY_CONTROL_URL",
				Value: fmt.Sprintf("tcp://%s:%d", controlPlaneDNS, proxyAPIPort),
			},
			v1.EnvVar{Name: "CONDUIT_PROXY_CONTROL_LISTENER", Value: fmt.Sprintf("tcp://0.0.0.0:%d", proxyControlPort)},
			v1.EnvVar{Name: "CONDUIT_PROXY_METRICS_LISTENER", Value: fmt.Sprintf("tcp://0.0.0.0:%d", proxyMetricsPort)},
			v1.EnvVar{Name: "CONDUIT_PROXY_PRIVATE_LISTENER", Value: fmt.Sprintf("tcp://127.0.0.1:%d", outboundPort)},
			v1.EnvVar{Name: "CONDUIT_PROXY_PUBLIC_LISTENER", Value: fmt.Sprintf("tcp://0.0.0.0:%d", inboundPort)},
			v1.EnvVar{
				Name:      "CONDUIT_PROXY_NODE_NAME",
				ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{FieldPath: "spec.nodeName"}},
			},
			v1.EnvVar{
				Name:      "CONDUIT_PROXY_POD_NAME",
				ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{FieldPath: "metadata.name"}},
			},
			v1.EnvVar{
				Name:      "CONDUIT_PROXY_POD_NAMESPACE",
				ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
			},
		},
	}

	if t.Annotations == nil {
		t.Annotations = make(map[string]string)
	}
	t.Annotations[k8s.CreatedByAnnotation] = k8s.CreatedByAnnotationValue()
	t.Annotations[k8s.ProxyVersionAnnotation] = version

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

func InjectYAML(in io.Reader, out io.Writer, version string) error {
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

		// The Kuberentes API is versioned and each version has an API modeled
		// with its own distinct Go types. If we tell `yaml.Unmarshal()` which
		// version we support then it will provide a representation of that
		// object using the given type if possible. However, it only allows us
		// to supply one object (of one type), so first we have to determine
		// what kind of object `bytes` represents so we can pass an object of
		// the correct type to `yaml.Unmarshal()`.

		// Unmarshal the object enough to read the Kind field
		var meta metaV1.TypeMeta
		if err := yaml.Unmarshal(bytes, &meta); err != nil {
			return err
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
			err = yaml.Unmarshal(bytes, &deployment)
			if err != nil {
				return err
			}
			if deployment.Name == ControlPlanePodName && deployment.Namespace == controlPlaneNamespace {
				DNSNameOverride = LocalhostDNSNameOverride
			}
			obj = &deployment
			k8sLabels[k8s.ProxyDeploymentLabel] = deployment.Name
			podTemplateSpec = &deployment.Spec.Template
		case "ReplicationController":
			var rc v1.ReplicationController
			err = yaml.Unmarshal(bytes, &rc)
			if err != nil {
				return err
			}
			obj = &rc
			k8sLabels[k8s.ProxyReplicationControllerLabel] = rc.Name
			podTemplateSpec = rc.Spec.Template
		case "ReplicaSet":
			var rs v1beta1.ReplicaSet
			err = yaml.Unmarshal(bytes, &rs)
			if err != nil {
				return err
			}
			obj = &rs
			k8sLabels[k8s.ProxyReplicaSetLabel] = rs.Name
			podTemplateSpec = &rs.Spec.Template
		case "Job":
			var job batchV1.Job
			err = yaml.Unmarshal(bytes, &job)
			if err != nil {
				return err
			}
			obj = &job
			k8sLabels[k8s.ProxyJobLabel] = job.Name
			podTemplateSpec = &job.Spec.Template
		case "DaemonSet":
			var ds v1beta1.DaemonSet
			err = yaml.Unmarshal(bytes, &ds)
			if err != nil {
				return err
			}
			obj = &ds
			k8sLabels[k8s.ProxyDaemonSetLabel] = ds.Name
			podTemplateSpec = &ds.Spec.Template
		}

		// If we don't inject anything into the pod template then output the
		// original serialization of the original object. Otherwise, output the
		// serialization of the modified object.
		output := bytes
		if podTemplateSpec != nil && injectPodTemplateSpec(podTemplateSpec, DNSNameOverride, version, k8sLabels) {
			output, err = yaml.Marshal(obj)
			if err != nil {
				return err
			}
		}

		out.Write(output)
		out.Write([]byte("---\n"))
	}
	return nil
}

func init() {
	RootCmd.AddCommand(injectCmd)
	addProxyConfigFlags(injectCmd)
	injectCmd.PersistentFlags().StringVar(&initImage, "init-image", "gcr.io/runconduit/proxy-init", "Conduit init container image name")
	injectCmd.PersistentFlags().UintVar(&inboundPort, "inbound-port", 4143, "proxy port to use for inbound traffic")
	injectCmd.PersistentFlags().UintVar(&outboundPort, "outbound-port", 4140, "proxy port to use for outbound traffic")
	injectCmd.PersistentFlags().UintSliceVar(&ignoreInboundPorts, "skip-inbound-ports", nil, "ports that should skip the proxy and send directly to the application")
	injectCmd.PersistentFlags().UintSliceVar(&ignoreOutboundPorts, "skip-outbound-ports", nil, "outbound ports that should skip the proxy")
}

func addProxyConfigFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVarP(&conduitVersion, "conduit-version", "v", version.Version, "tag to be used for Conduit images")
	cmd.PersistentFlags().StringVar(&proxyImage, "proxy-image", "gcr.io/runconduit/proxy", "Conduit proxy container image name")
	cmd.PersistentFlags().StringVar(&imagePullPolicy, "image-pull-policy", "IfNotPresent", "Docker image pull policy")
	cmd.PersistentFlags().Int64Var(&proxyUID, "proxy-uid", 2102, "Run the proxy under this user ID")
	cmd.PersistentFlags().StringVar(&proxyLogLevel, "proxy-log-level", "warn,conduit_proxy=info", "log level for the proxy")
	cmd.PersistentFlags().UintVar(&proxyAPIPort, "api-port", 8086, "port where the Conduit controller is running")
	cmd.PersistentFlags().UintVar(&proxyControlPort, "control-port", 4190, "proxy port to use for control")
	cmd.PersistentFlags().UintVar(&proxyMetricsPort, "metrics-port", 4191, "proxy port to serve metrics on")
}
