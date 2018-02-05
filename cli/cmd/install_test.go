package cmd

import (
	"bytes"
	"io/ioutil"
	"testing"

	"github.com/sergi/go-diff/diffmatchpatch"
)

func TestRender(t *testing.T) {
	t.Run("Should render an install config", func(t *testing.T) {
		goldenFileBytes, err := ioutil.ReadFile("testdata/install_output.golden")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		expectedContent := string(goldenFileBytes)

		var buf bytes.Buffer

		config := installConfig{
			Namespace:                "Namespace",
			ControllerImage:          "ControllerImage",
			WebImage:                 "WebImage",
			PrometheusImage:          "PrometheusImage",
			ControllerReplicas:       1,
			WebReplicas:              2,
			PrometheusReplicas:       3,
			ImagePullPolicy:          "ImagePullPolicy",
			UUID:                     "UUID",
			CliVersion:               "CliVersion",
			ControllerLogLevel:       "ControllerLogLevel",
			ControllerComponentLabel: "ControllerComponentLabel",
			CreatedByAnnotation:      "CreatedByAnnotation",
		}

		err = render(config, &buf)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		content := buf.String()

		if content != expectedContent {
			dmp := diffmatchpatch.New()
			diffs := dmp.DiffMain(content, expectedContent, true)
			patches := dmp.PatchMake(expectedContent, diffs)
			patchText := dmp.PatchToText(patches)
			t.Fatalf("Unexpected output:\n%+v", patchText)
		}
	})
}
