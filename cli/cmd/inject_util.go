package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	v1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type resourceTransformer func(bytes []byte, options *injectOptions) ([]byte, []injectReport, error)

type injectReport struct {
	name                string
	hostNetwork         bool
	sidecar             bool
	udp                 bool // true if any port in any container has `protocol: UDP`
	unsupportedResource bool
}

type resourceConfig struct {
	obj             interface{}
	om              objMeta
	meta            metaV1.TypeMeta
	podSpec         *v1.PodSpec
	objectMeta      *metaV1.ObjectMeta
	DNSNameOverride string
	k8sLabels       map[string]string
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

func generateReport(injectReports []injectReport, output io.Writer) {

	injected := []string{}
	hostNetwork := []string{}
	sidecar := []string{}
	udp := []string{}

	for _, r := range injectReports {
		if !r.hostNetwork && !r.sidecar && !r.unsupportedResource {
			injected = append(injected, r.name)
		}

		if r.hostNetwork {
			hostNetwork = append(hostNetwork, r.name)
		}

		if r.sidecar {
			sidecar = append(sidecar, r.name)
		}

		if r.udp {
			udp = append(udp, r.name)
		}
	}

	//
	// Warnings
	//

	// leading newline to separate from yaml output on stdout
	output.Write([]byte("\n"))

	hostNetworkPrefix := fmt.Sprintf("%s%s", hostNetworkDesc, getFiller(hostNetworkDesc))
	if len(hostNetwork) == 0 {
		output.Write([]byte(fmt.Sprintf("%s%s\n", hostNetworkPrefix, okStatus)))
	} else {
		output.Write([]byte(fmt.Sprintf("%s%s -- \"hostNetwork: true\" detected in %s\n", hostNetworkPrefix, warnStatus, strings.Join(hostNetwork, ", "))))
	}

	sidecarPrefix := fmt.Sprintf("%s%s", sidecarDesc, getFiller(sidecarDesc))
	if len(sidecar) == 0 {
		output.Write([]byte(fmt.Sprintf("%s%s\n", sidecarPrefix, okStatus)))
	} else {
		output.Write([]byte(fmt.Sprintf("%s%s -- known sidecar detected in %s\n", sidecarPrefix, warnStatus, strings.Join(sidecar, ", "))))
	}

	unsupportedPrefix := fmt.Sprintf("%s%s", unsupportedDesc, getFiller(unsupportedDesc))
	if len(injected) > 0 {
		output.Write([]byte(fmt.Sprintf("%s%s\n", unsupportedPrefix, okStatus)))
	} else {
		output.Write([]byte(fmt.Sprintf("%s%s -- no supported objects found\n", unsupportedPrefix, warnStatus)))
	}

	udpPrefix := fmt.Sprintf("%s%s", udpDesc, getFiller(udpDesc))
	if len(udp) == 0 {
		output.Write([]byte(fmt.Sprintf("%s%s\n", udpPrefix, okStatus)))
	} else {
		verb := "uses"
		if len(udp) > 1 {
			verb = "use"
		}
		output.Write([]byte(fmt.Sprintf("%s%s -- %s %s \"protocol: UDP\"\n", udpPrefix, warnStatus, strings.Join(udp, ", "), verb)))
	}

	//
	// Summary
	//

	// TODO: make message more generic (shared with uninject)
	summary := fmt.Sprintf("Summary: %d of %d YAML document(s) injected", len(injected), len(injectReports))
	output.Write([]byte(fmt.Sprintf("\n%s\n", summary)))

	for _, i := range injected {
		output.Write([]byte(fmt.Sprintf("  %s\n", i)))
	}

	// trailing newline to separate from kubectl output if piping
	output.Write([]byte("\n"))
}

func getFiller(text string) string {
	filler := ""
	for i := 0; i < lineWidth-len(text)-len(okStatus)-len("\n"); i++ {
		filler = filler + "."
	}

	return filler
}
