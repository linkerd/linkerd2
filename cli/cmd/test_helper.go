package cmd

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/sergi/go-diff/diffmatchpatch"
)

var updateFixtures bool

func diffCompare(t *testing.T, actual string, expected string) {
	if actual != expected {
		dmp := diffmatchpatch.New()
		diffs := dmp.DiffMain(expected, actual, true)

		// colorized output for local testing
		t.Fatalf("Unexpected output:\n%+v", dmp.DiffPrettyText(diffs))

		diffs = dmp.DiffCleanupSemantic(diffs)
		patches := dmp.PatchMake(diffs)
		patchText := dmp.PatchToText(patches)
		t.Fatalf("Unexpected output:\n%+v", patchText)
	}
}

// Attempts to read a file and return the contents of that file as a string.
// readOptionalTestFile returns an empty string if the file name parameter being passed
// in is an empty string.
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

// Attempts to read a file and return the contents of that file as a string.
// readOptionalTestFile returns an empty string if the file name parameter being passed
// in is an empty string.
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
