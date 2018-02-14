package k8s

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/runconduit/conduit/pkg/k8s"
	"github.com/runconduit/conduit/pkg/version"
	batchV1 "k8s.io/api/batch/v1"
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	yamlDecoder "k8s.io/apimachinery/pkg/util/yaml"
)

/* The v1.PodSpec struct contains a field annotation that causes the
 * InitContainers field to be omitted when serializing the struct as json.
 * Since we wish for this field to be included, we have to define our own
 * enhancedPodSpec struct with a different annotation on this field.  We then
 * must define our own structs to use this struct, and so on.
 */
type enhancedPodSpec struct {
	*v1.PodSpec
	InitContainers []v1.Container `json:"initContainers"`
}

type enhancedPodTemplateSpec struct {
	*v1.PodTemplateSpec
	Spec enhancedPodSpec `json:"spec,omitempty"`
}

type enhancedDeploymentSpec struct {
	*v1beta1.DeploymentSpec
	Template enhancedPodTemplateSpec `json:"template,omitempty"`
}

type enhancedDeployment struct {
	*v1beta1.Deployment
	Spec enhancedDeploymentSpec `json:"spec,omitempty"`
}

type enhancedReplicationControllerSpec struct {
	*v1.ReplicationControllerSpec
	Template enhancedPodTemplateSpec `json:"template,omitempty"`
}

type enhancedReplicationController struct {
	*v1.ReplicationController
	Spec enhancedReplicationControllerSpec `json:"spec,omitempty"`
}

type enhancedReplicaSetSpec struct {
	*v1beta1.ReplicaSetSpec
	Template enhancedPodTemplateSpec `json:"template,omitempty"`
}

type enhancedReplicaSet struct {
	*v1beta1.ReplicaSet
	Spec enhancedReplicaSetSpec `json:"spec,omitempty"`
}

type enhancedJobSpec struct {
	*batchV1.JobSpec
	Template enhancedPodTemplateSpec `json:"template,omitempty"`
}

type enhancedJob struct {
	*batchV1.Job
	Spec enhancedJobSpec `json:"spec,omitempty"`
}

type enhancedDaemonSetSpec struct {
	*v1beta1.DaemonSetSpec
	Template enhancedPodTemplateSpec `json:"template,omitempty"`
}

type enhancedDaemonSet struct {
	*v1beta1.DaemonSet
	Spec enhancedDaemonSetSpec `json:"spec,omitempty"`
}

type PodConfig struct {
	InitImage             string
	ProxyImage            string
	ProxyUID              int64
	InboundPort           uint
	OutboundPort          uint
	IgnoreInboundPorts    []uint
	IgnoreOutboundPorts   []uint
	ProxyControlPort      uint
	ProxyAPIPort          uint
	ProxyLogLevel         string
	ConduitVersion        string
	ImagePullPolicy       string
	ControlPlaneNamespace string
}

var DefaultProxyConfig = PodConfig{
	InitImage:             "gcr.io/runconduit/proxy-init",
	ProxyImage:            "gcr.io/runconduit/proxy",
	ProxyUID:              2102,
	InboundPort:           4143,
	OutboundPort:          4140,
	IgnoreInboundPorts:    nil,
	IgnoreOutboundPorts:   nil,
	ProxyControlPort:      4190,
	ProxyAPIPort:          8086,
	ConduitVersion:        version.Version,
	ImagePullPolicy:       "IfNotPresent",
	ProxyLogLevel:         "warn,conduit_proxy=info",
	ControlPlaneNamespace: "conduit",
}

func InjectYAML(in io.Reader, out io.Writer, podConfig PodConfig) error {
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

		// Unmarshal the object enough to read the Kind field
		var meta metaV1.TypeMeta
		if err := yaml.Unmarshal(bytes, &meta); err != nil {
			return err
		}

		var injected interface{} = nil
		switch meta.Kind {
		case "Deployment":
			injected, err = injectDeployment(bytes, podConfig)
		case "ReplicationController":
			injected, err = injectReplicationController(bytes, podConfig)
		case "ReplicaSet":
			injected, err = injectReplicaSet(bytes, podConfig)
		case "Job":
			injected, err = injectJob(bytes, podConfig)
		case "DaemonSet":
			injected, err = injectDaemonSet(bytes, podConfig)
		}
		output := bytes
		if injected != nil {
			output, err = yaml.Marshal(injected)
			if err != nil {
				return err
			}
		}
		out.Write(output)
		fmt.Println("---")
	}
	return nil
}

/* Given a byte slice representing a deployment, unmarshal the deployment and
 * return a new deployment with the sidecar and init-container injected.
 */
func injectDeployment(bytes []byte, podConfig PodConfig) (interface{}, error) {
	var deployment v1beta1.Deployment
	err := yaml.Unmarshal(bytes, &deployment)
	if err != nil {
		return nil, err
	}
	podTemplateSpec := podConfig.injectPodTemplateSpec(&deployment.Spec.Template)
	return enhancedDeployment{
		&deployment,
		enhancedDeploymentSpec{
			&deployment.Spec,
			podTemplateSpec,
		},
	}, nil
}

/* Given a byte slice representing a replication controller, unmarshal the
 * replication controller and return a new replication controller with the
 * sidecar and init-container injected.
 */
