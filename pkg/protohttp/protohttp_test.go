package protohttp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/golang/protobuf/proto"
	metricsPb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	someURL := "https://www.example.org/something"
	someMethod := http.MethodPost

	t.Run("Given a valid request, serializes its contents into protobuf object", func(t *testing.T) {
		expectedProtoMessage := metricsPb.Pod{
			Name:                "some-name",
			PodIP:               "some-name",
			Owner:               &metricsPb.Pod_Deployment{Deployment: "some-name"},
			Status:              "some-name",
			Added:               false,
			ControllerNamespace: "some-name",
			ControlPlane:        false,
		}
		payload, err := proto.Marshal(&expectedProtoMessage)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		req, err := http.NewRequest(someMethod, someURL, bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		var actualProtoMessage metricsPb.Pod
		err = HTTPRequestToProto(req, &actualProtoMessage)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if !proto.Equal(&actualProtoMessage, &expectedProtoMessage) {
			t.Fatalf("Expected request to be [%s], but got [%s]", expectedProtoMessage.String(), actualProtoMessage.String())
		}
	})

	t.Run("Given a broken request, returns http error", func(t *testing.T) {
		var actualProtoMessage metricsPb.Pod

		req, err := http.NewRequest(someMethod, someURL, strings.NewReader("not really protobuf"))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		err = HTTPRequestToProto(req, &actualProtoMessage)
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}

		if httpErr, ok := err.(HTTPError); ok {
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
		expectedErrorStatusCode := defaultHTTPErrorStatusCode

		responseWriter := newStubResponseWriter()
		genericError := errors.New("expected generic error")

		WriteErrorToHTTPResponse(responseWriter, genericError)

		assertResponseHasProtobufContentType(t, responseWriter)

		actualErrorStatusCode := responseWriter.headers.Get(errorHeader)
		if actualErrorStatusCode != http.StatusText(expectedErrorStatusCode) {
			t.Fatalf("Expecting response to have status code [%d], got [%s]", expectedErrorStatusCode, actualErrorStatusCode)
		}

		payloadRead, err := deserializePayloadFromReader(bufio.NewReader(bytes.NewReader(responseWriter.body.Bytes())))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expectedErrorPayload := metricsPb.ApiError{Error: genericError.Error()}
		var actualErrorPayload metricsPb.ApiError
		err = proto.Unmarshal(payloadRead, &actualErrorPayload)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if !proto.Equal(&actualErrorPayload, &expectedErrorPayload) {
			t.Fatalf("Expecting error to be serialized as [%s], but got [%s]", expectedErrorPayload.String(), actualErrorPayload.String())
		}
	})

	t.Run("Writes http specific error correctly to response", func(t *testing.T) {
		expectedErrorStatusCode := http.StatusBadGateway
		responseWriter := newStubResponseWriter()
		httpError := HTTPError{
			WrappedError: errors.New("expected to be wrapped"),
			Code:         http.StatusBadGateway,
		}

		WriteErrorToHTTPResponse(responseWriter, httpError)

		assertResponseHasProtobufContentType(t, responseWriter)

		actualErrorStatusCode := responseWriter.headers.Get(errorHeader)
		if actualErrorStatusCode != http.StatusText(expectedErrorStatusCode) {
			t.Fatalf("Expecting response to have status code [%d], got [%s]", expectedErrorStatusCode, actualErrorStatusCode)
		}

		payloadRead, err := deserializePayloadFromReader(bufio.NewReader(bytes.NewReader(responseWriter.body.Bytes())))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expectedErrorPayload := metricsPb.ApiError{Error: httpError.WrappedError.Error()}
		var actualErrorPayload metricsPb.ApiError
		err = proto.Unmarshal(payloadRead, &actualErrorPayload)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if !proto.Equal(&actualErrorPayload, &expectedErrorPayload) {
			t.Fatalf("Expecting error to be serialized as [%s], but got [%s]", expectedErrorPayload.String(), actualErrorPayload.String())
		}
	})

	t.Run("Writes gRPC specific error correctly to response", func(t *testing.T) {
		expectedErrorStatusCode := defaultHTTPErrorStatusCode

		responseWriter := newStubResponseWriter()
		expectedErrorMessage := "error message"
		grpcError := status.Errorf(codes.AlreadyExists, expectedErrorMessage)

		WriteErrorToHTTPResponse(responseWriter, grpcError)

		assertResponseHasProtobufContentType(t, responseWriter)

		actualErrorStatusCode := responseWriter.headers.Get(errorHeader)
		if actualErrorStatusCode != http.StatusText(expectedErrorStatusCode) {
			t.Fatalf("Expecting response to have status code [%d], got [%s]", expectedErrorStatusCode, actualErrorStatusCode)
		}

		payloadRead, err := deserializePayloadFromReader(bufio.NewReader(bytes.NewReader(responseWriter.body.Bytes())))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expectedErrorPayload := metricsPb.ApiError{Error: expectedErrorMessage}
		var actualErrorPayload metricsPb.ApiError
		err = proto.Unmarshal(payloadRead, &actualErrorPayload)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if actualErrorPayload.String() != expectedErrorPayload.String() {
			t.Fatalf("Expecting error to be serialized as [%s], but got [%s]", expectedErrorPayload.String(), actualErrorPayload.String())
		}
	})
}

