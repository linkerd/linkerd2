package main

import (
	"os"

	"github.com/linkerd/linkerd2/cli/cmd"
)

func main() {
	if err := cmd.RootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
