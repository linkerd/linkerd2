package public

import (
	"bufio"
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	pb "github.com/runconduit/conduit/controller/gen/public"
)

type mockTransport struct {
	responseToReturn *http.Response
	requestSent      *http.Request
	errorToReturn    error
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.requestSent = req
	return m.responseToReturn, m.errorToReturn
}

func TestNewInternalClient(t *testing.T) {
	t.Run("Makes a well-formed request over the Kubernetes public API", func(t *testing.T) {
		mockTransport := &mockTransport{}
		mockTransport.responseToReturn = &http.Response{
			StatusCode: 500,
			Body:       ioutil.NopCloser(strings.NewReader("body")),
		}
		mockHttpClient := &http.Client{
			Transport: mockTransport,
		}

		apiURL := &url.URL{
			Scheme: "http",
			Host:   "some-hostname",
			Path:   "/",
		}

		client, err := newClient(apiURL, mockHttpClient)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		_, err = client.Version(context.Background(), &pb.Empty{})

		expectedUrlRequested := "http://some-hostname/api/v1/Version"
		actualUrlRequested := mockTransport.requestSent.URL.String()
		if actualUrlRequested != expectedUrlRequested {
			t.Fatalf("Expected request to URL [%v], but got [%v]", expectedUrlRequested, actualUrlRequested)
		}
	})
}

func TestFromByteStreamToProtocolBuffers(t *testing.T) {

	t.Run("Correctly marshalls an valid object", func(t *testing.T) {
		versionInfo := pb.VersionInfo{
			GoVersion:      "1.9.1",
			BuildDate:      "2017.11.17",
			ReleaseVersion: "1.2.3",
		}

		var protobufMessageToBeFilledWithData pb.VersionInfo
		reader := bufferedReader(t, &versionInfo)

		err := fromByteStreamToProtocolBuffers(reader, &protobufMessageToBeFilledWithData)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if protobufMessageToBeFilledWithData != versionInfo {
			t.Fatalf("mismatch, %+v != %+v", protobufMessageToBeFilledWithData, versionInfo)
		}
	})

	t.Run("Correctly marshalls a large byte array", func(t *testing.T) {
		series := pb.MetricSeries{
			Name:       pb.MetricName_REQUEST_RATE,
			Metadata:   &pb.MetricMetadata{},
			Datapoints: make([]*pb.MetricDatapoint, 0),
		}

		numberOfDatapointsInMessage := 400
		for i := 0; i < numberOfDatapointsInMessage; i++ {
			datapoint := pb.MetricDatapoint{
				Value:       &pb.MetricValue{Value: &pb.MetricValue_Gauge{Gauge: float64(i)}},
				TimestampMs: time.Now().UnixNano() / int64(time.Millisecond),
			}
			series.Datapoints = append(series.Datapoints, &datapoint)
		}
		reader := bufferedReader(t, &series)

		protobufMessageToBeFilledWithData := &pb.MetricSeries{}
		err := fromByteStreamToProtocolBuffers(reader, protobufMessageToBeFilledWithData)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		actualNumberOfDatapointsMarshalled := len(protobufMessageToBeFilledWithData.Datapoints)
		if actualNumberOfDatapointsMarshalled != numberOfDatapointsInMessage {
			t.Fatalf("Expected marshalling to return [%d] datapoints, but got [%d]", numberOfDatapointsInMessage, actualNumberOfDatapointsMarshalled)
		}
	})

	t.Run("When byte array contains error, treats stream as regular protobuf object", func(t *testing.T) {
		apiError := pb.ApiError{Error: "an error occurred"}

		var protobufMessageToBeFilledWithData pb.ApiError
		reader := bufferedReader(t, &apiError)
		err := fromByteStreamToProtocolBuffers(reader, &protobufMessageToBeFilledWithData)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expectedErrorMessage := apiError.Error
		actualErrorMessage := protobufMessageToBeFilledWithData.Error
		if actualErrorMessage != expectedErrorMessage {
			t.Fatalf("Expected object to contain message [%s], but got [%s]", expectedErrorMessage, actualErrorMessage)
		}
	})

	t.Run("Returns error if byte stream contains wrong object", func(t *testing.T) {
		versionInfo := &pb.VersionInfo{
			GoVersion:      "1.9.1",
			BuildDate:      "2017.11.17",
			ReleaseVersion: "1.2.3",
		}

		reader := bufferedReader(t, versionInfo)

		protobufMessageToBeFilledWithData := &pb.MetricSeries{}
		err := fromByteStreamToProtocolBuffers(reader, protobufMessageToBeFilledWithData)
		if err == nil {
			t.Fatal("Expecting error, got nothing")
		}
	})
}

func bufferedReader(t *testing.T, msg proto.Message) *bufio.Reader {
	msgBytes, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	payload, err := serializeAsPayload(msgBytes)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	return bufio.NewReader(bytes.NewReader(payload))
}
