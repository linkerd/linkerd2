package testutil

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-test/deep"
	"github.com/sergi/go-diff/diffmatchpatch"
	"gopkg.in/yaml.v2"
)

// TestDataDiffer holds configuration for generating test diff
type TestDataDiffer struct {
	PrettyDiff     bool
	UpdateFixtures bool
	RejectPath     string
}

// DiffTestYAML compares a YAML structure to a fixture on the filestystem.
func (td *TestDataDiffer) DiffTestYAML(path string, actualYAML string) error {
	actual, err := unmarshalYAML([]byte(actualYAML))
	if err != nil {
		return fmt.Errorf("Failed to unmarshal generated YAML: %w", err)
	}
	expected, err := unmarshalYAML([]byte(ReadTestdata(path)))
	if err != nil {
		return fmt.Errorf("Failed to unmarshal generated YAML from %s: %w", path, err)
	}
	diff := deep.Equal(expected, actual)
	if diff == nil {
		return nil
	}

	td.storeActual(path, []byte(actualYAML))

	e := fmt.Sprintf("YAML mismatches %s:", path)
	for _, d := range diff {
		e += fmt.Sprintf("\n	%s", d)
	}
	return errors.New(e)
}

// DiffTestdata generates the diff for actual w.r.the file in path
func (td *TestDataDiffer) DiffTestdata(t *testing.T, path, actual string) {
	expected := ReadTestdata(path)
	if actual == expected {
		return
	}
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(expected, actual, true)
	diffs = dmp.DiffCleanupSemantic(diffs)
	var diff string
	if td.PrettyDiff {
		diff = dmp.DiffPrettyText(diffs)
	} else {
		diff = dmp.PatchToText(dmp.PatchMake(diffs))
	}
	t.Errorf("mismatch: %s\n%s", path, diff)

	td.storeActual(path, []byte(actual))
}

func (td *TestDataDiffer) storeActual(path string, actual []byte) {
	if td.UpdateFixtures {
		writeTestdata(path, actual)
	}

	if td.RejectPath != "" {
		writeRejects(path, actual, td.RejectPath)
	}
}

// ReadTestdata reads a file and returns the contents of that file as a string.
func ReadTestdata(fileName string) string {
	file, err := os.Open(filepath.Join("testdata", fileName))
	if err != nil {
		panic(fmt.Sprintf("Failed to open expected output file: %v", err))
	}

	fixture, err := io.ReadAll(file)
	if err != nil {
		panic(fmt.Sprintf("Failed to read expected output file: %v", err))
	}

	return string(fixture)
}

func unmarshalYAML(data []byte) ([]interface{}, error) {
	objs := make([]interface{}, 0)
	rd := bytes.NewReader(data)
	decoder := yaml.NewDecoder(rd)
	for {
		var obj interface{}
		if err := decoder.Decode(&obj); err != nil {
			if errors.Is(err, io.EOF) {
				return objs, nil
			}
			return nil, err
		}

		objs = append(objs, obj)
	}
}

func writeTestdata(fileName string, data []byte) {
	p := filepath.Join("testdata", fileName)
	if err := os.WriteFile(p, data, 0600); err != nil {
		panic(err)
	}
}

func writeRejects(origFileName string, data []byte, rejectPath string) {
	p := filepath.Join(rejectPath, origFileName+".rej")
	if err := os.WriteFile(p, data, 0600); err != nil {
		panic(err)
	}
}
