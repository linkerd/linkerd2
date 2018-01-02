package conduit

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	pb "github.com/runconduit/conduit/controller/gen/public"
)

func TestClientUnmarshal(t *testing.T) {
	versionInfo := pb.VersionInfo{
		GoVersion:      "1.9.1",
		BuildDate:      "2017.11.17",
		ReleaseVersion: "1.2.3",
	}

	var unmarshaled pb.VersionInfo
	reader := bufferedReader(t, &versionInfo)
	err := clientUnmarshal(reader, "", &unmarshaled)
	if err != nil {
		t.Fatal(err.Error())
	}

	if unmarshaled != versionInfo {
		t.Fatalf("mismatch, %+v != %+v", unmarshaled, versionInfo)
	}
}

func TestClientUnmarshalLargeMessage(t *testing.T) {
	series := pb.MetricSeries{
		Name:       pb.MetricName_REQUEST_RATE,
		Metadata:   &pb.MetricMetadata{},
		Datapoints: make([]*pb.MetricDatapoint, 0),
	}

	for i := float64(0); i < 1000; i++ {
		datapoint := pb.MetricDatapoint{
			Value:       &pb.MetricValue{Value: &pb.MetricValue_Gauge{Gauge: i}},
			TimestampMs: time.Now().UnixNano() / int64(time.Millisecond),
		}
		series.Datapoints = append(series.Datapoints, &datapoint)
	}

	var unmarshaled pb.MetricSeries
	reader := bufferedReader(t, &series)
	err := clientUnmarshal(reader, "", &unmarshaled)
	if err != nil {
		t.Fatal(err.Error())
	}

	if len(unmarshaled.Datapoints) != 1000 {
		t.Fatal("missing datapoints")
	}
}

func TestClientUnmarshalApiErrorAsError(t *testing.T) {
	apiError := pb.ApiError{Error: "an error occurred"}

	var unmarshaled pb.VersionInfo
	reader := bufferedReader(t, &apiError)
	err := clientUnmarshal(reader, "Bad Request", &unmarshaled)
	if err == nil {
		t.Fatal("expected error")
	}

	expected := "Bad Request: an error occurred"
	if err.Error() != expected {
		t.Fatalf("mismatch, %s != %s", err.Error(), expected)
	}
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
