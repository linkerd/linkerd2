package public

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/gogo/protobuf/proto"

	pb "github.com/runconduit/conduit/controller/gen/public"
)

type stubResponseWriter struct {
	body    *bytes.Buffer
	headers http.Header
}

func (w *stubResponseWriter) Header() http.Header {
	return w.headers
}

func (w *stubResponseWriter) Write(p []byte) (int, error) {
	n, err := w.body.Write(p)
	fmt.Print(n)
	return n, err
}

func (w *stubResponseWriter) WriteHeader(int) {}

func (w *stubResponseWriter) Flush() {}

type nonStreamingResponseWriter struct {
}

func (w *nonStreamingResponseWriter) Header() http.Header { return nil }

func (w *nonStreamingResponseWriter) Write(p []byte) (int, error) { return -1, nil }

func (w *nonStreamingResponseWriter) WriteHeader(int) {}

func newStubResponseWriter() *stubResponseWriter {
	return &stubResponseWriter{
		headers: make(http.Header),
		body:    bytes.NewBufferString(""),
	}
}

func TestHttpRequestToProto(t *testing.T) {
	someUrl := "www.example.org/something"
	someMethod := http.MethodPost

	t.Run("Given a valid request, serializes its contents into protobuf object", func(t *testing.T) {
		expectedProtoMessage := pb.Pod{
			Name:                "some-name",
			PodIP:               "some-name",
			Deployment:          "some-name",
			Status:              "some-name",
			Added:               false,
			ControllerNamespace: "some-name",
			ControlPlane:        false,
		}
		payload, err := proto.Marshal(&expectedProtoMessage)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		req, err := http.NewRequest(someMethod, someUrl, bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		var actualProtoMessage pb.Pod
		err = httpRequestToProto(req, &actualProtoMessage)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if actualProtoMessage != expectedProtoMessage {
			t.Fatalf("Expected request to be [%v], but got [%v]", actualProtoMessage, expectedProtoMessage)
		}
	})

	t.Run("Given a broken request, returns http error", func(t *testing.T) {
		var actualProtoMessage pb.Pod

		req, err := http.NewRequest(someMethod, someUrl, strings.NewReader("not really protobuf"))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		err = httpRequestToProto(req, &actualProtoMessage)
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}

		if httpErr, ok := err.(httpError); ok {
			expectedStatusCode := http.StatusBadRequest
			if httpErr.Code != expectedStatusCode || httpErr.WrappedError == nil {
				t.Fatalf("Expected error status to be [%d] and contain wrapper error, got status [%d] and error [%v]", expectedStatusCode, httpErr.Code, httpErr.WrappedError)
			}
		} else {
			t.Fatalf("Expected error to be httpError, got: %v", err)
		}
	})
}

func TestWriteErrorToHttpResponse(t *testing.T) {
	t.Run("Writes generic error correctly to response", func(t *testing.T) {
		expectedErrorStatusCode := defaultHttpErrorStatusCode

		responseWriter := newStubResponseWriter()
		genericError := errors.New("expected generic error")

		writeErrorToHttpResponse(responseWriter, genericError)

		assertResponseHasProtobufContentType(t, responseWriter)

		actualErrorStatusCode := responseWriter.headers.Get(ErrorHeader)
		if actualErrorStatusCode != http.StatusText(expectedErrorStatusCode) {
			t.Fatalf("Expecting response to have status code [%d], got [%s]", expectedErrorStatusCode, actualErrorStatusCode)
		}

		payloadRead, err := deserializePayloadFromReader(bufio.NewReader(bytes.NewReader(responseWriter.body.Bytes())))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expectedErrorPayload := pb.ApiError{Error: genericError.Error()}
		var actualErrorPayload pb.ApiError
		err = proto.Unmarshal(payloadRead, &actualErrorPayload)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if actualErrorPayload != expectedErrorPayload {
			t.Fatalf("Expecting error to be serialised as [%v], but got [%v]", expectedErrorPayload, actualErrorPayload)
		}
	})

	t.Run("Writes http specific error correctly to response", func(t *testing.T) {
		expectedErrorStatusCode := http.StatusBadGateway
		responseWriter := newStubResponseWriter()
		httpError := httpError{
			WrappedError: errors.New("expected to be wrapped"),
			Code:         http.StatusBadGateway,
		}

		writeErrorToHttpResponse(responseWriter, httpError)

		assertResponseHasProtobufContentType(t, responseWriter)

		actualErrorStatusCode := responseWriter.headers.Get(ErrorHeader)
		if actualErrorStatusCode != http.StatusText(expectedErrorStatusCode) {
			t.Fatalf("Expecting response to have status code [%d], got [%s]", expectedErrorStatusCode, actualErrorStatusCode)
		}

		payloadRead, err := deserializePayloadFromReader(bufio.NewReader(bytes.NewReader(responseWriter.body.Bytes())))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expectedErrorPayload := pb.ApiError{Error: httpError.WrappedError.Error()}
		var actualErrorPayload pb.ApiError
		err = proto.Unmarshal(payloadRead, &actualErrorPayload)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if actualErrorPayload != expectedErrorPayload {
			t.Fatalf("Expecting error to be serialised as [%v], but got [%v]", expectedErrorPayload, actualErrorPayload)
		}
	})
}