func injectReplicationController(bytes []byte, podConfig PodConfig) (interface{}, error) {
	var rc v1.ReplicationController
	err := yaml.Unmarshal(bytes, &rc)
	if err != nil {
		return nil, err
	}
	podTemplateSpec := podConfig.injectPodTemplateSpec(rc.Spec.Template)
	return enhancedReplicationController{
		&rc,
		enhancedReplicationControllerSpec{
			&rc.Spec,
			podTemplateSpec,
		},
	}, nil
}

/* Given a byte slice representing a replica set, unmarshal the replica set and
 * return a new replica set with the sidecar and init-container injected.
 */
func injectReplicaSet(bytes []byte, podConfig PodConfig) (interface{}, error) {
	var rs v1beta1.ReplicaSet
	err := yaml.Unmarshal(bytes, &rs)
	if err != nil {
		return nil, err
	}
	podTemplateSpec := podConfig.injectPodTemplateSpec(&rs.Spec.Template)
	return enhancedReplicaSet{
		&rs,
		enhancedReplicaSetSpec{
			&rs.Spec,
			podTemplateSpec,
		},
	}, nil
}

/* Given a byte slice representing a job, unmarshal the job and return a new job
 * with the sidecar and init-container injected.
 */
func injectJob(bytes []byte, podConfig PodConfig) (interface{}, error) {
	var job batchV1.Job
	err := yaml.Unmarshal(bytes, &job)
	if err != nil {
		return nil, err
	}
	podTemplateSpec := podConfig.injectPodTemplateSpec(&job.Spec.Template)
	return enhancedJob{
		&job,
		enhancedJobSpec{
			&job.Spec,
			podTemplateSpec,
		},
	}, nil
}

/* Given a byte slice representing a daemonset, unmarshal the daemonset and
 * return a new daemonset with the sidecar and init-container injected.
 */
func injectDaemonSet(bytes []byte, podConfig PodConfig) (interface{}, error) {
	var ds v1beta1.DaemonSet
	err := yaml.Unmarshal(bytes, &ds)
	if err != nil {
		return nil, err
	}
	podTemplateSpec := podConfig.injectPodTemplateSpec(&ds.Spec.Template)
	return enhancedDaemonSet{
		&ds,
		enhancedDaemonSetSpec{
			&ds.Spec,
			podTemplateSpec,
		},
	}, nil
}

/* Given a PodTemplateSpec, return a new PodTemplateSpec with the sidecar
 * and init-container injected.
 */
func (p PodConfig) injectPodTemplateSpec(t *v1.PodTemplateSpec) enhancedPodTemplateSpec {
	f := false
	inboundSkipPorts := append(p.IgnoreInboundPorts, p.ProxyControlPort)
	inboundSkipPortsStr := make([]string, len(inboundSkipPorts))
	for i, p := range inboundSkipPorts {
		inboundSkipPortsStr[i] = strconv.Itoa(int(p))
	}

	outboundSkipPortsStr := make([]string, len(p.IgnoreOutboundPorts))
	for i, p := range p.IgnoreOutboundPorts {
		outboundSkipPortsStr[i] = strconv.Itoa(int(p))
	}

	initArgs := []string{
		"--incoming-proxy-port", fmt.Sprintf("%d", p.InboundPort),
		"--outgoing-proxy-port", fmt.Sprintf("%d", p.OutboundPort),
		"--proxy-uid", fmt.Sprintf("%d", p.ProxyUID),
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
		Image:           fmt.Sprintf("%s:%s", p.InitImage, p.ConduitVersion),
		ImagePullPolicy: v1.PullPolicy(p.ImagePullPolicy),
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
		Image:           fmt.Sprintf("%s:%s", p.ProxyImage, p.ConduitVersion),
		ImagePullPolicy: v1.PullPolicy(p.ImagePullPolicy),
		SecurityContext: &v1.SecurityContext{
			RunAsUser: &p.ProxyUID,
		},
		Ports: []v1.ContainerPort{
			v1.ContainerPort{
				Name:          "conduit-proxy",
				ContainerPort: int32(p.InboundPort),
			},
		},
		Env: []v1.EnvVar{
			v1.EnvVar{Name: "CONDUIT_PROXY_LOG", Value: p.ProxyLogLevel},
			v1.EnvVar{
				Name:  "CONDUIT_PROXY_CONTROL_URL",
				Value: fmt.Sprintf("tcp://proxy-api.%s.svc.cluster.local:%d", p.ControlPlaneNamespace, p.ProxyAPIPort),
			},
			v1.EnvVar{Name: "CONDUIT_PROXY_CONTROL_LISTENER", Value: fmt.Sprintf("tcp://0.0.0.0:%d", p.ProxyControlPort)},
			v1.EnvVar{Name: "CONDUIT_PROXY_PRIVATE_LISTENER", Value: fmt.Sprintf("tcp://127.0.0.1:%d", p.OutboundPort)},
			v1.EnvVar{Name: "CONDUIT_PROXY_PUBLIC_LISTENER", Value: fmt.Sprintf("tcp://0.0.0.0:%d", p.InboundPort)},
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
	t.Annotations[k8s.ProxyVersionAnnotation] = p.ConduitVersion

	if t.Labels == nil {
		t.Labels = make(map[string]string)
	}
	t.Labels[k8s.ControllerNSLabel] = p.ControlPlaneNamespace
	t.Spec.Containers = append(t.Spec.Containers, sidecar)
	return enhancedPodTemplateSpec{
		t,
		enhancedPodSpec{
			&t.Spec,
			append(t.Spec.InitContainers, initContainer),
		},
	}
}
