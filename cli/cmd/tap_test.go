package cmd

import (
	"errors"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/runconduit/conduit/cli/k8s"
	pb "github.com/runconduit/conduit/controller/gen/public"

	"github.com/golang/protobuf/ptypes/duration"
	google_protobuf "github.com/golang/protobuf/ptypes/duration"
	common "github.com/runconduit/conduit/controller/gen/common"
	"github.com/runconduit/conduit/controller/util"
	"google.golang.org/grpc/codes"
)

func TestRequestTapFromApi(t *testing.T) {
	t.Run("Should render busy response if everything went well", func(t *testing.T) {
		authority := "localhost"
		targetName := "pod-666"
		resourceType := k8s.KubernetesPods
		scheme := "https"
		method := "GET"
		path := "/some/path"
		sourceIp := "234.234.234.234"
		targetIp := "123.123.123.123"
		mockApiClient := &mockApiClient{}

		event1 := createEvent(&common.TapEvent_Http{
			Event: &common.TapEvent_Http_RequestInit_{
				RequestInit: &common.TapEvent_Http_RequestInit{
					Id: &common.TapEvent_Http_StreamId{
						Base: 1,
					},
					Authority: authority,
					Path:      path,
				},
			},
		})
		event2 := createEvent(&common.TapEvent_Http{
			Event: &common.TapEvent_Http_ResponseEnd_{
				ResponseEnd: &common.TapEvent_Http_ResponseEnd{
					Id: &common.TapEvent_Http_StreamId{
						Base: 1,
					},
					GrpcStatus: 666,
					SinceRequestInit: &google_protobuf.Duration{
						Seconds: 10,
					},
					SinceResponseInit: &google_protobuf.Duration{
						Seconds: 100,
					},
					ResponseBytes: 1337,
				},
			},
		})
		mockApiClient.api_TapClientToReturn = &mockApi_TapClient{
			tapEventsToReturn: []common.TapEvent{event1, event2},
		}

		partialReq := &pb.TapRequest{
			MaxRps:    0,
			ToPort:    8080,
			ToIP:      targetIp,
			FromPort:  90,
			FromIP:    sourceIp,
			Scheme:    scheme,
			Method:    method,
			Authority: authority,
			Path:      path,
		}

		output, err := requestTapFromApi(mockApiClient, targetName, resourceType, partialReq)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		goldenFileBytes, err := ioutil.ReadFile("testdata/tap_busy_output.golden")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		expectedContent := string(goldenFileBytes)

		if expectedContent != output {
			t.Fatalf("Expected function to render:\n%s\bbut got:\n%s", expectedContent, output)
		}
	})

	t.Run("Should render empty response if no events returned", func(t *testing.T) {
		authority := "localhost"
		targetName := "pod-666"
		resourceType := k8s.KubernetesPods
		scheme := "https"
		method := "GET"
		path := "/some/path"
		sourceIp := "234.234.234.234"
		targetIp := "123.123.123.123"
		mockApiClient := &mockApiClient{}

		mockApiClient.api_TapClientToReturn = &mockApi_TapClient{
			tapEventsToReturn: []common.TapEvent{},
		}

		partialReq := &pb.TapRequest{
			MaxRps:    0,
			ToPort:    8080,
			ToIP:      targetIp,
			FromPort:  90,
			FromIP:    sourceIp,
			Scheme:    scheme,
			Method:    method,
			Authority: authority,
			Path:      path,
		}

		output, err := requestTapFromApi(mockApiClient, targetName, resourceType, partialReq)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		goldenFileBytes, err := ioutil.ReadFile("testdata/tap_empty_output.golden")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		expectedContent := string(goldenFileBytes)

		if expectedContent != output {
			t.Fatalf("Expected function to render:\n%s\bbut got:\n%s", expectedContent, output)
		}
	})

	t.Run("Should return error if stream returned error", func(t *testing.T) {
		t.SkipNow()
		authority := "localhost"
		targetName := "pod-666"
		resourceType := k8s.KubernetesPods
		scheme := "https"
		method := "GET"
		path := "/some/path"
		sourceIp := "234.234.234.234"
		targetIp := "123.123.123.123"
		mockApiClient := &mockApiClient{}
		mockApiClient.api_TapClientToReturn = &mockApi_TapClient{
			errorsToReturn: []error{errors.New("expected")},
		}

		partialReq := &pb.TapRequest{
			MaxRps:    0,
			ToPort:    8080,
			ToIP:      targetIp,
			FromPort:  90,
			FromIP:    sourceIp,
			Scheme:    scheme,
			Method:    method,
			Authority: authority,
			Path:      path,
		}

		output, err := requestTapFromApi(mockApiClient, targetName, resourceType, partialReq)
		if err == nil {
			t.Fatalf("Expecting error, got nothing but outpus [%s]", output)
		}
	})
}

