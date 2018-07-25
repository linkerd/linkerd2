package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/linkerd/linkerd2/controller/api/proxy"
	"github.com/linkerd/linkerd2/controller/destination"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/logging"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
)

func main() {
	addr := flag.String("addr", ":8086", "address to serve on")
	metricsAddr := flag.String("metrics-addr", ":9996", "address to serve scrapable metrics on")
	destinationAddr := flag.String("destination-addr", "127.0.0.1:8089", "address of destination service")
	logLevel := logging.LogLevelFlag()
	printVersion := version.VersionFlag()
	flag.Parse()

	logging.SetLogLevel(*logLevel)
	version.MaybePrintVersionAndExit(*printVersion)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	destinationClient, conn, err := destination.NewClient(*destinationAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	server, lis, err := proxy.NewServer(*addr, destinationClient)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		log.Infof("starting gRPC server on %s", *addr)
		server.Serve(lis)
	}()

	go admin.StartServer(*metricsAddr, nil)

	<-stop

	log.Infof("shutting down gRPC server on %s", *addr)
	server.GracefulStop()
}
