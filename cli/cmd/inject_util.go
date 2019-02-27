package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/inject"
	yamlDecoder "k8s.io/apimachinery/pkg/util/yaml"
)

type configs struct {
	global *config.Global
	proxy  *config.Proxy
}

type resourceTransformer interface {
	transform([]byte) ([]byte, []inject.Report, error)
	generateReport([]inject.Report, io.Writer)
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

		result, irs, err := rt.transform(bytes)
		if err != nil {
			return err
		}

		out.Write(result)
		out.Write([]byte("---\n"))

		reports = append(reports, irs...)
	}

	rt.generateReport(reports, report)

	return nil
}

// TODO: Temporarily disabled processing Lists of resources because the recursion was too hard
// to refactor. I'll come back to this later.
/*func processList(b []byte, options *injectOptions, rt resourceTransformer) ([]byte, []inject.InjectReport, error) {
	var sourceList v1.List
	if err := yaml.Unmarshal(b, &sourceList); err != nil {
		return nil, nil, err
	}

	injectReports := []inject.InjectReport{}
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
}*/

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
