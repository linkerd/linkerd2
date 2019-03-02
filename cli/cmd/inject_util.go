package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	yamlDecoder "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	pb "github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/inject"
	"github.com/linkerd/linkerd2/pkg/k8s"
)

type configs struct {
	global *pb.Global
	proxy  *pb.Proxy
}

type resourceTransformer interface {
	transform([]byte) ([]byte, []inject.Report, error)
	generateReport([]inject.Report, io.Writer)
}

// fetchConfigsFromK8s uses the CLI's global configuration to fetch linkerd
// configuration from Kubernetes.
func fetchConfigsFromK8s() (configs, error) {
	api, err := k8s.NewAPI(kubeconfigPath, kubeContext)
	if err != nil {
		return configs{}, err
	}

	k, err := kubernetes.NewForConfig(api.Config)
	if err != nil {
		return configs{}, err
	}

	global, proxy, err := config.Fetch(k.CoreV1().ConfigMaps(controlPlaneNamespace))
	return configs{global, proxy}, err
}

// TODO: this is just a temporary function to convert command-line options to GlobalConfig
// and ProxyConfig, until we come up with an abstraction over those GRPC structs
func (c *configs) overrideFromOptions(options *injectOptions) {
	if c == nil {
		newc := newConfig()
		c = &newc
	}

	if c.global == nil {
		c.global = &pb.Global{}
	}

	if c.global.Version == "" {
		c.global.Version = options.linkerdVersion
	}

	c.global.LinkerdNamespace = controlPlaneNamespace
	c.global.CniEnabled = c.global.CniEnabled || options.noInitContainer

	if options.tls != optionalTLS {
		c.global.IdentityContext = nil
	}

	if c.proxy == nil {
		c.proxy = &pb.Proxy{}
	}
	if len(options.ignoreInboundPorts) > 0 {
		c.proxy.IgnoreInboundPorts = []*pb.Port{}
		for _, port := range options.ignoreInboundPorts {
			c.proxy.IgnoreInboundPorts = append(c.proxy.IgnoreInboundPorts, &pb.Port{Port: uint32(port)})
		}
	}
	if len(options.ignoreOutboundPorts) > 0 {
		c.proxy.IgnoreOutboundPorts = []*pb.Port{}
		for _, port := range options.ignoreOutboundPorts {
			c.proxy.IgnoreOutboundPorts = append(c.proxy.IgnoreOutboundPorts, &pb.Port{Port: uint32(port)})
		}
	}

	if c.proxy.ProxyImage == nil {
		c.proxy.ProxyImage = &pb.Image{}
	}
	if options.proxyImage != "" && options.dockerRegistry != "" {
		c.proxy.ProxyImage.ImageName = registryOverride(options.proxyImage, options.dockerRegistry)
	}
	if options.imagePullPolicy != "" {
		c.proxy.ProxyImage.PullPolicy = options.imagePullPolicy
	}

	if options.initImage != "" && options.dockerRegistry != "" {
		c.proxy.ProxyInitImage.ImageName = registryOverride(options.initImage, options.dockerRegistry)
	}
	if options.imagePullPolicy != "" {
		c.proxy.ProxyInitImage.PullPolicy = options.imagePullPolicy
	}

	if options.proxyControlPort != 0 {
		c.proxy.ControlPort = &pb.Port{Port: uint32(options.proxyControlPort)}
	}
	if options.inboundPort != 0 {
		c.proxy.InboundPort = &pb.Port{Port: uint32(options.inboundPort)}
	}
	if options.outboundPort != 0 {
		c.proxy.OutboundPort = &pb.Port{Port: uint32(options.outboundPort)}
	}
	if options.proxyMetricsPort != 0 {
		c.proxy.MetricsPort = &pb.Port{Port: uint32(options.proxyMetricsPort)}
	}

	if options.proxyUID != 0 {
		c.proxy.ProxyUid = options.proxyUID
	}
	if options.proxyLogLevel != "" {
		c.proxy.LogLevel = &pb.LogLevel{Level: options.proxyLogLevel}
	}
	if options.disableExternalProfiles {
		c.proxy.DisableExternalProfiles = options.disableExternalProfiles
	}

	if options.proxyCPURequest != "" || options.proxyCPULimit != "" ||
		options.proxyMemoryRequest != "" || options.proxyMemoryLimit != "" {
		if c.proxy.Resource == nil {
			c.proxy.Resource = &pb.ResourceRequirements{}
		}
		if options.proxyCPURequest != "" {
			c.proxy.Resource.RequestCpu = options.proxyCPURequest
		}
		if options.proxyCPULimit != "" {
			c.proxy.Resource.LimitCpu = options.proxyCPULimit
		}
		if options.proxyMemoryRequest != "" {
			c.proxy.Resource.RequestMemory = options.proxyMemoryRequest
		}
		if options.proxyMemoryLimit != "" {
			c.proxy.Resource.LimitMemory = options.proxyMemoryLimit
		}
	}
}

// Returns the integer representation of os.Exit code; 0 on success and 1 on failure.
func transformInput(inputs []io.Reader, errWriter, outWriter io.Writer, rt resourceTransformer) int {
	postInjectBuf := &bytes.Buffer{}
	reportBuf := &bytes.Buffer{}

	for _, input := range inputs {
		err := processYAML(input, postInjectBuf, reportBuf, rt)
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

// processYAML takes an input stream of YAML, outputting injected/uninjected YAML to out.
func processYAML(in io.Reader, out io.Writer, report io.Writer, rt resourceTransformer) error {
	reader := yamlDecoder.NewYAMLReader(bufio.NewReaderSize(in, 4096))

	reports := []inject.Report{}

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

		var result []byte
		var irs []inject.Report

		isList, err := kindIsList(bytes)
		if err != nil {
			return err
		}
		if isList {
			result, irs, err = processList(bytes, rt)
		} else {
			result, irs, err = rt.transform(bytes)
		}
		if err != nil {
			return err
		}
		reports = append(reports, irs...)
		out.Write(result)
		out.Write([]byte("---\n"))
	}

	rt.generateReport(reports, report)

	return nil
}

func kindIsList(bytes []byte) (bool, error) {
	var meta metav1.TypeMeta
	if err := yaml.Unmarshal(bytes, &meta); err != nil {
		return false, err
	}
	return meta.Kind == "List", nil
}

func processList(bytes []byte, rt resourceTransformer) ([]byte, []inject.Report, error) {
	var sourceList corev1.List
	if err := yaml.Unmarshal(bytes, &sourceList); err != nil {
		return nil, nil, err
	}

	reports := []inject.Report{}
	items := []runtime.RawExtension{}

	for _, item := range sourceList.Items {
		result, irs, err := rt.transform(item.Raw)
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
		reports = append(reports, irs...)
	}

	sourceList.Items = items
	result, err := yaml.Marshal(sourceList)
	if err != nil {
		return nil, nil, err
	}
	return result, reports, nil
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
