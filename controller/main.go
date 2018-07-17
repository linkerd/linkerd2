package main

import (
	log "github.com/sirupsen/logrus"
	"os"
	"github.com/linkerd/linkerd2/controller/cmd/ca"
	"github.com/linkerd/linkerd2/controller/cmd/destination"
	"github.com/linkerd/linkerd2/controller/cmd/public-api"
	"github.com/linkerd/linkerd2/controller/cmd/proxy-api"
	"github.com/linkerd/linkerd2/controller/cmd/tap"
	"strings"
)

func main() {
	// Especially because all of the commands use client-go which ends up
	// making the executable huge, it is much more efficient for link time,
	// executable size, and container image size to have them all in one
	// executable.
	if len(os.Args) < 2 || strings.HasPrefix(os.Args[1], "-") {
		log.Fatal("no command given; it must be the first argument")
	}
	// All the commands' Main() functions are written as though they are
	// main.main(). They use the flag package that doesn't tolerate non-flag
	// arguments, so remove the command from os.Args before calling Main().
	cmd := os.Args[1]
	copy(os.Args[1:], os.Args[2:]) // Remove os.Args[1]
	switch cmd {
	case "ca":
		ca_main.Main()
	case "destination":
		destination_main.Main()
	case "proxy-api":
		proxy_api_main.Main()
	case "public-api":
		public_api_main.Main()
	case "tap":
		tap_main.Main()
	default:
		log.Fatalf("unrecognized command: %s", os.Args[1])
	}
}
