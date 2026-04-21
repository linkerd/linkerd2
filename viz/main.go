package main

import (
	"os"

	"github.com/linkerd/linkerd2/viz/cmd"
)

func main() {
	if err := cmd.NewCmdViz().Execute(); err != nil {
		os.Exit(1)
	}
}
