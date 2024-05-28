package testutil

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

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

func NewTestDataDiffer() TestDataDiffer {
	differ := TestDataDiffer{
		RejectPath:     "",
		PrettyDiff:     false,
		UpdateFixtures: false,
	}

	prettyDiff := os.Getenv("LINKERD_TEST_PRETTY_DIFF") != ""

	flag.BoolVar(&differ.UpdateFixtures, "update", false, "update text fixtures in place")
	flag.BoolVar(&differ.PrettyDiff, "pretty-diff", prettyDiff, "display the full text when diffing")
	flag.StringVar(&differ.RejectPath, "reject-path", "", "write results for failed tests to this path (path is relative to the test location)")

	flag.Parse()

	return differ
}

// DiffTestFileHashes checks the SHA256 hashes of the specified go source file
// and its associated rendered values file with the "golden" hashes for each
func (td *TestDataDiffer) DiffTestFileHashes(state *testing.T, goFile string, renderedFile string, goldenHashes string) {

	fileHashes := make(map[string]string)
	decoder := yaml.NewDecoder(strings.NewReader(ReadTestdata(goldenHashes)))

	if err := decoder.Decode(&fileHashes); err != nil {
		if !errors.Is(err, io.EOF) {
			state.Fatal(err)
		}
	}

	sourceHash := getFileHash(state, goFile)
	renderedHash := getFileHash(state, renderedFile)

	if td.UpdateFixtures {

		fileHashes[goFile] = sourceHash
		fileHashes[renderedFile] = renderedHash

		buffer := bytes.NewBufferString("---\n")
		encoder := yaml.NewEncoder(buffer)

		if err := encoder.Encode(fileHashes); err != nil {
			state.Fatal("could not encode updated golden hash data", err)
		}

		writeTestdata(goldenHashes, buffer.Bytes())

		return
	}

	expectedSourceHash, hashFound := fileHashes[goFile]

	if !hashFound {
		state.Fatalf("Missing expected hash for go source file %s", goFile)
	}

	expectedRenderedHash, hashFound := fileHashes[renderedFile]

	if !hashFound {
		state.Fatalf("Missing expected hash for rendered values file %s", renderedFile)
	}

	errLog := ""

	if sourceHash != expectedSourceHash {
		errLog += fmt.Sprintf("Hash mismatch for go source file %s: [actual: %s expected: %s]\n", goFile, sourceHash, expectedSourceHash)
		fileHashes[goFile+".rejected"] = sourceHash
	}

	if renderedHash != expectedRenderedHash {
		errLog += fmt.Sprintf("Hash mismatch for rendered values file %s: [actual: %s expected: %s]", renderedFile, renderedHash, expectedRenderedHash)
		fileHashes[renderedFile+".rejected"] = renderedHash
	}

	if errLog != "" {

		if td.RejectPath != "" {

			buffer := bytes.NewBufferString("---\n")
			encoder := yaml.NewEncoder(buffer)

			if err := encoder.Encode(fileHashes); err != nil {
				state.Fatal("could not encode rejected hash data", err)
			}

			writeRejects(goldenHashes, buffer.Bytes(), td.RejectPath)
		}

		state.Fatal(errLog)
	}

}

// DiffTestYAML compares a YAML structure to a fixture on the filesystem.
func (td *TestDataDiffer) DiffTestYAML(path string, actualYAML string) error {
	expectedYAML := ReadTestdata(path)
	return td.diffTestYAML(path, actualYAML, expectedYAML)
}

// DiffTestYAMLTemplate compares a YAML structure to a parameterized fixture on the filesystem.
func (td *TestDataDiffer) DiffTestYAMLTemplate(path string, actualYAML string, params any) error {
	file := filepath.Join("testdata", path)
	t, err := template.New(path).ParseFiles(file)
	if err != nil {
		return fmt.Errorf("failed to read YAML template from %s: %w", path, err)
	}
	var buf bytes.Buffer
	err = t.Execute(&buf, params)
	if err != nil {
		return fmt.Errorf("failed to build YAML from template %s: %w", path, err)
	}
	return td.diffTestYAML(path, actualYAML, buf.String())
}

func (td *TestDataDiffer) diffTestYAML(path, actualYAML, expectedYAML string) error {
	actual, err := unmarshalYAML([]byte(actualYAML))
	if err != nil {
		return fmt.Errorf("failed to unmarshal generated YAML: %w", err)
	}
	expected, err := unmarshalYAML([]byte(expectedYAML))
	if err != nil {
		return fmt.Errorf("failed to unmarshal expected YAML: %w", err)
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
	t.Helper()
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
		panic(fmt.Sprintf("failed to open expected output file: %v", err))
	}

	fixture, err := io.ReadAll(file)
	if err != nil {
		panic(fmt.Sprintf("failed to read expected output file: %v", err))
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

func getFileHash(state *testing.T, target string) string {
	if !path.IsAbs(target) {
		var err error
		target, err = filepath.Abs(target)

		if err != nil {
			state.Fatal(err)
		}
	}

	data, err := os.Open(target)

	if err != nil {
		state.Fatal(err)
	}

	defer func(f *os.File) {
		err := f.Close()

		if err != nil {
			state.Fatal(err)
		}

	}(data)

	fileHash := sha256.New()

	if _, err := io.Copy(fileHash, data); err != nil {
		state.Fatal(err)
	}

	return fmt.Sprintf("%x", fileHash.Sum(nil))
}
