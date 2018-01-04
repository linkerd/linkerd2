package shell

import (
	"bufio"
	"fmt"
	"strings"
	"time"
)

type MockShell struct {
	LastNameCalled string
	LastArgsCalled []string
	OutputToReturn []string
	ErrorToReturn  error
}

func (sh *MockShell) LastFullCommand() string {
	return fmt.Sprintf("%s %s", sh.LastNameCalled, strings.Join(sh.LastArgsCalled, " "))
}

func (sh *MockShell) pop() (string, error) {
	var outputPopped string
	if len(sh.OutputToReturn) != 0 {
		outputPopped, sh.OutputToReturn = sh.OutputToReturn[0], sh.OutputToReturn[1:]
	}

	return outputPopped, sh.ErrorToReturn
}

func (sh *MockShell) CombinedOutput(name string, arg ...string) (string, error) {
	sh.LastNameCalled = name
	sh.LastArgsCalled = arg

	return sh.pop()
}

func (sh *MockShell) AsyncStdout(asyncError chan error, name string, arg ...string) (*bufio.Reader, error) {
	sh.LastNameCalled = name
	sh.LastArgsCalled = arg

	outputToReturn, errToReturn := sh.pop()
	return bufio.NewReader(strings.NewReader(outputToReturn)), errToReturn
}

func (sh *MockShell) WaitForCharacter(charToWaitFor byte, outputReader *bufio.Reader, timeout time.Duration) (string, error) {
	return outputReader.ReadString(charToWaitFor)
}

func (sh *MockShell) HomeDir() string {
	return "/home/bob"
}

func (sh *MockShell) Path() string {
	return "/bin/fake"
}
