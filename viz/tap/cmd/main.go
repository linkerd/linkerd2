package main

import (
	"fmt"
	"os"

	"github.com/linkerd/linkerd2/viz/tap/api"
	"github.com/linkerd/linkerd2/viz/tap/injector"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("expected a subcommand")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "api":
		api.Main(os.Args[2:])
	case "injector":
		injector.Main(os.Args[2:])
	}
}
