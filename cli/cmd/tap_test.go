package cmd

import (
	"bytes"
	"errors"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/golang/protobuf/ptypes/duration"
	"github.com/runconduit/conduit/controller/api/public"
	common "github.com/runconduit/conduit/controller/gen/common"
	"github.com/runconduit/conduit/controller/util"
	"github.com/runconduit/conduit/pkg/k8s"
	"google.golang.org/grpc/codes"
)

func TestRequestTapByResourceFromAPI(t *testing.T) {
	t.Run("Should render busy response if everything went well", func(t *testing.T) {
		resourceType := k8s.Pods
		targetName := "pod-666"
		options := &tapOptions{
			scheme:    "https",
			method:    "GET",
			authority: "localhost",
			path:      "/some/path",
		}

		req, err := buildTapByResourceRequest(
			[]string{resourceType, targetName},
			options,
		)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		event1 := createEvent(
			&common.TapEvent_Http{
				Event: &common.TapEvent_Http_RequestInit_{
					RequestInit: &common.TapEvent_Http_RequestInit{
						Id: &common.TapEvent_Http_StreamId{
							Base: 1,
						},
						Authority: options.authority,
						Path:      options.path,
					},
				},
			},
			map[string]string{
				"pod": "my-pod",
				"tls": "true",
			},
		)
		event2 := createEvent(
			&common.TapEvent_Http{
				Event: &common.TapEvent_Http_ResponseEnd_{
					ResponseEnd: &common.TapEvent_Http_ResponseEnd{
						Id: &common.TapEvent_Http_StreamId{
							Base: 1,
						},
						Eos: &common.Eos{
							End: &common.Eos_GrpcStatusCode{GrpcStatusCode: 666},
						},
						SinceRequestInit: &duration.Duration{
							Seconds: 10,
						},
						SinceResponseInit: &duration.Duration{
							Seconds: 100,
						},
						ResponseBytes: 1337,
					},
				},
			},
			map[string]string{},
		)
		mockApiClient := &public.MockConduitApiClient{}
		mockApiClient.Api_TapByResourceClientToReturn = &public.MockApi_TapByResourceClient{
			TapEventsToReturn: []common.TapEvent{event1, event2},
		}

		writer := bytes.NewBufferString("")
		err = requestTapByResourceFromAPI(writer, mockApiClient, req)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		goldenFileBytes, err := ioutil.ReadFile("testdata/tap_busy_output.golden")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		expectedContent := string(goldenFileBytes)
		output := writer.String()
		if expectedContent != output {
			t.Fatalf("Expected function to render:\n%s\bbut got:\n%s", expectedContent, output)
		}
	})

	t.Run("Should render empty response if no events returned", func(t *testing.T) {
		resourceType := k8s.Pods
		targetName := "pod-666"
		options := &tapOptions{
			scheme:    "https",
			method:    "GET",
			authority: "localhost",
			path:      "/some/path",
		}

		req, err := buildTapByResourceRequest(
			[]string{resourceType, targetName},
			options,
		)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		mockApiClient := &public.MockConduitApiClient{}
		mockApiClient.Api_TapByResourceClientToReturn = &public.MockApi_TapByResourceClient{
			TapEventsToReturn: []common.TapEvent{},
		}

		writer := bytes.NewBufferString("")
		err = requestTapByResourceFromAPI(writer, mockApiClient, req)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		goldenFileBytes, err := ioutil.ReadFile("testdata/tap_empty_output.golden")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		expectedContent := string(goldenFileBytes)
		output := writer.String()
		if expectedContent != output {
			t.Fatalf("Expected function to render:\n%s\bbut got:\n%s", expectedContent, output)
		}
	})

	t.Run("Should return error if stream returned error", func(t *testing.T) {
		t.SkipNow()
		resourceType := k8s.Pods
		targetName := "pod-666"
		options := &tapOptions{
			scheme:    "https",
			method:    "GET",
			authority: "localhost",
			path:      "/some/path",
		}

		req, err := buildTapByResourceRequest(
			[]string{resourceType, targetName},
			options,
		)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		mockApiClient := &public.MockConduitApiClient{}
		mockApiClient.Api_TapByResourceClientToReturn = &public.MockApi_TapByResourceClient{
			ErrorsToReturn: []error{errors.New("expected")},
		}

		writer := bytes.NewBufferString("")
		err = requestTapByResourceFromAPI(writer, mockApiClient, req)
		if err == nil {
			t.Fatalf("Expecting error, got nothing but output [%s]", writer.String())
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
			Destination: &common.TcpAddress{
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

		expectedOutput := "req id=7:8 src=1.2.3.4:5555 dst=2.3.4.5:6666 :method=POST :authority=hello.default:7777 :path=/hello.v1.HelloService/Hello secured=no"
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

		expectedOutput := "rsp id=7:8 src=1.2.3.4:5555 dst=2.3.4.5:6666 :status=200 latency=999µs secured=no"
		output := renderTapEvent(event)
		if output != expectedOutput {
			t.Fatalf("Expecting command output to be [%s], got [%s]", expectedOutput, output)
		}
	})

	t.Run("Converts gRPC response end event to string", func(t *testing.T) {
		event := toTapEvent(&common.TapEvent_Http{
			Event: &common.TapEvent_Http_ResponseEnd_{
				ResponseEnd: &common.TapEvent_Http_ResponseEnd{
					SinceRequestInit:  &duration.Duration{Nanos: 999000},
					SinceResponseInit: &duration.Duration{Nanos: 888000},
					ResponseBytes:     111,
					Eos: &common.Eos{
						End: &common.Eos_GrpcStatusCode{GrpcStatusCode: uint32(codes.OK)},
					},
				},
			},
		})

		expectedOutput := "end id=7:8 src=1.2.3.4:5555 dst=2.3.4.5:6666 grpc-status=OK duration=888µs response-length=111B secured=no"
		output := renderTapEvent(event)
		if output != expectedOutput {
			t.Fatalf("Expecting command output to be [%s], got [%s]", expectedOutput, output)
		}
	})

	t.Run("Converts HTTP response end event with reset error code to string", func(t *testing.T) {
		event := toTapEvent(&common.TapEvent_Http{
			Event: &common.TapEvent_Http_ResponseEnd_{
				ResponseEnd: &common.TapEvent_Http_ResponseEnd{
					SinceRequestInit:  &duration.Duration{Nanos: 999000},
					SinceResponseInit: &duration.Duration{Nanos: 888000},
					ResponseBytes:     111,
					Eos: &common.Eos{
						End: &common.Eos_ResetErrorCode{ResetErrorCode: 123},
					},
				},
			},
		})

		expectedOutput := "end id=7:8 src=1.2.3.4:5555 dst=2.3.4.5:6666 reset-error=123 duration=888µs response-length=111B secured=no"
		output := renderTapEvent(event)
		if output != expectedOutput {
			t.Fatalf("Expecting command output to be [%s], got [%s]", expectedOutput, output)
		}
	})

	t.Run("Converts HTTP response end event with empty EOS context string", func(t *testing.T) {
		event := toTapEvent(&common.TapEvent_Http{
			Event: &common.TapEvent_Http_ResponseEnd_{
				ResponseEnd: &common.TapEvent_Http_ResponseEnd{
					SinceRequestInit:  &duration.Duration{Nanos: 999000},
					SinceResponseInit: &duration.Duration{Nanos: 888000},
					ResponseBytes:     111,
					Eos:               &common.Eos{},
				},
			},
		})

		expectedOutput := "end id=7:8 src=1.2.3.4:5555 dst=2.3.4.5:6666 duration=888µs response-length=111B secured=no"
		output := renderTapEvent(event)
		if output != expectedOutput {
			t.Fatalf("Expecting command output to be [%s], got [%s]", expectedOutput, output)
		}
	})

	t.Run("Converts HTTP response end event without EOS context string", func(t *testing.T) {
		event := toTapEvent(&common.TapEvent_Http{
			Event: &common.TapEvent_Http_ResponseEnd_{
				ResponseEnd: &common.TapEvent_Http_ResponseEnd{
					SinceRequestInit:  &duration.Duration{Nanos: 999000},
					SinceResponseInit: &duration.Duration{Nanos: 888000},
					ResponseBytes:     111,
				},
			},
		})

		expectedOutput := "end id=7:8 src=1.2.3.4:5555 dst=2.3.4.5:6666 duration=888µs response-length=111B secured=no"
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

func createEvent(event_http *common.TapEvent_Http, dstMeta map[string]string) common.TapEvent {
	event := common.TapEvent{
		Source: &common.TcpAddress{
			Ip: &common.IPAddress{
				Ip: &common.IPAddress_Ipv4{
					Ipv4: uint32(1),
				},
			},
		},
		Destination: &common.TcpAddress{
			Ip: &common.IPAddress{
				Ip: &common.IPAddress_Ipv4{
					Ipv4: uint32(9),
				},
			},
		},
		Event: &common.TapEvent_Http_{
			Http: event_http,
		},
		DestinationMeta: &common.TapEvent_EndpointMeta{
			Labels: dstMeta,
		},
	}
	return event
}
