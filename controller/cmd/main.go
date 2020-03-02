package main

import (
	"fmt"
	"os"

	servicemirror "github.com/linkerd/linkerd2/controller/cmd/service-mirror"

	"github.com/linkerd/linkerd2/controller/cmd/destination"
	"github.com/linkerd/linkerd2/controller/cmd/heartbeat"
	"github.com/linkerd/linkerd2/controller/cmd/identity"
	proxyinjector "github.com/linkerd/linkerd2/controller/cmd/proxy-injector"
	publicapi "github.com/linkerd/linkerd2/controller/cmd/public-api"
	spvalidator "github.com/linkerd/linkerd2/controller/cmd/sp-validator"
	"github.com/linkerd/linkerd2/controller/cmd/tap"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("expected a subcommand")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "destination":
		destination.Main(os.Args[2:])
	case "heartbeat":
		heartbeat.Main(os.Args[2:])
	case "identity":
		identity.Main(os.Args[2:])
	case "proxy-injector":
		proxyinjector.Main(os.Args[2:])
	case "public-api":
		publicapi.Main(os.Args[2:])
	case "sp-validator":
		spvalidator.Main(os.Args[2:])
	case "tap":
		tap.Main(os.Args[2:])
	case "service-mirror":
		servicemirror.Main(os.Args[2:])
	default:
		fmt.Printf("unknown subcommand: %s", os.Args[1])
		os.Exit(1)
	}
}
