package shell

import (
	"testing"
	"strings"
	"io/ioutil"
	"time"
)

func TestCombinedOutput(t *testing.T) {
	t.Run("Executes command and returns result without error if return code 0", func(t *testing.T) {
		expectedOutput := "expected"
		output, err := MakeUnixShell().CombinedOutput("echo", expectedOutput)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if strings.TrimSpace(output) != expectedOutput {
			t.Fatalf("Expecting command output to be [%s], got [%s]", expectedOutput, output)
		}
	})

	t.Run("Executes command and returns result and  error if return code>0", func(t *testing.T) {
		_, err := MakeUnixShell().CombinedOutput("command-that-doesnt", "--exist")

		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}
	})
}

func TestAsyncStdout(t *testing.T) {
	t.Run("Executes command and returns result without error if return code 0", func(t *testing.T) {
		expectedOutput := "expected"
		output, asyncError := MakeUnixShell().AsyncStdout("echo", expectedOutput)


		outputBytes, err := ioutil.ReadAll(output)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if strings.TrimSpace(string(outputBytes)) != expectedOutput {
			t.Fatalf("Expecting command output to be [%s], got [%s]", expectedOutput, output)
		}

		select {
		case e := <-asyncError:
			if e != nil {
				t.Fatalf("Unexpected error from the async process: %v", err)
			}
		}
	})

	t.Run("Executes command and returns result and error if did not find expected character", func(t *testing.T) {
		out, asyncError := MakeUnixShell().AsyncStdout("command-that-doesnt", "--exist")
		select {
		case err := <-asyncError:
			if err == nil {
				outputBytes, _ := ioutil.ReadAll(out)
				t.Fatalf("Expecting error, got nothing. Output: [%s]", string(outputBytes))
			}
		}
	})
}

func TestWaitForCharacter(t *testing.T) {

	t.Run("Executes command and returns result without error if return code 0", func(t *testing.T) {
		shell := MakeUnixShell()
		expectedOutput := "expected>"
		output, asyncError := shell.AsyncStdout("echo", expectedOutput)


		outputString, err := shell.WaitForCharacter('>', output, 100*time.Millisecond)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if strings.TrimSpace(outputString) != expectedOutput {
			t.Fatalf("Expecting command output to be [%s], got [%s]", expectedOutput, output)
		}
		select {
		case e := <-asyncError:
			if e != nil {
				t.Fatalf("Unexpected error from the async process: %v", err)
			}
		}
	})

	t.Run("Executes command and returns timeout error if expected character never shows up in output", func(t *testing.T) {
		shell := MakeUnixShell()
		output, asyncError := shell.AsyncStdout("tail", "-f", "/dev/random")

		outputString, err := shell.WaitForCharacter('!', output, 100*time.Millisecond)
		if err != nil {
			t.Fatalf("Expecting error, got nothing. output was [%s]", outputString)
		}
		select {
		case e := <-asyncError:
			if e != nil {
				t.Fatalf("Unexpected error from the async process: %v", err)
			}
		}
	})
}
