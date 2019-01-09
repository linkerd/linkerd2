package cmd

import (
	"bytes"
	"errors"
	"testing"

	"github.com/fatih/color"
	"github.com/wercker/stern/stern"
)

func assertConfig(actual, expected *stern.Config) bool {
	return actual.Template != nil &&
		actual.ContainerState == expected.ContainerState &&
		actual.PodQuery != nil
}
func TestNewSternConfig(t *testing.T) {
	t.Run("Default log template", func(t *testing.T) {
		flags := &logFlags{containerState:"running"}
		testColor := color.New(color.FgHiRed)
		expectedLogLine := "linkerd-controller-xyz web-container Starting server on port 8084"
		buf := bytes.Buffer{}
		c, err := flags.NewSternConfig()

		if err != nil {
			t.Fatalf("Error creating stern config: %s", err.Error())
		}

		err = c.Template.Execute(&buf, &stern.Log{
			Message:        "Starting server on port 8084",
			Namespace:      "linkerd",
			PodName:        "linkerd-controller-xyz",
			ContainerName:  "web-container",
			PodColor:       testColor,
			ContainerColor: testColor,
		})

		if err != nil{
			t.Fatalf("Unexpected error: %s", err.Error())
		}

		if buf.String() != expectedLogLine {
			t.Fatalf("Invalid log line format\n got: %s\n expected:%s\n", buf.String(), expectedLogLine)
		}
	})
	
	t.Run("label selector creation", func(t *testing.T) {
		testCases := []struct{
			labelStr string
			expectedLabelSelector string
			expectedErr error
		}{
			{labelStr:"", expectedLabelSelector:""},
			{labelStr:"app=frontend", expectedLabelSelector:"app=frontend"},
			{labelStr:"=app=invalid", expectedLabelSelector:"", expectedErr: errors.New("found '=', expected: !, identifier, or 'end of string'")},
		}

		for _, tt := range testCases{
			flags := logFlags{
				labelSelector: tt.labelStr,
				containerState: "running",
			}
			c, err := flags.NewSternConfig()
			if err != nil && err.Error() != tt.expectedErr.Error() {
				t.Fatalf("Unexpected error: %s", err.Error())
			}

			if c != nil && c.LabelSelector.String() != tt.expectedLabelSelector{
				t.Fatalf("Error creating label selector: %s", c.LabelSelector.String())
			}
		}
	})
}