func TestEventToString(t *testing.T) {
	toTapEvent := func(httpEvent *common.TapEvent_Http) *common.TapEvent {
		streamId := &common.TapEvent_Http_StreamId{
			Base:   7,
			Stream: 8,
		}

		switch httpEvent.Event.(type) {
		case *common.TapEvent_Http_RequestInit_:
			httpEvent.GetRequestInit().Id = streamId
		case *common.TapEvent_Http_ResponseInit_:
			httpEvent.GetResponseInit().Id = streamId
		case *common.TapEvent_Http_ResponseEnd_:
			httpEvent.GetResponseEnd().Id = streamId
		}

		return &common.TapEvent{
			Source: &common.TcpAddress{
				Ip:   util.IPV4(1, 2, 3, 4),
				Port: 5555,
			},
			Target: &common.TcpAddress{
				Ip:   util.IPV4(2, 3, 4, 5),
				Port: 6666,
			},
			Event: &common.TapEvent_Http_{Http: httpEvent},
		}
	}

	t.Run("Converts HTTP request init event to string", func(t *testing.T) {
		event := toTapEvent(&common.TapEvent_Http{
			Event: &common.TapEvent_Http_RequestInit_{
				RequestInit: &common.TapEvent_Http_RequestInit{
					Method: &common.HttpMethod{
						Type: &common.HttpMethod_Registered_{
							Registered: common.HttpMethod_POST,
						},
					},
					Scheme: &common.Scheme{
						Type: &common.Scheme_Registered_{
							Registered: common.Scheme_HTTPS,
						},
					},
					Authority: "hello.default:7777",
					Path:      "/hello.v1.HelloService/Hello",
				},
			},
		})

		expectedOutput := "req id=7:8 src=1.2.3.4:5555 dst=2.3.4.5:6666 :method=POST :authority=hello.default:7777 :path=/hello.v1.HelloService/Hello"
		output := renderTapEvent(event)
		if output != expectedOutput {
			t.Fatalf("Expecting command output to be [%s], got [%s]", expectedOutput, output)
		}
	})

	t.Run("Converts HTTP response init event to string", func(t *testing.T) {
		event := toTapEvent(&common.TapEvent_Http{
			Event: &common.TapEvent_Http_ResponseInit_{
				ResponseInit: &common.TapEvent_Http_ResponseInit{
					SinceRequestInit: &duration.Duration{Nanos: 999000},
					HttpStatus:       http.StatusOK,
				},
			},
		})

		expectedOutput := "rsp id=7:8 src=1.2.3.4:5555 dst=2.3.4.5:6666 :status=200 latency=999µs"
		output := renderTapEvent(event)
		if output != expectedOutput {
			t.Fatalf("Expecting command output to be [%s], got [%s]", expectedOutput, output)
		}
	})

	t.Run("Converts HTTP response end event to string", func(t *testing.T) {
		event := toTapEvent(&common.TapEvent_Http{
			Event: &common.TapEvent_Http_ResponseEnd_{
				ResponseEnd: &common.TapEvent_Http_ResponseEnd{
					SinceRequestInit:  &duration.Duration{Nanos: 999000},
					SinceResponseInit: &duration.Duration{Nanos: 888000},
					ResponseBytes:     111,
					GrpcStatus:        uint32(codes.OK),
				},
			},
		})

		expectedOutput := "end id=7:8 src=1.2.3.4:5555 dst=2.3.4.5:6666 grpc-status=OK duration=888µs response-length=111B"
		output := renderTapEvent(event)
		if output != expectedOutput {
			t.Fatalf("Expecting command output to be [%s], got [%s]", expectedOutput, output)
		}
	})

	t.Run("Handles unknown event types", func(t *testing.T) {
		event := toTapEvent(&common.TapEvent_Http{})

		expectedOutput := "unknown src=1.2.3.4:5555 dst=2.3.4.5:6666"
		output := renderTapEvent(event)
		if output != expectedOutput {
			t.Fatalf("Expecting command output to be [%s], got [%s]", expectedOutput, output)
		}
	})
}

func createEvent(event_http *common.TapEvent_Http) common.TapEvent {
	event := common.TapEvent{
		Source: &common.TcpAddress{
			Ip: &common.IPAddress{
				Ip: &common.IPAddress_Ipv4{
					Ipv4: uint32(1),
				},
			},
		},
		Target: &common.TcpAddress{
			Ip: &common.IPAddress{
				Ip: &common.IPAddress_Ipv4{
					Ipv4: uint32(9),
				},
			},
		},
		Event: &common.TapEvent_Http_{
			Http: event_http,
		},
	}
	return event
}
