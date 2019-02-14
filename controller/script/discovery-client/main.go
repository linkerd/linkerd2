package main

import (
	"context"
	"flag"
	"os"

	"github.com/golang/protobuf/jsonpb"
	"github.com/linkerd/linkerd2/controller/gen/controller/discovery"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

// This is a throwaway script for testing the destination service

func main() {
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

	marshaler := jsonpb.Marshaler{EmitDefaults: true, Indent: "  "}
	err = marshaler.Marshal(os.Stdout, rsp)
	if err != nil {
		log.Fatal(err.Error())
	}
}

// newClient creates a new gRPC client to the Proxy API service.
func newClient(addr string) (discovery.DiscoveryClient, *grpc.ClientConn, error) {
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return nil, nil, err
	}

	return discovery.NewDiscoveryClient(conn), conn, nil
}
