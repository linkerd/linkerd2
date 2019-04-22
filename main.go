package main

import (
	"context"
	"fmt"

	"github.com/linkerd/linkerd2/controller/api/public"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
)

func main() {
	client, err := public.NewInternalClient("linkerd", "localhost:8085")
	if err != nil {
		panic(err)
	}

	rsp, err := client.Edges(context.Background(), &pb.StatSummaryRequest{
		Selector: &pb.ResourceSelection{
			Resource: &pb.Resource{
				Namespace: "linkerd",
				Type:      "authority",
			},
		},
		TimeWindow: "1m",
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("%+v\n", rsp)
}
