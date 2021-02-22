package cmd

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang/protobuf/ptypes/duration"
	netPb "github.com/linkerd/linkerd2/controller/gen/common/net"
	"github.com/linkerd/linkerd2/pkg/addr"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/protohttp"
	metricsPb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	tapPb "github.com/linkerd/linkerd2/viz/tap/gen/tap"
	"github.com/linkerd/linkerd2/viz/tap/pkg"
	"google.golang.org/grpc/codes"
)

const targetName = "pod-666"

func busyTest(t *testing.T, output string) {
	resourceType := k8s.Pod
	params := pkg.TapRequestParams{
		Resource:  resourceType + "/" + targetName,
		Scheme:    "https",
		Method:    "GET",
		Authority: "localhost",
		Path:      "/some/path",
	}

	req, err := pkg.BuildTapByResourceRequest(params)
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
					Method: &metricsPb.HttpMethod{
						Type: &metricsPb.HttpMethod_Registered_{
							Registered: metricsPb.HttpMethod_GET,
						},
					},
					Scheme: &metricsPb.Scheme{
						Type: &metricsPb.Scheme_Registered_{
							Registered: metricsPb.Scheme_HTTPS,
						},
					},
					Authority: params.Authority,
					Path:      params.Path,
					Headers: &metricsPb.Headers{
						Headers: []*metricsPb.Headers_Header{
							{
								Name: "header-name-1",
								Value: &metricsPb.Headers_Header_ValueStr{
									ValueStr: "header-value-str-1",
								},
							},
							{
								Name: "header-name-2",
								Value: &metricsPb.Headers_Header_ValueBin{
									ValueBin: []byte("header-value-bin-2"),
								},
							},
						},
					},
				},
			},
		},
		map[string]string{
			"pod": "my-pod",
			"tls": "true",
		},
		tapPb.TapEvent_OUTBOUND,
	)
	event2 := pkg.CreateTapEvent(
		&tapPb.TapEvent_Http{
			Event: &tapPb.TapEvent_Http_ResponseEnd_{
				ResponseEnd: &tapPb.TapEvent_Http_ResponseEnd{
					Id: &tapPb.TapEvent_Http_StreamId{
						Base: 1,
					},
					Eos: &metricsPb.Eos{
						End: &metricsPb.Eos_GrpcStatusCode{GrpcStatusCode: 666},
					},
					SinceRequestInit: &duration.Duration{
						Seconds: 10,
					},
					SinceResponseInit: &duration.Duration{
						Seconds: 100,
					},
					ResponseBytes: 1337,
					Trailers: &metricsPb.Headers{
						Headers: []*metricsPb.Headers_Header{
							{
								Name: "trailer-name",
								Value: &metricsPb.Headers_Header_ValueBin{
									ValueBin: []byte("header-value-bin"),
								},
							},
						},
					},
				},
			},
		},
		map[string]string{},
		tapPb.TapEvent_OUTBOUND,
	)
	kubeAPI, err := k8s.NewFakeAPI()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	ts := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			for _, event := range []*tapPb.TapEvent{event1, event2} {
				err = protohttp.WriteProtoToHTTPResponse(w, event)
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
			}
		}),
	)
	defer ts.Close()
	kubeAPI.Config.Host = ts.URL

	options := newTapOptions()
	options.output = output

	writer := bytes.NewBufferString("")
	err = requestTapByResourceFromAPI(context.Background(), writer, kubeAPI, req, options)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	var goldenFilePath string
	switch options.output {
	case wideOutput:
		goldenFilePath = "testdata/tap_busy_output_wide.golden"
	case jsonOutput:
		goldenFilePath = "testdata/tap_busy_output_json.golden"
	default:
		goldenFilePath = "testdata/tap_busy_output.golden"
	}

	goldenFileBytes, err := ioutil.ReadFile(goldenFilePath)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	expectedContent := string(goldenFileBytes)
	actual := writer.String()
	if expectedContent != actual {
		t.Fatalf("Expected function to render:\n%s\bbut got:\n%s", expectedContent, actual)
	}
}

