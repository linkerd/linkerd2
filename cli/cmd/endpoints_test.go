package cmd

import (
	"fmt"
	"testing"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	net "github.com/linkerd/linkerd2-proxy-api/go/net"
	"github.com/linkerd/linkerd2/controller/api/public"
)

type endpointsExp struct {
	options     *endpointsOptions
	authorities []string
	endpoints   []authorityEndpoint
	file        string
}

type authorityEndpoint struct {
	namespace string
	serviceID string
	pods      []podDetails
}

type podDetails struct {
	name string
	ip   uint32
	port uint32
}

func TestEndpoints(t *testing.T) {
	options := newEndpointsOptions()

	t.Run("Returns endpoints same namespace", func(t *testing.T) {
		testEndpointsCall(endpointsExp{
			options:     options,
			authorities: []string{"emoji-svc.emojivoto.svc.cluster.local:8080", "voting-svc.emojivoto.svc.cluster.local:8080"},
			endpoints: []authorityEndpoint{
				{
					namespace: "emojivoto",
					serviceID: "emoji-svc",
					pods: []podDetails{
						{
							name: "emoji-6bf9f47bd5-jjcrl",
							ip:   16909060,
							port: 8080,
						},
					},
				},
				{
					namespace: "emojivoto",
					serviceID: "voting-svc",
					pods: []podDetails{
						{
							name: "voting-7bf9f47bd5-jjdrl",
							ip:   84281096,
							port: 8080,
						},
					},
				},
			},
			file: "endpoints_one_output.golden",
		}, t)
	})

	t.Run("Returns endpoints different namespace", func(t *testing.T) {
		testEndpointsCall(endpointsExp{
			options:     options,
			authorities: []string{"emoji-svc.emojivoto.svc.cluster.local:8080", "voting-svc.emojivoto2.svc.cluster.local:8080"},
			endpoints: []authorityEndpoint{
				{
					namespace: "emojivoto",
					serviceID: "emoji-svc",
					pods: []podDetails{
						{
							name: "emoji-6bf9f47bd5-jjcrl",
							ip:   16909060,
							port: 8080,
						},
					},
				},
				{
					namespace: "emojivoto2",
					serviceID: "voting-svc",
					pods: []podDetails{
						{
							name: "voting-7bf9f47bd5-jjdrl",
							ip:   84281096,
							port: 8080,
						},
					},
				},
			},
			file: "endpoints_two_outputs.golden",
		}, t)
	})

	options.outputFormat = jsonOutput
	t.Run("Returns endpoints same namespace (json)", func(t *testing.T) {
		testEndpointsCall(endpointsExp{
			options:     options,
			authorities: []string{"emoji-svc.emojivoto.svc.cluster.local:8080", "voting-svc.emojivoto.svc.cluster.local:8080"},
			endpoints: []authorityEndpoint{
				{
					namespace: "emojivoto",
					serviceID: "emoji-svc",
					pods: []podDetails{
						{
							name: "emoji-6bf9f47bd5-jjcrl",
							ip:   16909060,
							port: 8080,
						},
					},
				},
				{
					namespace: "emojivoto",
					serviceID: "voting-svc",
					pods: []podDetails{
						{
							name: "voting-7bf9f47bd5-jjdrl",
							ip:   84281096,
							port: 8080,
						},
					},
				},
			},
			file: "endpoints_one_output_json.golden",
		}, t)
	})
}

func buildAddrSet(endpoint authorityEndpoint) *pb.WeightedAddrSet {
	addrs := make([]*pb.WeightedAddr, 0)
	for _, pod := range endpoint.pods {
		addr := &net.TcpAddress{
			Ip:   &net.IPAddress{Ip: &net.IPAddress_Ipv4{Ipv4: pod.ip}},
			Port: pod.port,
		}
		labels := map[string]string{"pod": pod.name}
		weightedAddr := &pb.WeightedAddr{Addr: addr, MetricLabels: labels}
		addrs = append(addrs, weightedAddr)
	}
	labels := map[string]string{"namespace": endpoint.namespace, "service": endpoint.serviceID}
	return &pb.WeightedAddrSet{Addrs: addrs, MetricLabels: labels}
}

func testEndpointsCall(exp endpointsExp, t *testing.T) {
	updates := make([]pb.Update, 0)
	for _, endpoint := range exp.endpoints {
		addrSet := buildAddrSet(endpoint)
		updates = append(updates, pb.Update{Update: &pb.Update_Add{Add: addrSet}})
	}

	mockClient := &public.MockAPIClient{
		DestinationGetClientToReturn: &public.MockDestinationGetClient{
			UpdatesToReturn: updates,
		},
	}

	endpoints, err := requestEndpointsFromAPI(mockClient, exp.authorities)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := renderEndpoints(endpoints, exp.options)
	fmt.Printf("output:\n%s\n", output)

	diffTestdata(t, exp.file, output)
}
