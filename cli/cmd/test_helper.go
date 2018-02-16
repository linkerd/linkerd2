package cmd

import (
	"testing"

	"github.com/sergi/go-diff/diffmatchpatch"
)

func diffCompare(t *testing.T, actual string, expected string) {
	if actual != expected {
		dmp := diffmatchpatch.New()
		diffs := dmp.DiffMain(actual, expected, true)
		patches := dmp.PatchMake(expected, diffs)
		patchText := dmp.PatchToText(patches)
		t.Fatalf("Unexpected output:\n%+v", patchText)

	}
}
