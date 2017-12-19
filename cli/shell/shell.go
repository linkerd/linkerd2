package shell

import (
	"os/exec"
	"bufio"
	"time"
	"fmt"
	"io"
)

type Shell interface {
	CombinedOutput(name string, arg ...string) (string, error)
	AsyncStdout(potentialErrorFromAsyncProcess chan error, name string, arg ...string) (*bufio.Reader, error)
	WaitForCharacter(charToWaitFor byte, output *bufio.Reader, timeout time.Duration) (string, error)
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
		return nil, fmt.Errorf("Error executing command in an async way: %v", err)
	}

	go func(e chan error) { e <- command.Run() }(potentialErrorFromAsyncProcess)
	return bufio.NewReader(stdout), nil
}

func (sh *unixShell) WaitForCharacter(charToWaitFor byte, outputReader *bufio.Reader, timeout time.Duration) (string, error) {
	output := make(chan string, 1)
	potentialError := make(chan error, 1)

	go func(output chan string, e chan error) {
		outputString, err := outputReader.ReadString(charToWaitFor)
		if err != nil {
			if err == io.EOF {
				e <- fmt.Errorf("Reached end of stream while waiting for character [%c] in output [%s] of command: %v", charToWaitFor, outputString, err)
			} else {
				e <- fmt.Errorf("Error while reading output from command: %v", err)
			}
		}

		output <- outputString
	}(output, potentialError)

	select {
	case <-time.After(timeout):
		return "", fmt.Errorf("Timed-out expoecting token [%c] in reader [%v]", charToWaitFor, outputReader)
	case e := <-potentialError:
		return "", e
	case o := <-output:
		return o, nil
	}

}

func MakeUnixShell() Shell {
	return &unixShell{}
}
