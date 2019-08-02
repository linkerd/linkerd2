package profiles

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/controller/api/util"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/protohttp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTapToServiceProfile(t *testing.T) {
	name := "service-name"
	namespace := "service-namespace"
	tapDuration := 5 * time.Second
	routeLimit := 20

	params := util.TapRequestParams{
		Resource:  "deploy/" + name,
		Namespace: namespace,
	}

	tapReq, err := util.BuildTapByResourceRequest(params)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	event1 := util.CreateTapEvent(
		&pb.TapEvent_Http{
			Event: &pb.TapEvent_Http_RequestInit_{

				RequestInit: &pb.TapEvent_Http_RequestInit{
					Id: &pb.TapEvent_Http_StreamId{
						Base: 1,
					},
					Authority: "",
					Path:      "/emojivoto.v1.VotingService/VoteFire",
					Method: &pb.HttpMethod{
						Type: &pb.HttpMethod_Registered_{
							Registered: pb.HttpMethod_POST,
						},
					},
				},
			},
		},
		map[string]string{},
		pb.TapEvent_INBOUND,
	)

	event2 := util.CreateTapEvent(
		&pb.TapEvent_Http{
			Event: &pb.TapEvent_Http_RequestInit_{

				RequestInit: &pb.TapEvent_Http_RequestInit{
					Id: &pb.TapEvent_Http_StreamId{
						Base: 2,
					},
					Authority: "",
					Path:      "/my/path/hi",
					Method: &pb.HttpMethod{
						Type: &pb.HttpMethod_Registered_{
							Registered: pb.HttpMethod_GET,
						},
					},
				},
			},
		},
		map[string]string{},
		pb.TapEvent_INBOUND,
	)

	kubeAPI, err := k8s.NewFakeAPI()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	ts := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			for _, event := range []pb.TapEvent{event1, event2} {
				event := event // pin
				err = protohttp.WriteProtoToHTTPResponse(w, &event)
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
			}
		}),
	)
	defer ts.Close()
	kubeAPI.Config.Host = ts.URL

	expectedServiceProfile := sp.ServiceProfile{
		TypeMeta: serviceProfileMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "." + namespace + ".svc.cluster.local",
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

	actualServiceProfile, err := tapToServiceProfile(kubeAPI, tapReq, namespace, name, tapDuration, routeLimit)
	if err != nil {
		t.Fatalf("Failed to create ServiceProfile: %v", err)
	}

	err = ServiceProfileYamlEquals(actualServiceProfile, expectedServiceProfile)
	if err != nil {
		t.Fatalf("ServiceProfiles are not equal: %v", err)
	}
}
