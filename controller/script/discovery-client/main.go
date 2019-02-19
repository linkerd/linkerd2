package main

import (
	"context"
	"flag"
	"os"

	"github.com/golang/protobuf/jsonpb"
	"github.com/linkerd/linkerd2/controller/api/discovery"
	pb "github.com/linkerd/linkerd2/controller/gen/controller/discovery"
	log "github.com/sirupsen/logrus"
)

// This is a throwaway script for testing the destination service

func main() {
	addr := flag.String("addr", ":8086", "address of destination service")
	flag.Parse()

	client, conn, err := discovery.NewClient(*addr)
	if err != nil {
		log.Fatal(err.Error())
	}
	defer conn.Close()

	rsp, err := client.Endpoints(context.Background(), &pb.EndpointsParams{})
	if err != nil {
		log.Fatal(err.Error())
	}

	marshaler := jsonpb.Marshaler{EmitDefaults: true, Indent: "  "}
	err = marshaler.Marshal(os.Stdout, rsp)
	if err != nil {
		log.Fatal(err.Error())
	}
}
