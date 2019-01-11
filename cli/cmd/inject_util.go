package cmd

import (
	"io"
	"os"
	"path/filepath"

	"k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type resourceTransformer interface {
	transform([]byte, *injectOptions) ([]byte, []injectReport, error)
	generateReport([]injectReport, io.Writer)
}

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
	dnsNameOverride string
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

func getFiller(text string) string {
	filler := ""
	for i := 0; i < lineWidth-len(text)-len(okStatus)-len("\n"); i++ {
		filler = filler + "."
	}

	return filler
}
