//go:build !windows

package main

import (
	"os"
	"syscall"

	log "github.com/sirupsen/logrus"
)

func runProxy() {
	// The input arguments are static.
	//nolint:gosec
	err := syscall.Exec("/usr/lib/linkerd/linkerd2-proxy", []string{}, os.Environ())
	if err != nil {
		log.Fatalf("Failed to run proxy: %s", err)
	}
}
