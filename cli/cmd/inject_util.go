package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	appsV1 "k8s.io/api/apps/v1"
	batchV1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	yamlDecoder "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

type resourceTransformer interface {
	transform([]byte, *injectOptions) ([]byte, []injectReport, error)
	generateReport([]injectReport, io.Writer)
}

type injectReport struct {
	kind                string
	name                string
	hostNetwork         bool
	sidecar             bool
	udp                 bool // true if any port in any container has `protocol: UDP`
	unsupportedResource bool
	injectDisabled      bool
}

type resourceConfig struct {
	obj             interface{}
	om              objMeta
	meta            metaV1.TypeMeta
	podSpec         *v1.PodSpec
	objectMeta      *metaV1.ObjectMeta
	dnsNameOverride string
	k8sLabels       map[string]string
}

func (i injectReport) resName() string {
	return fmt.Sprintf("%s/%s", i.kind, i.name)
}

// Returns the integer representation of os.Exit code; 0 on success and 1 on failure.
func transformInput(inputs []io.Reader, errWriter, outWriter io.Writer, options *injectOptions, rt resourceTransformer) int {
	postInjectBuf := &bytes.Buffer{}
	reportBuf := &bytes.Buffer{}

	for _, input := range inputs {
		err := ProcessYAML(input, postInjectBuf, reportBuf, options, rt)
		if err != nil {
			fmt.Fprintf(errWriter, "Error transforming resources: %v\n", err)
			return 1
		}
		_, err = io.Copy(outWriter, postInjectBuf)

		// print error report after yaml output, for better visibility
		io.Copy(errWriter, reportBuf)

		if err != nil {
			fmt.Fprintf(errWriter, "Error printing YAML: %v\n", err)
			return 1
		}
	}
	return 0
}

// ProcessYAML takes an input stream of YAML, outputting injected/uninjected YAML to out.
func ProcessYAML(in io.Reader, out io.Writer, report io.Writer, options *injectOptions, rt resourceTransformer) error {
	reader := yamlDecoder.NewYAMLReader(bufio.NewReaderSize(in, 4096))

	injectReports := []injectReport{}

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

		result, irs, err := rt.transform(bytes, options)
		if err != nil {
			return err
		}

		out.Write(result)
		out.Write([]byte("---\n"))

		injectReports = append(injectReports, irs...)
	}

	rt.generateReport(injectReports, report)

	return nil
}

func processList(b []byte, options *injectOptions, rt resourceTransformer) ([]byte, []injectReport, error) {
	var sourceList v1.List
	if err := yaml.Unmarshal(b, &sourceList); err != nil {
		return nil, nil, err
	}

	injectReports := []injectReport{}
	items := []runtime.RawExtension{}

	for _, item := range sourceList.Items {
		result, reports, err := rt.transform(item.Raw, options)
		if err != nil {
			return nil, nil, err
		}

		// At this point, we have yaml. The kubernetes internal representation is
		// json. Because we're building a list from RawExtensions, the yaml needs
		// to be converted to json.
		injected, err := yaml.YAMLToJSON(result)
		if err != nil {
			return nil, nil, err
		}

		items = append(items, runtime.RawExtension{Raw: injected})
		injectReports = append(injectReports, reports...)
	}

	sourceList.Items = items
	result, err := yaml.Marshal(sourceList)
	if err != nil {
		return nil, nil, err
	}

	return result, injectReports, nil
}

