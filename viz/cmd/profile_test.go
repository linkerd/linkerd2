package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/profiles"
	"github.com/linkerd/linkerd2/pkg/protohttp"
	metricsPb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	tapPb "github.com/linkerd/linkerd2/viz/tap/gen/tap"
	"github.com/linkerd/linkerd2/viz/tap/pkg"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTapToServiceProfile(t *testing.T) {
	name := "service-name"
	namespace := "service-namespace"
	clusterDomain := "service-cluster.local"
	tapDuration := 5 * time.Second
	routeLimit := 20

	params := pkg.TapRequestParams{
		Resource:  "deploy/" + name,
		Namespace: namespace,
	}

	tapReq, err := pkg.BuildTapByResourceRequest(params)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	event1 := pkg.CreateTapEvent(
		&tapPb.TapEvent_Http{
			Event: &tapPb.TapEvent_Http_RequestInit_{

				RequestInit: &tapPb.TapEvent_Http_RequestInit{
					Id: &tapPb.TapEvent_Http_StreamId{
						Base: 1,
					},
					Authority: "",
					Path:      "/emojivoto.v1.VotingService/VoteFire",
					Method: &metricsPb.HttpMethod{
						Type: &metricsPb.HttpMethod_Registered_{
							Registered: metricsPb.HttpMethod_POST,
						},
					},
				},
			},
		},
		map[string]string{},
		tapPb.TapEvent_INBOUND,
	)

	event2 := pkg.CreateTapEvent(
		&tapPb.TapEvent_Http{
			Event: &tapPb.TapEvent_Http_RequestInit_{

				RequestInit: &tapPb.TapEvent_Http_RequestInit{
					Id: &tapPb.TapEvent_Http_StreamId{
						Base: 2,
					},
					Authority: "",
					Path:      "/my/path/hi",
					Method: &metricsPb.HttpMethod{
						Type: &metricsPb.HttpMethod_Registered_{
							Registered: metricsPb.HttpMethod_GET,
						},
					},
				},
			},
		},
		map[string]string{},
		tapPb.TapEvent_INBOUND,
	)

	kubeAPI, err := k8s.NewFakeAPI()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	ts := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			for _, event := range []*tapPb.TapEvent{event1, event2} {
				event := event // pin
				err = protohttp.WriteProtoToHTTPResponse(w, event)
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
			}
		}),
	)
	defer ts.Close()
	kubeAPI.Config.Host = ts.URL

	expectedServiceProfile := sp.ServiceProfile{
		TypeMeta: profiles.ServiceProfileMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "." + namespace + ".svc." + clusterDomain,
			Namespace: namespace,
		},
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				{
					Name: "GET /my/path/hi",
					Condition: &sp.RequestMatch{
						PathRegex: `/my/path/hi`,
						Method:    "GET",
					},
				},
				{
					Name: "POST /emojivoto.v1.VotingService/VoteFire",
					Condition: &sp.RequestMatch{
						PathRegex: `/emojivoto\.v1\.VotingService/VoteFire`,
						Method:    "POST",
					},
				},
			},
		},
	}

	actualServiceProfile, err := tapToServiceProfile(context.Background(), kubeAPI, tapReq, namespace, name, clusterDomain, tapDuration, routeLimit)
	if err != nil {
		t.Fatalf("Failed to create ServiceProfile: %v", err)
	}

	err = profiles.ServiceProfileYamlEquals(actualServiceProfile, expectedServiceProfile)
	if err != nil {
		t.Fatalf("ServiceProfiles are not equal: %v", err)
	}
}