func TestDeserializePayloadFromReader(t *testing.T) {
	t.Run("Can read message correctly based on payload size correct payload size to message", func(t *testing.T) {
		expectedMessage := "this is the message"

		messageWithSize := SerializeAsPayload([]byte(expectedMessage))
		messageWithSomeNoise := append(messageWithSize, []byte("this is noise and should not be read")...)

		actualMessage, err := deserializePayloadFromReader(bufio.NewReader(bytes.NewReader(messageWithSomeNoise)))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if string(actualMessage) != expectedMessage {
			t.Fatalf("Expecting payload to contain message [%s], but it had [%s]", expectedMessage, actualMessage)
		}
	})

	t.Run("Can multiple messages in the same stream", func(t *testing.T) {
		expectedMessage1 := "Hit the road, Jack and don't you come back\n"
		for i := 0; i < 450; i++ {
			expectedMessage1 = expectedMessage1 + fmt.Sprintf("no more (%d), ", i)
		}

		expectedMessage2 := "back street back, alright\n"
		for i := 0; i < 450; i++ {
			expectedMessage2 = expectedMessage2 + fmt.Sprintf("tum (%d), ", i)
		}

		messageWithSize1 := SerializeAsPayload([]byte(expectedMessage1))
		messageWithSize2 := SerializeAsPayload([]byte(expectedMessage2))

		streamWithManyMessages := append(messageWithSize1, messageWithSize2...)
		reader := bufio.NewReader(bytes.NewReader(streamWithManyMessages))

		actualMessage1, err := deserializePayloadFromReader(reader)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		actualMessage2, err := deserializePayloadFromReader(reader)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if string(actualMessage1) != expectedMessage1 {
			t.Fatalf("Expecting payload to contain message:\n%s\nbut it had\n%s", expectedMessage1, actualMessage1)
		}

		if string(actualMessage2) != expectedMessage2 {
			t.Fatalf("Expecting payload to contain message:\n%s\nbut it had\n%s", expectedMessage2, actualMessage2)
		}
	})

	t.Run("Can read byte streams larger than Go's default buffer chunk size", func(t *testing.T) {
		goDefaultChunkSize := 4000
		expectedMessage := "Hit the road, Jack and don't you come back\n"
		for i := 0; i < 450; i++ {
			expectedMessage = expectedMessage + fmt.Sprintf("no more (%d), ", i)
		}

		expectedMessageAsBytes := []byte(expectedMessage)
		lengthOfInputData := len(expectedMessageAsBytes)

		if lengthOfInputData < goDefaultChunkSize {
			t.Fatalf("Test needs data larger than [%d] bytes, currently only [%d] bytes", goDefaultChunkSize, lengthOfInputData)
		}

		payload := SerializeAsPayload(expectedMessageAsBytes)
		actualMessage, err := deserializePayloadFromReader(bufio.NewReader(bytes.NewReader(payload)))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if string(actualMessage) != expectedMessage {
			t.Fatalf("Expecting payload to contain message:\n%s\n, but it had\n%s", expectedMessageAsBytes, actualMessage)
		}
	})

	t.Run("Returns error when message has fewer bytes than declared message size", func(t *testing.T) {
		expectedMessage := "this is the message"

		messageWithSize := SerializeAsPayload([]byte(expectedMessage))
		messageMissingOneCharacter := messageWithSize[:len(expectedMessage)-1]
		_, err := deserializePayloadFromReader(bufio.NewReader(bytes.NewReader(messageMissingOneCharacter)))
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}
	})
}

