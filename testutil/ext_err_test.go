package testutil

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	os.Setenv(envFlag, "true")
	os.Exit(m.Run())
}

func redirectStdout(t *testing.T) (*os.File, chan string) {
	origStdout := os.Stdout
	newStdout, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("error creating os.Pipe(): %s", pipeErr)
	}
	os.Stdout = w

	// retrieve the payload sent to newStdout in a separate goroutine
	// to avoid blocking
	outC := make(chan string)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, newStdout)
		outC <- buf.String()
	}()

	return origStdout, outC
}

func restoreStdout(outC chan string, origStdout *os.File) string {
	os.Stdout.Close()
	out := <-outC
	os.Stdout = origStdout
	return out
}

func TestExtError(t *testing.T) {
	msg := "This is an error"

	// redirect stdout temporarily to catch the Github annotation output
	origStdout, outC := redirectStdout(t)
	err := Error(msg)
	errStr := err.Error()
	out := restoreStdout(outC, origStdout)

	if errStr != msg {
		t.Fatalf("unexpected error message: %s", errStr)
	}
	if strings.TrimSpace(out) != "::error file=testutil/ext_err_test.go,line=49::This is an error" {
		t.Fatalf("unexpected stdout content: %s", out)
	}
}

func TestExtErrorfWithAnn(t *testing.T) {
	msgFormat := "This is a detailed error: %s"
	str := "foobar"
	msgDesc := "This is a generic error"

	// redirect stdout temporarily to catch the Github annotation output
	origStdout, outC := redirectStdout(t)
	err := Errorf(msgFormat, str).WithAnn(msgDesc)
	errStr := err.Error()
	out := restoreStdout(outC, origStdout)

	if errStr != fmt.Sprintf(msgFormat, str) {
		t.Fatalf("unexpected error message: %s", errStr)
	}
	if strings.TrimSpace(out) != "::error file=testutil/ext_err_test.go,line=68::This is a generic error" {
		t.Fatalf("unexpected stdout content: %s", out)
	}
}
