package testutil

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// TestDataDiffer holds configuration for generating test diff
type TestDataDiffer struct {
	PrettyDiff     bool
	UpdateFixtures bool
	RejectPath     string
}

// DiffTestdata generates the diff for actual w.r.the file in path
func (td *TestDataDiffer) DiffTestdata(t *testing.T, path, actual string) {
	expected := ReadTestdata(t, path)
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

	if td.UpdateFixtures {
		writeTestdata(t, path, []byte(actual))
	}

	if td.RejectPath != "" {
		writeRejects(t, path, []byte(actual), td.RejectPath)
	}
}

// ReadTestdata reads a file and returns the contents of that file as a string.
func ReadTestdata(t *testing.T, fileName string) string {
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

func writeRejects(t *testing.T, origFileName string, data []byte, rejectPath string) {
	p := filepath.Join(rejectPath, origFileName+".rej")
	if err := ioutil.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
}
