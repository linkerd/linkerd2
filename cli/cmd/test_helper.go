package cmd

import (
	"flag"
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

// TestMain parses flags before running tests
func TestMain(m *testing.M) {
	flag.BoolVar(&updateFixtures, "update", false, "update text fixtures in place")
	prettyDiff = os.Getenv("LINKERD_TEST_PRETTY_DIFF") != ""
	flag.BoolVar(&prettyDiff, "pretty-diff", prettyDiff, "display the full text when diffing")
	flag.Parse()
	os.Exit(m.Run())
}

func diffCompare(t *testing.T, actual string, expected string) {
	if actual != expected {
		dmp := diffmatchpatch.New()
		diffs := dmp.DiffMain(expected, actual, true)
		diffs = dmp.DiffCleanupSemantic(diffs)

		var txt string
		if prettyDiff {
			txt = dmp.DiffPrettyText(diffs)
		} else {
			txt = dmp.PatchToText(dmp.PatchMake(diffs))
		}
		t.Errorf("mismatch:\n%s", txt)
	}
}

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

// readTestdataIfFileName returns an empty string if the file name parameter being passed
// in is an empty string, and otherwise calls readTestdata.
func readTestdataIfFileName(t *testing.T, fileName string) string {
	if fileName == "" {
		return ""
	}

	return readTestdata(t, fileName)
}

func writeTestdataIfUpdate(t *testing.T, fileName string, data []byte) {
	if updateFixtures {
		p := filepath.Join("testdata", fileName)
		if err := ioutil.WriteFile(p, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func diffCompareFile(t *testing.T, actual string, goldenFile string) {
	expectedOutput := readTestdata(t, goldenFile)
	diffCompare(t, actual, expectedOutput)
}