func TestRequestTapByResourceFromAPI(t *testing.T) {

	ctx := context.Background()
	t.Run("Should render busy response if everything went well", func(t *testing.T) {
		busyTest(t, "")
	})

	t.Run("Should render wide busy response if everything went well", func(t *testing.T) {
		busyTest(t, "wide")
	})

	t.Run("Should render JSON busy response if everything went well", func(t *testing.T) {
		busyTest(t, "json")
	})

	t.Run("Should render empty response if no events returned", func(t *testing.T) {
		resourceType := k8s.Pod
		params := pkg.TapRequestParams{
			Resource:  resourceType + "/" + targetName,
			Scheme:    "https",
			Method:    "GET",
			Authority: "localhost",
			Path:      "/some/path",
		}

		req, err := pkg.BuildTapByResourceRequest(params)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		kubeAPI, err := k8s.NewFakeAPI()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		ts := httptest.NewServer(http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {}),
		)
		defer ts.Close()
		kubeAPI.Config.Host = ts.URL

		options := newTapOptions()
		writer := bytes.NewBufferString("")
		err = requestTapByResourceFromAPI(ctx, writer, kubeAPI, req, options)
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
		resourceType := k8s.Pod
		params := pkg.TapRequestParams{
			Resource:  resourceType + "/" + targetName,
			Scheme:    "https",
			Method:    "GET",
			Authority: "localhost",
			Path:      "/some/path",
		}

		req, err := pkg.BuildTapByResourceRequest(params)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		kubeAPI, err := k8s.NewFakeAPI()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		options := newTapOptions()
		writer := bytes.NewBufferString("")
		err = requestTapByResourceFromAPI(ctx, writer, kubeAPI, req, options)
		if err == nil {
			t.Fatalf("Expecting error, got nothing but output [%s]", writer.String())
		}
	})
}

