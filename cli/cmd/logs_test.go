package cmd

import (
	"bytes"
	"fmt"
	"testing"
)


func TestRunLogOutput(t *testing.T) {
	var (
		tests []struct {
		}
	)
	for _, tt := range tests {
		fmt.Sprintf("%v", tt)
		t.Run("Log output", func(t *testing.T) {
			outBuffer := bytes.Buffer{}
			opts := &logCmdOpts{}
			err := runLogOutput(opts)
			fmt.Println(outBuffer.String())

			if err != nil {
				t.Fatalf("Unexpected error: %s", err.Error())
			}
		})

	}
}
