package main

import "github.com/linkerd/linkerd2/proxy-init/cmd"

func main() {
	cmd.NewRootCmd().Execute()
}
