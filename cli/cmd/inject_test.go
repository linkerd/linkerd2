package cmd

import (
	"bufio"
	"bytes"
	"io/ioutil"
	"os"
	"testing"

	"github.com/sergi/go-diff/diffmatchpatch"
)

func TestInjectYAML(t *testing.T) {
	t.Run("Run successful conduit inject on valid k8s yaml", func(t *testing.T) {
		file, err := os.Open("testdata/inject_emojivoto_deployment.input.yml")
		if err != nil {
			t.Errorf("error opening test file: %v\n", err)
		}

		goldenFileBytes, err := ioutil.ReadFile("testdata/inject_emojivoto_deployment.golden.yml")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		expectedContent := string(goldenFileBytes)

		read := bufio.NewReader(file)

		output := new(bytes.Buffer)

		err = InjectYAML(read, output)
		if err != nil {
			t.Errorf("Unexpected error injecting YAML: %v\n", err)
		}

		actualOutput := output.String()

		if actualOutput != expectedContent {
			dmp := diffmatchpatch.New()
			diffs := dmp.DiffMain(actualOutput, expectedContent, true)
			patches := dmp.PatchMake(expectedContent, diffs)
			patchText := dmp.PatchToText(patches)
			t.Fatalf("Unexpected output:\n%+v", patchText)
		}
	})
}
