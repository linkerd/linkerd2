package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/linkerd/linkerd2/cli/cmd"
)

func main() {
	root := cmd.NewRootCmd()
	args := os.Args[1:]
	if _, _, err := root.Find(args); err != nil {
		if strings.HasPrefix(args[0], "-") {
			fmt.Fprintln(os.Stderr, "Cannot accept flags before Linkerd extension name")
			os.Exit(1)
		}
		path, err := exec.LookPath(fmt.Sprintf("linkerd-%s", args[0]))
		if err == nil {
			// We're working with a Linkerd plugin at this point which means
			// it's up to the plugin to cleanse the arguments if needed.
			//nolint:gosec
			plugin := exec.Command(path, args[1:]...)
			plugin.Stdin = os.Stdin
			plugin.Stdout = os.Stdout
			plugin.Stderr = os.Stderr
			err = plugin.Run()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
	}
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
