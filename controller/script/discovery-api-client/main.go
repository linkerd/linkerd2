package main

import (
	"context"
	"flag"
	"math/rand"
	"time"

	"github.com/linkerd/linkerd2/controller/gen/controller/discovery"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

// This is a throwaway script for testing the proxy-api service

func main() {
	rand.Seed(time.Now().UnixNano())

	addr := flag.String("addr", ":8086", "address of destination service")
	flag.Parse()

	client, conn, err := newClient(*addr)
	if err != nil {
		log.Fatal(err.Error())
	}
	defer conn.Close()

	rsp, err := client.Endpoints(context.Background(), &discovery.EndpointsParams{})
	if err != nil {
		log.Fatal(err.Error())
	}

	log.Infof("%+v", rsp)
}

// newClient creates a new gRPC client to the Proxy API service.
func newClient(addr string) (discovery.ApiClient, *grpc.ClientConn, error) {
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return nil, nil, err
	}

	return discovery.NewApiClient(conn), conn, nil
}
