package cmd

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/sergi/go-diff/diffmatchpatch"
)

var (
	// updateFixtures is set by the `-update` flag.
	updateFixtures bool

	// prettyDiff is set by the `-verbose-diff` flag.
	prettyDiff bool
)

// readTesdtata reads a file and return the contents of that file as a string.
func readTestdata(t *testing.T, fileName string) string {
	file, err := os.Open(filepath.Join("testdata", fileName))
	if err != nil {
		t.Fatalf("Failed to open expected output file: %v", err)
	}

	fixture, err := ioutil.ReadAll(file)
	if err != nil {
		t.Fatalf("Failed to read expected output file: %v", err)
	}

	return string(fixture)
}

func writeTestdata(t *testing.T, fileName string, data []byte) {
	p := filepath.Join("testdata", fileName)
	if err := ioutil.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func testDiff(t *testing.T, path, actual string) {
	expected := readTestdata(t, path)
	if actual == expected {
		return
	}

	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(expected, actual, true)
	diffs = dmp.DiffCleanupSemantic(diffs)
	var diff string
	if prettyDiff {
		diff = dmp.DiffPrettyText(diffs)
	} else {
		diff = dmp.PatchToText(dmp.PatchMake(diffs))
	}
	t.Errorf("mismatch: %s\n%s", path, diff)

	if updateFixtures {
		writeTestdata(t, path, []byte(actual))
	}
}
