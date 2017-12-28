package cmd

import (
	"strings"
	"testing"
)

func TestCompletion(t *testing.T) {
	t.Run("Returns completion code", func(t *testing.T) {

		bash, err := getCompletion("bash")
		if err != nil {
			t.Fatalf("Unexpected error: %+v", err)
		}

		zsh, err := getCompletion("zsh")
		if err != nil {
			t.Fatalf("Unexpected error: %+v", err)
		}

		if !strings.Contains(bash, "# bash completion for conduit") {
			t.Fatalf("Unexpected bash output: %+v", bash)
		}

		if !strings.Contains(zsh, "#compdef conduit") {
			t.Fatalf("Unexpected zsh output: %+v", zsh)
		}
	})

	t.Run("Fails with invalid shell type", func(t *testing.T) {
		out, err := getCompletion("foo")
		if err == nil {
			t.Fatalf("Unexpected success for invalid shell type: %+v", out)
		}
	})
}
