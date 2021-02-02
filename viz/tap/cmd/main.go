package main

import (
	"fmt"
	"os"

	"github.com/linkerd/linkerd2/viz/tap/controller"
	"github.com/linkerd/linkerd2/viz/tap/injector"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("expected a subcommand")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "controller":
		controller.Main(os.Args[2:])
	case "injector":
		injector.Main(os.Args[2:])
	}
}
