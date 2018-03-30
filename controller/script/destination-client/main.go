package main

import (
	"context"
	"flag"
	"io"
	"math/rand"
	"time"

	"github.com/runconduit/conduit/controller/destination"
	common "github.com/runconduit/conduit/controller/gen/common"
	pb "github.com/runconduit/conduit/controller/gen/proxy/destination"
	"github.com/runconduit/conduit/controller/util"
	log "github.com/sirupsen/logrus"
)

// This is a throwaway script for testing the destination service

func main() {
	rand.Seed(time.Now().UnixNano())

	addr := flag.String("addr", ":8089", "address of destination service")
	path := flag.String("path", "strest-server.default.svc.cluster.local:8888", "destination path")
	flag.Parse()

	client, conn, err := destination.NewClient(*addr)
	if err != nil {
		log.Fatal(err.Error())
	}
	defer conn.Close()

	req := &common.Destination{
		Scheme: "k8s",
		Path:   *path,
	}

	rsp, err := client.Get(context.Background(), req)
	if err != nil {
		log.Fatal(err.Error())
	}

	for {
		update, err := rsp.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err.Error())
		}

		switch updateType := update.Update.(type) {
		case *pb.Update_Add:
			log.Println("Add:")
			log.Printf("metric_labels: %v", updateType.Add.MetricLabels)
			for _, addr := range updateType.Add.Addrs {
				log.Printf("- %s:%d - %v", util.IPToString(addr.Addr.GetIp()), addr.Addr.Port, addr.MetricLabels)
			}
			log.Println()
		case *pb.Update_Remove:
			log.Println("Remove:")
			for _, addr := range updateType.Remove.Addrs {
				log.Printf("- %s:%d", util.IPToString(addr.GetIp()), addr.Port)
			}
			log.Println()
		case *pb.Update_NoEndpoints:
			log.Println("NoEndpoints:")
			log.Printf("- exists:%t", updateType.NoEndpoints.Exists)
			log.Println()
		}
	}
}
