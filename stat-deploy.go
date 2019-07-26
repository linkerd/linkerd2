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

	rsp, err := client.StatSummary(context.Background(), &pb.StatSummaryRequest{
		Selector: &pb.ResourceSelection{
			Resource: &pb.Resource{
				Namespace: "default",
				Type:      "deployment",
			},
		},
		TimeWindow: "1m",
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("%+v\n", rsp)
}
