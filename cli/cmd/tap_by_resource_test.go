package cmd

import (
	"bytes"
	"errors"
	"io/ioutil"
	"testing"

	google_protobuf "github.com/golang/protobuf/ptypes/duration"
	"github.com/runconduit/conduit/controller/api/public"
	common "github.com/runconduit/conduit/controller/gen/common"
	"github.com/runconduit/conduit/pkg/k8s"
)

func TestRequestTapByResourceFromAPI(t *testing.T) {
	t.Run("Should render busy response if everything went well", func(t *testing.T) {
		resourceType := k8s.KubernetesPods
		targetName := "pod-666"
		scheme := "https"
		method := "GET"
		authority := "localhost"
		path := "/some/path"

		req, err := buildTapByResourceRequest(
			[]string{resourceType, targetName},
			"", "", "", 0,
			scheme, method, authority, path,
		)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

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
					Eos: &common.Eos{
						End: &common.Eos_GrpcStatusCode{GrpcStatusCode: 666},
					},
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
		resourceType := k8s.KubernetesPods
		targetName := "pod-666"
		scheme := "https"
		method := "GET"
		authority := "localhost"
		path := "/some/path"

		req, err := buildTapByResourceRequest(
			[]string{resourceType, targetName},
			"", "", "", 0,
			scheme, method, authority, path,
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
		resourceType := k8s.KubernetesPods
		targetName := "pod-666"
		scheme := "https"
		method := "GET"
		authority := "localhost"
		path := "/some/path"

		req, err := buildTapByResourceRequest(
			[]string{resourceType, targetName},
			"", "", "", 0,
			scheme, method, authority, path,
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

// TODO: re-introduce TestEventToString and friends from tap_test.go
