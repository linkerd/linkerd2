package cmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/sergi/go-diff/diffmatchpatch"
)

func diffCompare(t *testing.T, actual string, expected string) {
	if actual != expected {
		dmp := diffmatchpatch.New()
		diffs := dmp.DiffMain(expected, actual, true)
		patches := dmp.PatchMake(expected, diffs)
		patchText := dmp.PatchToText(patches)
		t.Fatalf("Unexpected output:\n%+v", patchText)
	}
}

func readGoldenTestFile(t *testing.T, testDirWithTrailingSlash string, goldenFileName string) string {
	var fileData string

	if goldenFileName != "" {
		testDirWithFileName := fmt.Sprintf("%s%s", testDirWithTrailingSlash, goldenFileName)
		file, err := os.Open(testDirWithFileName)
		if err != nil {
			t.Fatalf("Failed to open expected output file: %v", err)
		}

		goldenStdOutFile, err := ioutil.ReadAll(file)
		if err != nil {
			t.Fatalf("Failed to read expected output file: %v", err)
		}
		fileData = string(goldenStdOutFile)
	}

	return fileData
}