func TestNewStreamingWriter(t *testing.T) {
	t.Run("Returns a streaming writer if the ResponseWriter is compatible with streaming", func(t *testing.T) {
		rawWriter := newStubResponseWriter()
		flushableWriter, err := NewStreamingWriter(rawWriter)
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

	t.Run("Returns an error if writer does not support streaming", func(t *testing.T) {
		_, err := NewStreamingWriter(&nonStreamingResponseWriter{})
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}
	})
}

func TestCheckIfResponseHasError(t *testing.T) {
	t.Run("returns nil if response doesn't contain linkerd-error header and is 200", func(t *testing.T) {
		response := &http.Response{
			Header:     make(http.Header),
			StatusCode: http.StatusOK,
		}
		err := CheckIfResponseHasError(response)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
	})

	t.Run("returns error in body if response contains linkerd-error header", func(t *testing.T) {
		expectedErrorMessage := "expected error message"
		protoInBytes, err := proto.Marshal(&metricsPb.ApiError{Error: expectedErrorMessage})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		message := SerializeAsPayload(protoInBytes)
		response := &http.Response{
			Header:     make(http.Header),
			Body:       ioutil.NopCloser(bytes.NewReader(message)),
			StatusCode: http.StatusInternalServerError,
		}
		response.Header.Set(errorHeader, "error")

		err = CheckIfResponseHasError(response)
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}

		actualErrorMessage := err.Error()
		if actualErrorMessage != expectedErrorMessage {
			t.Fatalf("Expected error message to be [%s], but it was [%s]", expectedErrorMessage, actualErrorMessage)
		}
	})

	t.Run("returns Kubernetes StatusError if present", func(t *testing.T) {
		statusError := kerrors.NewForbidden(
			schema.GroupResource{Group: "group", Resource: "res"},
			"name", errors.New("test-err"),
		)
		statusError.ErrStatus.Kind = "Status"
		statusError.ErrStatus.APIVersion = "v1"
		j, err := json.Marshal(statusError.ErrStatus)
		if err != nil {
			log.Fatalf("Failed to marshal JSON: %+v", statusError)
		}
		fmt.Printf("J: %+v\n", string(j))

		response := &http.Response{
			Header:     make(http.Header),
			Body:       ioutil.NopCloser(bytes.NewReader(j)),
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
		}

		err = CheckIfResponseHasError(response)
		expectedErr := HTTPError{Code: http.StatusForbidden, WrappedError: statusError}

		if !reflect.DeepEqual(err, expectedErr) {
			t.Fatalf("Expected %s, got %s", expectedErr, err)
		}
	})

	t.Run("returns error if response is not a 200", func(t *testing.T) {
		response := &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Status:     "503 Service Unavailable",
		}

		err := CheckIfResponseHasError(response)
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}

		expectedErrorMessage := "HTTP error, status Code [503] (unexpected API response)"
		actualErrorMessage := err.Error()
		if actualErrorMessage != expectedErrorMessage {
			t.Fatalf("Expected error message to be [%s], but it was [%s]", expectedErrorMessage, actualErrorMessage)
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
