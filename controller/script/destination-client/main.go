package main

import (
	"context"
	"flag"
	"io"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	addrUtil "github.com/linkerd/linkerd2/pkg/addr"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

// This is a throwaway script for testing the destination service

func main() {
	addr := flag.String("addr", ":8086", "address of destination service")
	path := flag.String("path", "strest-server.default.svc.cluster.local:8888", "destination path")
	method := flag.String("method", "get", "which gRPC method to invoke")
	flag.Parse()

	client, conn, err := newClient(*addr)
	if err != nil {
		log.Fatal(err.Error())
	}
	defer conn.Close()

	req := &pb.GetDestination{
		Scheme: "k8s",
		Path:   *path,
	}

	switch *method {
	case "get":
		get(client, req)
	case "getProfile":
		getProfile(client, req)
	default:
		log.Fatalf("Unknown method: %s; supported methods: get, getProfile", *method)
	}
}

// newClient creates a new gRPC client to the Destination service.
func newClient(addr string) (pb.DestinationClient, *grpc.ClientConn, error) {
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return nil, nil, err
	}

	return pb.NewDestinationClient(conn), conn, nil
}

func get(client pb.DestinationClient, req *pb.GetDestination) {
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
			log.Printf("labels: %v", updateType.Add.MetricLabels)
			for _, addr := range updateType.Add.Addrs {
				log.Printf("- %s:%d", addrUtil.ProxyIPToString(addr.Addr.GetIp()), addr.Addr.Port)
				log.Printf("  - labels: %v", addr.MetricLabels)
				switch addr.GetProtocolHint().GetProtocol().(type) {
				case *pb.ProtocolHint_H2_:
					log.Printf("  - protocol hint: H2")
				default:
					log.Printf("  - protocol hint: UNKNOWN")
				}
				switch identityType := addr.GetTlsIdentity().GetStrategy().(type) {
				case *pb.TlsIdentity_K8SPodIdentity_:
					log.Printf("  - pod identity: %s", identityType.K8SPodIdentity.PodIdentity)
					log.Printf("  - controller ns: %s", identityType.K8SPodIdentity.ControllerNs)
				}
			}
			log.Println()
		case *pb.Update_Remove:
			log.Println("Remove:")
			for _, addr := range updateType.Remove.Addrs {
				log.Printf("- %s:%d", addrUtil.ProxyIPToString(addr.GetIp()), addr.Port)
			}
			log.Println()
		case *pb.Update_NoEndpoints:
			log.Println("NoEndpoints:")
			log.Printf("- exists:%t", updateType.NoEndpoints.Exists)
			log.Println()
		}
	}
}

func getProfile(client pb.DestinationClient, req *pb.GetDestination) {
	rsp, err := client.GetProfile(context.Background(), req)
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
		log.Printf("%+v", update)
		log.Println()
	}
}
