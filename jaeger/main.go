package main

import (
	"os"

	"github.com/linkerd/linkerd2/jaeger/cmd"
)

func main() {
	if err := cmd.RootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