func TestWriteProtoToHttpResponse(t *testing.T) {
	t.Run("Writes valid payload", func(t *testing.T) {
		expectedMessage := pb.VersionInfo{
			ReleaseVersion: "0.0.1",
			BuildDate:      "02/21/1983",
			GoVersion:      "10.2.45",
		}

		responseWriter := newStubResponseWriter()
		err := writeProtoToHttpResponse(responseWriter, &expectedMessage)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		assertResponseHasProtobufContentType(t, responseWriter)

		payloadRead, err := deserializePayloadFromReader(bufio.NewReader(bytes.NewReader(responseWriter.body.Bytes())))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		var actualMessage pb.VersionInfo
		err = proto.Unmarshal(payloadRead, &actualMessage)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if expectedMessage != actualMessage {
			t.Fatalf("Expected response body to contain message [%v], but got [%v]", expectedMessage, actualMessage)
		}
	})
}

func TestPayloadSize(t *testing.T) {
	t.Run("Can write and read message correctly based on payload size correct payload size to message", func(t *testing.T) {
		expectedMessage := "this is the message"

		messageWithSize, err := serializeAsPayload([]byte(expectedMessage))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		messageWithSomeNoise := append(messageWithSize, []byte("this is noise and should not be read")...)

		actualMessage, err := deserializePayloadFromReader(bufio.NewReader(bytes.NewReader(messageWithSomeNoise)))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if string(actualMessage) != expectedMessage {
			t.Fatalf("Expecting payload to contain message [%s], but it had [%s]", expectedMessage, actualMessage)
		}
	})

	t.Run("Can write and read  marshalled protobuf messages", func(t *testing.T) {
		seriesToReturn := make([]*pb.MetricSeries, 0)
		for i := 0; i < 351; i++ {
			seriesToReturn = append(seriesToReturn, &pb.MetricSeries{Name: pb.MetricName_LATENCY})
		}

		expectedMessage := &pb.MetricResponse{
			Metrics: seriesToReturn,
		}

		expectedReadArray, err := proto.Marshal(expectedMessage)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		serialized, err := serializeAsPayload(expectedReadArray)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		reader := bufio.NewReader(bytes.NewReader(serialized))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		actualReadArray, err := deserializePayloadFromReader(reader)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if !reflect.DeepEqual(actualReadArray, expectedReadArray) {
			n := len(actualReadArray)
			xor := make([]byte, n)
			for i := 0; i < n; i++ {
				xor[i] = actualReadArray[i] ^ expectedReadArray[i]
			}
			t.Fatalf("Expecting read byte array to be equal to written byte array, but they were different. xor: [%v]", xor)
		}

		actualMessage := &pb.MetricResponse{}
		err = proto.Unmarshal(actualReadArray, actualMessage)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if !reflect.DeepEqual(actualMessage, expectedMessage) {
			t.Fatalf("Expecting payload to contain message [%s], but it had [%s]", expectedMessage, actualMessage)
		}
	})
}

func TestNewStreamingWriter(t *testing.T) {
	t.Run("Returns a streaming writer if the ResponseWriter is compatible with streaming", func(t *testing.T) {
		rawWriter := newStubResponseWriter()
		flushableWriter, err := newStreamingWriter(rawWriter)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if flushableWriter != rawWriter {
			t.Fatalf("Expected to return same instance of writer")
		}

		header := "Connection"
		expectedValue := "keep-alive"
		actualValue := rawWriter.Header().Get(header)
		if actualValue != expectedValue {
			t.Fatalf("Expected header [%s] to be set to [%s], but was [%s]", header, expectedValue, actualValue)
		}

		header = "Transfer-Encoding"
		expectedValue = "chunked"
		actualValue = rawWriter.Header().Get(header)
		if actualValue != expectedValue {
			t.Fatalf("Expected header [%s] to be set to [%s], but was [%s]", header, expectedValue, actualValue)
		}
	})

	t.Run("Returns an error if writer doesnt support streaming", func(t *testing.T) {
		_, err := newStreamingWriter(&nonStreamingResponseWriter{})
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}
	})
}

func assertResponseHasProtobufContentType(t *testing.T, responseWriter *stubResponseWriter) {
	actualContentType := responseWriter.headers.Get(contentTypeHeader)
	expectedContentType := protobufContentType
	if actualContentType != expectedContentType {
		t.Fatalf("Expected content-type to be [%s], but got [%s]", expectedContentType, actualContentType)
	}
}
