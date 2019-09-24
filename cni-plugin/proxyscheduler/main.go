package main

import (
	"context"
	"flag"
	"github.com/linkerd/linkerd2/cni-plugin/proxyscheduler/server"
)

func main() {
	config := server.ProxySchedulerConfig{}
	flag.StringVar(&config.BindPort, "bind-port", "8087", "Port to bind for serving")
	flag.StringVar(&config.LinkerdNamespace, "linkerd-namespace", "linkerd", "The namespace in which linkerd has been installed")
	flag.Parse()

	agent, err := server.NewProxyAgentScheduler(config)
	if err != nil {
		panic(err)
	}
	err = agent.Run(context.TODO())
	if err != nil {
		panic(err)
	}
}