func (conf *resourceConfig) parse(bytes []byte, options *injectOptions, rt resourceTransformer) ([]byte, []injectReport, error) {
	// The Kubernetes API is versioned and each version has an API modeled
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
	if err := yaml.Unmarshal(bytes, &conf.meta); err != nil {
		return nil, nil, err
	}

	// retrieve the `metadata/name` field for reporting later
	if err := yaml.Unmarshal(bytes, &conf.om); err != nil {
		return nil, nil, err
	}

	conf.k8sLabels = map[string]string{}

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
			return nil, nil, err
		}

		if deployment.Name == ControlPlanePodName && deployment.Namespace == controlPlaneNamespace {
			conf.dnsNameOverride = LocalhostDNSNameOverride
		}

		conf.obj = &deployment
		conf.k8sLabels[k8s.ProxyDeploymentLabel] = deployment.Name
		conf.podSpec = &deployment.Spec.Template.Spec
		conf.objectMeta = &deployment.Spec.Template.ObjectMeta

	case "ReplicationController":
		var rc v1.ReplicationController
		if err := yaml.Unmarshal(bytes, &rc); err != nil {
			return nil, nil, err
		}

		conf.obj = &rc
		conf.k8sLabels[k8s.ProxyReplicationControllerLabel] = rc.Name
		conf.podSpec = &rc.Spec.Template.Spec
		conf.objectMeta = &rc.Spec.Template.ObjectMeta

	case "ReplicaSet":
		var rs v1beta1.ReplicaSet
		if err := yaml.Unmarshal(bytes, &rs); err != nil {
			return nil, nil, err
		}

		conf.obj = &rs
		conf.k8sLabels[k8s.ProxyReplicaSetLabel] = rs.Name
		conf.podSpec = &rs.Spec.Template.Spec
		conf.objectMeta = &rs.Spec.Template.ObjectMeta

	case "Job":
		var job batchV1.Job
		if err := yaml.Unmarshal(bytes, &job); err != nil {
			return nil, nil, err
		}

		conf.obj = &job
		conf.k8sLabels[k8s.ProxyJobLabel] = job.Name
		conf.podSpec = &job.Spec.Template.Spec
		conf.objectMeta = &job.Spec.Template.ObjectMeta

	case "DaemonSet":
		var ds v1beta1.DaemonSet
		if err := yaml.Unmarshal(bytes, &ds); err != nil {
			return nil, nil, err
		}

		conf.obj = &ds
		conf.k8sLabels[k8s.ProxyDaemonSetLabel] = ds.Name
		conf.podSpec = &ds.Spec.Template.Spec
		conf.objectMeta = &ds.Spec.Template.ObjectMeta

	case "StatefulSet":
		var statefulset appsV1.StatefulSet
		if err := yaml.Unmarshal(bytes, &statefulset); err != nil {
			return nil, nil, err
		}

		conf.obj = &statefulset
		conf.k8sLabels[k8s.ProxyStatefulSetLabel] = statefulset.Name
		conf.podSpec = &statefulset.Spec.Template.Spec
		conf.objectMeta = &statefulset.Spec.Template.ObjectMeta

	case "Pod":
		var pod v1.Pod
		if err := yaml.Unmarshal(bytes, &pod); err != nil {
			return nil, nil, err
		}

		conf.obj = &pod
		conf.podSpec = &pod.Spec
		conf.objectMeta = &pod.ObjectMeta

	case "List":
		// Lists are a little different than the other types. There's no immediate
		// pod template. Because of this, we do a recursive call for each element
		// in the list (instead of just marshaling the injected pod template).
		return processList(bytes, options, rt)
	}

	return nil, nil, nil
}

// Read all the resource files found in path into a slice of readers.
// path can be either a file, directory or stdin.
func read(path string) ([]io.Reader, error) {
	var (
		in  []io.Reader
		err error
	)
	if path == "-" {
		in = append(in, os.Stdin)
	} else {
		in, err = walk(path)
		if err != nil {
			return nil, err
		}
	}

	return in, nil
}

// walk walks the file tree rooted at path. path may be a file or a directory.
// Creates a reader for each file found.
func walk(path string) ([]io.Reader, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if !stat.IsDir() {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}

		return []io.Reader{file}, nil
	}

	var in []io.Reader
	werr := filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}

		in = append(in, file)
		return nil
	})

	if werr != nil {
		return nil, werr
	}

	return in, nil
}

// updateReportAndCheck updates the report for the provided resources.
func (r *injectReport) update(m *metaV1.ObjectMeta, p *v1.PodSpec) {
	r.injectDisabled = injectDisabled(m)
	r.hostNetwork = p.HostNetwork
	r.sidecar = healthcheck.HasExistingSidecars(p)
	r.udp = checkUDPPorts(p)
}

// shouldInject returns false if the resource should not be injected.
//
// Injection is skipped in the following situations
// - Injection is disabled by annotation
// - Pods with `hostNetwork: true` share a network namespace with the host.
//   The init-container would destroy the iptables configuration on the host.
// - Known 3rd party sidecars already present.
func (r *injectReport) shouldInject() bool {
	return !r.injectDisabled && !r.hostNetwork && !r.sidecar
}
