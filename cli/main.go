package main

import (
	"os"

	"github.com/runconduit/conduit/cli/cmd"
)

func main() {
	if err := cmd.RootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
