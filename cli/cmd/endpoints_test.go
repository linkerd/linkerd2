package cmd

import (
	"testing"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/public"
)

type endpointsExp struct {
	options     *endpointsOptions
	authorities []string
	endpoints   []public.AuthorityEndpoints
	file        string
}

func TestEndpoints(t *testing.T) {
	options := newEndpointsOptions()

	t.Run("Returns endpoints same namespace", func(t *testing.T) {
		testEndpointsCall(endpointsExp{
			options:     options,
			authorities: []string{"emoji-svc.emojivoto.svc.cluster.local:8080", "voting-svc.emojivoto.svc.cluster.local:8080"},
			endpoints: []public.AuthorityEndpoints{
				{
					Namespace: "emojivoto",
					ServiceID: "emoji-svc",
					Pods: []public.PodDetails{
						{
							Name: "emoji-6bf9f47bd5-jjcrl",
							IP:   16909060,
							Port: 8080,
						},
					},
				},
				{
					Namespace: "emojivoto",
					ServiceID: "voting-svc",
					Pods: []public.PodDetails{
						{
							Name: "voting-7bf9f47bd5-jjdrl",
							IP:   84281096,
							Port: 8080,
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
			endpoints: []public.AuthorityEndpoints{
				{
					Namespace: "emojivoto",
					ServiceID: "emoji-svc",
					Pods: []public.PodDetails{
						{
							Name: "emoji-6bf9f47bd5-jjcrl",
							IP:   16909060,
							Port: 8080,
						},
					},
				},
				{
					Namespace: "emojivoto2",
					ServiceID: "voting-svc",
					Pods: []public.PodDetails{
						{
							Name: "voting-7bf9f47bd5-jjdrl",
							IP:   84281096,
							Port: 8080,
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
			endpoints: []public.AuthorityEndpoints{
				{
					Namespace: "emojivoto",
					ServiceID: "emoji-svc",
					Pods: []public.PodDetails{
						{
							Name: "emoji-6bf9f47bd5-jjcrl",
							IP:   16909060,
							Port: 8080,
						},
					},
				},
				{
					Namespace: "emojivoto",
					ServiceID: "voting-svc",
					Pods: []public.PodDetails{
						{
							Name: "voting-7bf9f47bd5-jjdrl",
							IP:   84281096,
							Port: 8080,
						},
					},
				},
			},
			file: "endpoints_one_output_json.golden",
		}, t)
	})
}

func testEndpointsCall(exp endpointsExp, t *testing.T) {
	updates := make([]pb.Update, 0)
	for _, endpoint := range exp.endpoints {
		addrSet := public.BuildAddrSet(endpoint)
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

	diffTestdata(t, exp.file, output)
}
