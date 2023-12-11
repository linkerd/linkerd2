package autoregistration

import (
	"context"
	"flag"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/autoregistration"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	"github.com/linkerd/linkerd2/pkg/trace"
	log "github.com/sirupsen/logrus"
)

// Main executes the autoregistration subcommand
func Main(args []string) {
	cmd := flag.NewFlagSet("autoregistration", flag.ExitOnError)

	addr := cmd.String("addr", ":8089", "address to serve on")
	adminAddr := cmd.String("admin-addr", ":9996", "address of HTTP admin server")
	enablePprof := cmd.Bool("enable-pprof", false, "Enable pprof endpoints on the admin server")

	traceCollector := flags.AddTraceFlags(cmd)
	componentName := "linkerd-autoregistration"

	flags.ConfigureAndParse(cmd, args)

	ready := false
	adminServer := admin.NewServer(*adminAddr, *enablePprof, &ready)

	go func() {
		log.Infof("starting admin server on %s", *adminAddr)
		if err := adminServer.ListenAndServe(); err != nil {
			log.Errorf("failed to start autoregistration admin server: %s", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	//
	// Create, initialize and run service
	//
	svc := autoregistration.NewService()
	//
	// Bind and serve
	//
	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		//nolint:gocritic
		log.Fatalf("Failed to listen on %s: %s", *addr, err)
	}

	if *traceCollector != "" {
		if err := trace.InitializeTracing(componentName, *traceCollector); err != nil {
			log.Warnf("failed to initialize tracing: %s", err)
		}
	}
	srv := prometheus.NewGrpcServer()
	autoregistration.Register(srv, svc)
	go func() {
		log.Infof("starting gRPC server on %s", *addr)
		if err := srv.Serve(lis); err != nil {
			log.Errorf("failed to start autoregistration gRPC server: %s", err)
		}
	}()

	ready = true

	<-stop
	log.Infof("shutting down gRPC server on %s", *addr)
	srv.GracefulStop()
	adminServer.Shutdown(ctx)
}
