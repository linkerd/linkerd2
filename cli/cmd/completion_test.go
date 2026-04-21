package cmd

import (
	"testing"
)

func TestCompletion(t *testing.T) {
	rootCmd := NewRootCmd()
	t.Run("Returns completion code", func(t *testing.T) {

		_, err := getCompletion("bash", rootCmd)
		if err != nil {
			t.Fatalf("Unexpected error: %+v", err)
		}

		_, err = getCompletion("zsh", rootCmd)
		if err != nil {
			t.Fatalf("Unexpected error: %+v", err)
		}
	})

	t.Run("Fails with invalid shell type", func(t *testing.T) {
		out, err := getCompletion("foo", rootCmd)
		if err == nil {
			t.Fatalf("Unexpected success for invalid shell type: %+v", out)
		}
	})
}
