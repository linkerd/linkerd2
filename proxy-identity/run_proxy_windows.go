package main

import (
	"os"
	"os/exec"

	log "github.com/sirupsen/logrus"
)

func runProxy() {
	// The input arguments are static.
	//nolint:gosec
	cmd := exec.Command("C:\\usr\\lib\\linkerd\\linkerd2-proxy.exe")
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		log.Fatalf("Failed to run proxy: %s", err)
	}
}
