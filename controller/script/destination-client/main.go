package main

import (
	"context"
	"flag"
	"io"
	log "github.com/sirupsen/logrus"
	"math/rand"
	"time"

	"github.com/runconduit/conduit/controller/destination"
	common "github.com/runconduit/conduit/controller/gen/common"
	"github.com/runconduit/conduit/controller/util"
)

// This is a throwaway script for testing the destination service

func main() {
	rand.Seed(time.Now().UnixNano())

	addr := flag.String("addr", ":8089", "address of proxy api")
	flag.Parse()

	client, conn, err := destination.NewClient(*addr)
	if err != nil {
		log.Fatal(err.Error())
	}
	defer conn.Close()

	req := &common.Destination{
		Scheme: "k8s",
		Path:   "strest-server.default.svc.cluster.local:8888",
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
		if add := update.GetAdd(); add != nil {
			log.Println("Add:")
			for _, addr := range add.Addrs {
				log.Printf("- %s:%d", util.IPToString(addr.Addr.GetIp()), addr.Addr.Port)
			}
			log.Println()
		}
		if remove := update.GetRemove(); remove != nil {
			log.Println("Remove:")
			for _, addr := range remove.Addrs {
				log.Printf("- %s:%d", util.IPToString(addr.GetIp()), addr.Port)
			}
			log.Println()
		}
	}
}