func TestEventToString(t *testing.T) {
	toTapEvent := func(httpEvent *tapPb.TapEvent_Http) *tapPb.TapEvent {
		streamID := &tapPb.TapEvent_Http_StreamId{
			Base:   7,
			Stream: 8,
		}

		switch httpEvent.Event.(type) {
		case *tapPb.TapEvent_Http_RequestInit_:
			httpEvent.GetRequestInit().Id = streamID
		case *tapPb.TapEvent_Http_ResponseInit_:
			httpEvent.GetResponseInit().Id = streamID
		case *tapPb.TapEvent_Http_ResponseEnd_:
			httpEvent.GetResponseEnd().Id = streamID
		}

		return &tapPb.TapEvent{
			ProxyDirection: tapPb.TapEvent_OUTBOUND,
			Source: &netPb.TcpAddress{
				Ip:   addr.PublicIPV4(1, 2, 3, 4),
				Port: 5555,
			},
			Destination: &netPb.TcpAddress{
				Ip:   addr.PublicIPV4(2, 3, 4, 5),
				Port: 6666,
			},
			Event: &tapPb.TapEvent_Http_{Http: httpEvent},
		}
	}

	t.Run("Converts HTTP request init event to string", func(t *testing.T) {
		event := toTapEvent(&tapPb.TapEvent_Http{
			Event: &tapPb.TapEvent_Http_RequestInit_{
				RequestInit: &tapPb.TapEvent_Http_RequestInit{
					Method: &metricsPb.HttpMethod{
						Type: &metricsPb.HttpMethod_Registered_{
							Registered: metricsPb.HttpMethod_POST,
						},
					},
					Scheme: &metricsPb.Scheme{
						Type: &metricsPb.Scheme_Registered_{
							Registered: metricsPb.Scheme_HTTPS,
						},
					},
					Authority: "hello.default:7777",
					Path:      "/hello.v1.HelloService/Hello",
				},
			},
		})

		expectedOutput := "req id=7:8 proxy=out src=1.2.3.4:5555 dst=2.3.4.5:6666 tls= :method=POST :authority=hello.default:7777 :path=/hello.v1.HelloService/Hello"
		output := renderTapEvent(event, "")
		if output != expectedOutput {
			t.Fatalf("Expecting command output to be [%s], got [%s]", expectedOutput, output)
		}
	})

	t.Run("Converts HTTP response init event to string", func(t *testing.T) {
		event := toTapEvent(&tapPb.TapEvent_Http{
			Event: &tapPb.TapEvent_Http_ResponseInit_{
				ResponseInit: &tapPb.TapEvent_Http_ResponseInit{
					SinceRequestInit: &duration.Duration{Nanos: 999000},
					HttpStatus:       http.StatusOK,
				},
			},
		})

		expectedOutput := "rsp id=7:8 proxy=out src=1.2.3.4:5555 dst=2.3.4.5:6666 tls= :status=200 latency=999µs"
		output := renderTapEvent(event, "")
		if output != expectedOutput {
			t.Fatalf("Expecting command output to be [%s], got [%s]", expectedOutput, output)
		}
	})

	t.Run("Converts gRPC response end event to string", func(t *testing.T) {
		event := toTapEvent(&tapPb.TapEvent_Http{
			Event: &tapPb.TapEvent_Http_ResponseEnd_{
				ResponseEnd: &tapPb.TapEvent_Http_ResponseEnd{
					SinceRequestInit:  &duration.Duration{Nanos: 999000},
					SinceResponseInit: &duration.Duration{Nanos: 888000},
					ResponseBytes:     111,
					Eos: &metricsPb.Eos{
						End: &metricsPb.Eos_GrpcStatusCode{GrpcStatusCode: uint32(codes.OK)},
					},
				},
			},
		})

		expectedOutput := "end id=7:8 proxy=out src=1.2.3.4:5555 dst=2.3.4.5:6666 tls= grpc-status=OK duration=888µs response-length=111B"
		output := renderTapEvent(event, "")
		if output != expectedOutput {
			t.Fatalf("Expecting command output to be [%s], got [%s]", expectedOutput, output)
		}
	})

	t.Run("Converts HTTP response end event with reset error code to string", func(t *testing.T) {
		event := toTapEvent(&tapPb.TapEvent_Http{
			Event: &tapPb.TapEvent_Http_ResponseEnd_{
				ResponseEnd: &tapPb.TapEvent_Http_ResponseEnd{
					SinceRequestInit:  &duration.Duration{Nanos: 999000},
					SinceResponseInit: &duration.Duration{Nanos: 888000},
					ResponseBytes:     111,
					Eos: &metricsPb.Eos{
						End: &metricsPb.Eos_ResetErrorCode{ResetErrorCode: 123},
					},
				},
			},
		})

		expectedOutput := "end id=7:8 proxy=out src=1.2.3.4:5555 dst=2.3.4.5:6666 tls= reset-error=123 duration=888µs response-length=111B"
		output := renderTapEvent(event, "")
		if output != expectedOutput {
			t.Fatalf("Expecting command output to be [%s], got [%s]", expectedOutput, output)
		}
	})

	t.Run("Converts HTTP response end event with empty EOS context string", func(t *testing.T) {
		event := toTapEvent(&tapPb.TapEvent_Http{
			Event: &tapPb.TapEvent_Http_ResponseEnd_{
				ResponseEnd: &tapPb.TapEvent_Http_ResponseEnd{
					SinceRequestInit:  &duration.Duration{Nanos: 999000},
					SinceResponseInit: &duration.Duration{Nanos: 888000},
					ResponseBytes:     111,
					Eos:               &metricsPb.Eos{},
				},
			},
		})

		expectedOutput := "end id=7:8 proxy=out src=1.2.3.4:5555 dst=2.3.4.5:6666 tls= duration=888µs response-length=111B"
		output := renderTapEvent(event, "")
		if output != expectedOutput {
			t.Fatalf("Expecting command output to be [%s], got [%s]", expectedOutput, output)
		}
	})

	t.Run("Converts HTTP response end event without EOS context string", func(t *testing.T) {
		event := toTapEvent(&tapPb.TapEvent_Http{
			Event: &tapPb.TapEvent_Http_ResponseEnd_{
				ResponseEnd: &tapPb.TapEvent_Http_ResponseEnd{
					SinceRequestInit:  &duration.Duration{Nanos: 999000},
					SinceResponseInit: &duration.Duration{Nanos: 888000},
					ResponseBytes:     111,
				},
			},
		})

		expectedOutput := "end id=7:8 proxy=out src=1.2.3.4:5555 dst=2.3.4.5:6666 tls= duration=888µs response-length=111B"
		output := renderTapEvent(event, "")
		if output != expectedOutput {
			t.Fatalf("Expecting command output to be [%s], got [%s]", expectedOutput, output)
		}
	})

	t.Run("Handles unknown event types", func(t *testing.T) {
		event := toTapEvent(&tapPb.TapEvent_Http{})

		expectedOutput := "unknown proxy=out src=1.2.3.4:5555 dst=2.3.4.5:6666 tls="
		output := renderTapEvent(event, "")
		if output != expectedOutput {
			t.Fatalf("Expecting command output to be [%s], got [%s]", expectedOutput, output)
		}
	})
}
