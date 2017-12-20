package cmd

import (
	"net/http"
	"testing"

	"github.com/golang/protobuf/ptypes/duration"
	common "github.com/runconduit/conduit/controller/gen/common"
	"github.com/runconduit/conduit/controller/util"
	"google.golang.org/grpc/codes"
)

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
		output := eventToString(event)
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
		output := eventToString(event)
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
		output := eventToString(event)
		if output != expectedOutput {
			t.Fatalf("Expecting command output to be [%s], got [%s]", expectedOutput, output)
		}
	})

	t.Run("Handles unknown event types", func(t *testing.T) {
		event := toTapEvent(&common.TapEvent_Http{})

		expectedOutput := "unknown src=1.2.3.4:5555 dst=2.3.4.5:6666"
		output := eventToString(event)
		if output != expectedOutput {
			t.Fatalf("Expecting command output to be [%s], got [%s]", expectedOutput, output)
		}
	})
}
