package conduit

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	pb "github.com/runconduit/conduit/controller/gen/public"
)

func TestFromByteStreamToProtocolBuffers(t *testing.T) {
	t.Run("Correctly marshalls an valid object", func(t *testing.T) {
		versionInfo := pb.VersionInfo{
			GoVersion:      "1.9.1",
			BuildDate:      "2017.11.17",
			ReleaseVersion: "1.2.3",
		}

		var protobufMessageToBeFilledWithData pb.VersionInfo
		reader := bufferedReader(t, &versionInfo)

		err := fromByteStreamToProtocolBuffers(reader, "", &protobufMessageToBeFilledWithData)
		if err != nil {
			t.Fatal(err.Error())
		}

		if protobufMessageToBeFilledWithData != versionInfo {
			t.Fatalf("mismatch, %+v != %+v", protobufMessageToBeFilledWithData, versionInfo)
		}
	})

	t.Run("Correctly marshalls a large byte arrey", func(t *testing.T) {
		series := pb.MetricSeries{
			Name:       pb.MetricName_REQUEST_RATE,
			Metadata:   &pb.MetricMetadata{},
			Datapoints: make([]*pb.MetricDatapoint, 0),
		}

		numberOfDatapointsInMessage := 1000
		for i := 0; i < numberOfDatapointsInMessage; i++ {
			datapoint := pb.MetricDatapoint{
				Value:       &pb.MetricValue{Value: &pb.MetricValue_Gauge{Gauge: float64(i)}},
				TimestampMs: time.Now().UnixNano() / int64(time.Millisecond),
			}
			series.Datapoints = append(series.Datapoints, &datapoint)
		}

		var protobufMessageToBeFilledWithData pb.MetricSeries
		reader := bufferedReader(t, &series)
		err := fromByteStreamToProtocolBuffers(reader, "", &protobufMessageToBeFilledWithData)
		if err != nil {
			t.Fatal(err.Error())
		}

		actualNumberOfDatapointsMarshalled := len(protobufMessageToBeFilledWithData.Datapoints)
		if actualNumberOfDatapointsMarshalled != numberOfDatapointsInMessage {
			t.Fatalf("Expected marshalling to return [%d] datapoints, but got [%d]", numberOfDatapointsInMessage, actualNumberOfDatapointsMarshalled)
		}
	})

	t.Run("When error, uses both byte array and supplied message to return error", func(t *testing.T) {
		apiError := pb.ApiError{Error: "an error occurred"}

		var protobufMessageToBeFilledWithData pb.VersionInfo
		reader := bufferedReader(t, &apiError)
		err := fromByteStreamToProtocolBuffers(reader, "Bad Request", &protobufMessageToBeFilledWithData)
		if err == nil {
			t.Fatal("expected error")
		}

		expectedErrorMessage := "Bad Request: an error occurred"
		actualErrorMessage := err.Error()
		if actualErrorMessage != expectedErrorMessage {
			t.Fatalf("Expecting returned error message to be [%s], but got [%s]", expectedErrorMessage, actualErrorMessage)
		}
	})

	t.Run("When byte array contains error but no message was supplied, treats stream as regular object", func(t *testing.T) {
		apiError := pb.ApiError{Error: "an error occurred"}

		var protobufMessageToBeFilledWithData pb.ApiError
		reader := bufferedReader(t, &apiError)
		err := fromByteStreamToProtocolBuffers(reader, "", &protobufMessageToBeFilledWithData)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expectedErrorMessage := apiError.Error
		actualErrorMessage := protobufMessageToBeFilledWithData.Error
		if actualErrorMessage != expectedErrorMessage {
			t.Fatalf("Expected object to contain message [%s], but got [%s]", expectedErrorMessage, actualErrorMessage)
		}
	})

	t.Run("When byte array does not contain error but a message was supplied, returns error", func(t *testing.T) {
		versionInfo := pb.VersionInfo{
			GoVersion:      "1.9.1",
			BuildDate:      "2017.11.17",
			ReleaseVersion: "1.2.3",
		}

		expectedErrorMessage := "supplied error message here"
		var protobufMessageToBeFilledWithData pb.VersionInfo
		reader := bufferedReader(t, &versionInfo)

		err := fromByteStreamToProtocolBuffers(reader, expectedErrorMessage, &protobufMessageToBeFilledWithData)
		if err == nil {
			t.Fatal("Expecting error, got nothing")
		}

		actualErrorMessage := err.Error()
		if !strings.Contains(actualErrorMessage, expectedErrorMessage) {
			t.Fatalf("Expected object to contain message [%s], but got [%s]", expectedErrorMessage, actualErrorMessage)
		}
	})

	t.Run("Correctly marshalls an valid object", func(t *testing.T) {
		versionInfo := pb.VersionInfo{
			GoVersion:      "1.9.1",
			BuildDate:      "2017.11.17",
			ReleaseVersion: "1.2.3",
		}

		var protobufMessageToBeFilledWithData pb.MetricSeries
		reader := bufferedReader(t, &versionInfo)

		err := fromByteStreamToProtocolBuffers(reader, "", &protobufMessageToBeFilledWithData)
		if err == nil {
			t.Fatal("Expecting error, got nothing")
		}
	})
}

func bufferedReader(t *testing.T, msg proto.Message) *bufio.Reader {
	msgBytes, err := proto.Marshal(msg)
	if err != nil {
		t.Fatal(err.Error())
	}
	sizeBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(sizeBytes, uint32(len(msgBytes)))
	return bufio.NewReader(bytes.NewReader(append(sizeBytes, msgBytes...)))
}
