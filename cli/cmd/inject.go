package cmd

import (
	"bufio"
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

var (
	initImage           string
	proxyImage          string
	proxyUID            int64
	inboundPort         uint
	outboundPort        uint
	ignoreInboundPorts  []uint
	ignoreOutboundPorts []uint
	proxyControlPort    uint
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
		return InjectYAML(in, os.Stdout)
	},
}

/* Given a PodTemplateSpec, return a new PodTemplateSpec with the sidecar
 * and init-container injected. If the pod is unsuitable for having them
 * injected, return null.
 */
func injectPodTemplateSpec(t *v1.PodTemplateSpec) bool {
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
		Image:           fmt.Sprintf("%s:%s", initImage, conduitVersion),
		ImagePullPolicy: v1.PullPolicy(imagePullPolicy),
		Args:            initArgs,
		SecurityContext: &v1.SecurityContext{
			Capabilities: &v1.Capabilities{
				Add: []v1.Capability{v1.Capability("NET_ADMIN")},
			},
			Privileged: &f,
		},
	}

	sidecar := v1.Container{
		Name:            "conduit-proxy",
		Image:           fmt.Sprintf("%s:%s", proxyImage, conduitVersion),
		ImagePullPolicy: v1.PullPolicy(imagePullPolicy),
		SecurityContext: &v1.SecurityContext{
			RunAsUser: &proxyUID,
		},
		Ports: []v1.ContainerPort{
			v1.ContainerPort{
				Name:          "conduit-proxy",
				ContainerPort: int32(inboundPort),
			},
		},
		Env: []v1.EnvVar{
			v1.EnvVar{Name: "CONDUIT_PROXY_LOG", Value: proxyLogLevel},
			v1.EnvVar{
				Name:  "CONDUIT_PROXY_CONTROL_URL",
				Value: fmt.Sprintf("tcp://proxy-api.%s.svc.cluster.local:%d", controlPlaneNamespace, proxyAPIPort),
			},
			v1.EnvVar{Name: "CONDUIT_PROXY_CONTROL_LISTENER", Value: fmt.Sprintf("tcp://0.0.0.0:%d", proxyControlPort)},
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
			v1.EnvVar{
				Name:  "CONDUIT_PROXY_DESTINATIONS_AUTOCOMPLETE_FQDN",
				Value: "Kubernetes",
			},
		},
	}

	if t.Annotations == nil {
		t.Annotations = make(map[string]string)
	}
	t.Annotations[k8s.CreatedByAnnotation] = k8s.CreatedByAnnotationValue()
	t.Annotations[k8s.ProxyVersionAnnotation] = conduitVersion

	if t.Labels == nil {
		t.Labels = make(map[string]string)
	}
	t.Labels[k8s.ControllerNSLabel] = controlPlaneNamespace
	t.Spec.Containers = append(t.Spec.Containers, sidecar)
	t.Spec.InitContainers = append(t.Spec.InitContainers, initContainer)

	return true
}

func InjectYAML(in io.Reader, out io.Writer) error {
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
		var obj interface{} = nil
		var podTemplateSpec *v1.PodTemplateSpec = nil

		switch meta.Kind {
		case "Deployment":
			var deployment v1beta1.Deployment
			err = yaml.Unmarshal(bytes, &deployment)
			if err != nil {
				return err
			}
			obj = &deployment
			podTemplateSpec = &deployment.Spec.Template
		case "ReplicationController":
			var rc v1.ReplicationController
			err = yaml.Unmarshal(bytes, &rc)
			if err != nil {
				return err
			}
			obj = &rc
			podTemplateSpec = rc.Spec.Template
		case "ReplicaSet":
			var rs v1beta1.ReplicaSet
			err = yaml.Unmarshal(bytes, &rs)
			if err != nil {
				return err
			}
			obj = &rs
			podTemplateSpec = &rs.Spec.Template
		case "Job":
			var job batchV1.Job
			err = yaml.Unmarshal(bytes, &job)
			if err != nil {
				return err
			}
			obj = &job
			podTemplateSpec = &job.Spec.Template
		case "DaemonSet":
			var ds v1beta1.DaemonSet
			err = yaml.Unmarshal(bytes, &ds)
			if err != nil {
				return err
			}
			obj = &ds
			podTemplateSpec = &ds.Spec.Template
		}

		// If we don't inject anything into the pod template then output the
		// original serialization of the original object. Otherwise, output the
		// serialization of the modified object.
		output := bytes
		if podTemplateSpec != nil && injectPodTemplateSpec(podTemplateSpec) {
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
	injectCmd.PersistentFlags().StringVarP(&conduitVersion, "conduit-version", "v", version.Version, "tag to be used for Conduit images")
	injectCmd.PersistentFlags().StringVar(&initImage, "init-image", "gcr.io/runconduit/proxy-init", "Conduit init container image name")
	injectCmd.PersistentFlags().StringVar(&proxyImage, "proxy-image", "gcr.io/runconduit/proxy", "Conduit proxy container image name")
	injectCmd.PersistentFlags().StringVar(&imagePullPolicy, "image-pull-policy", "IfNotPresent", "Docker image pull policy")
	injectCmd.PersistentFlags().Int64Var(&proxyUID, "proxy-uid", 2102, "Run the proxy under this user ID")
	injectCmd.PersistentFlags().UintVar(&inboundPort, "inbound-port", 4143, "proxy port to use for inbound traffic")
	injectCmd.PersistentFlags().UintVar(&outboundPort, "outbound-port", 4140, "proxy port to use for outbound traffic")
	injectCmd.PersistentFlags().UintSliceVar(&ignoreInboundPorts, "skip-inbound-ports", nil, "ports that should skip the proxy and send directly to the application")
	injectCmd.PersistentFlags().UintSliceVar(&ignoreOutboundPorts, "skip-outbound-ports", nil, "outbound ports that should skip the proxy")
	injectCmd.PersistentFlags().UintVar(&proxyControlPort, "control-port", 4190, "proxy port to use for control")
	injectCmd.PersistentFlags().UintVar(&proxyAPIPort, "api-port", 8086, "port where the Conduit controller is running")
	injectCmd.PersistentFlags().StringVar(&proxyLogLevel, "proxy-log-level", "warn,conduit_proxy=info", "log level for the proxy")
}
