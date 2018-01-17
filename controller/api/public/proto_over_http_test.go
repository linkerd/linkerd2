package public

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/golang/protobuf/proto"
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

		payload := deserializeFromPayload(responseWriter.body.Bytes())

		expectedErrorPayload := pb.ApiError{Error: genericError.Error()}
		var actualErrorPayload pb.ApiError
		err := proto.Unmarshal(payload, &actualErrorPayload)
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

		payload := deserializeFromPayload(responseWriter.body.Bytes())

		expectedErrorPayload := pb.ApiError{Error: httpError.WrappedError.Error()}
		var actualErrorPayload pb.ApiError
		err := proto.Unmarshal(payload, &actualErrorPayload)
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

		payload := deserializeFromPayload(responseWriter.body.Bytes())

		var actualMessage pb.VersionInfo
		err = proto.Unmarshal(payload, &actualMessage)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if expectedMessage != actualMessage {
			t.Fatalf("Expected response body to contain message [%v], but got [%v]", expectedMessage, actualMessage)
		}
	})
}

func TestPayloadSize(t *testing.T) {
	t.Run("Can read message correctly based on payload size correct payload size to message", func(t *testing.T) {
		expectedMessage := "this is the message"

		messageWithSize := serializeAsPayload([]byte(expectedMessage))

		messageWithSomeNoise := append(messageWithSize, []byte("this is noise and should not be read")...)

		actualMessage := deserializeFromPayload(messageWithSomeNoise)

		if string(actualMessage) != expectedMessage {
			t.Fatalf("Expecting payload to contain message [%s], but it had [%s]", expectedMessage, actualMessage)
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
