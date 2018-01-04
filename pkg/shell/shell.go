package shell

import (
	"bufio"
	"fmt"
	"io"

	"os"
	"os/exec"
	"runtime"
	"time"
)

type Shell interface {
	CombinedOutput(name string, arg ...string) (string, error)
	AsyncStdout(potentialErrorFromAsyncProcess chan error, name string, arg ...string) (*bufio.Reader, error)
	WaitForCharacter(charToWaitFor byte, output *bufio.Reader, timeout time.Duration) (string, error)
	HomeDir() string
	Path() string
}

type unixShell struct{}

func (sh *unixShell) CombinedOutput(name string, arg ...string) (string, error) {
	command := exec.Command(name, arg...)
	bytes, err := command.CombinedOutput()
	if err != nil {
		return string(bytes), err
	}

	return string(bytes), nil
}

func (sh *unixShell) AsyncStdout(potentialErrorFromAsyncProcess chan error, name string, arg ...string) (*bufio.Reader, error) {
	command := exec.Command(name, arg...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("error executing command in an async way: %v", err)
	}

	reader := bufio.NewReader(stdout)

	go func(e chan error) { e <- command.Run() }(potentialErrorFromAsyncProcess)

	return reader, nil
}

func (sh *unixShell) WaitForCharacter(charToWaitFor byte, outputReader *bufio.Reader, timeout time.Duration) (string, error) {
	output := make(chan string, 1)
	potentialError := make(chan error, 1)

	go func(output chan string, e chan error) {
		outputString, err := outputReader.ReadString(charToWaitFor)
		if err != nil {
			if err == io.EOF {
				e <- fmt.Errorf("reached end of stream while waiting for character [%c] in output [%s] of command: %v", charToWaitFor, outputString, err)
			} else {
				e <- fmt.Errorf("error while reading output from command: %v", err)
			}
		}

		output <- outputString
	}(output, potentialError)

	select {
	case <-time.After(timeout):
		return "", fmt.Errorf("timed-out expoecting token [%c] in reader", charToWaitFor)
	case e := <-potentialError:
		return "", e
	case o := <-output:
		return o, nil
	}

}

func (sh *unixShell) HomeDir() string {
	var homeEnvVar string
	if runtime.GOOS == "windows" {
		homeEnvVar = "USERPROFILE"
	} else {
		homeEnvVar = "HOME"
	}
	return os.Getenv(homeEnvVar)
}

func (sh *unixShell) Path() string {
	var pathEnvVar string
	if runtime.GOOS == "windows" {
		pathEnvVar = "Path"
	} else {
		pathEnvVar = "PATH"
	}
	return os.Getenv(pathEnvVar)
}

func NewUnixShell() Shell {
	return &unixShell{}
}
