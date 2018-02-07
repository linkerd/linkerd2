package shell

import (
	"os"
	"os/exec"
	"runtime"
)

type Shell interface {
	CombinedOutput(name string, arg ...string) (string, error)
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
