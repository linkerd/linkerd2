package main

import (
	"os"

	"github.com/linkerd/linkerd2/proxy-init/cmd"
)

func main() {
	if err := cmd.NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
