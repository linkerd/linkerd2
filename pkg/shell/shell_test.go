package shell

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestCombinedOutput(t *testing.T) {
	t.Run("Executes command and returns result without error if return code 0", func(t *testing.T) {
		expectedOutput := "expected"
		output, err := NewUnixShell().CombinedOutput("echo", expectedOutput)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if strings.TrimSpace(output) != expectedOutput {
			t.Fatalf("Expecting command output to be [%s], got [%s]", expectedOutput, output)
		}
	})

	t.Run("Executes command and returns result and  error if return code>0", func(t *testing.T) {
		_, err := NewUnixShell().CombinedOutput("command-that-doesnt", "--exist")

		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}
	})
}

func TestHomeDir(t *testing.T) {
	t.Run("Home dir for non-Windows boxes follow a common pattern", func(t *testing.T) {
		shell := NewUnixShell()
		home := shell.HomeDir()
		expected := os.Getenv("HOME")
		if runtime.GOOS != "windows" && !strings.Contains(home, expected) {
			t.Errorf("This is a UNIX-like system, expecting home dir [%s] to contain [%s]", home, expected)
		}
	})
}
