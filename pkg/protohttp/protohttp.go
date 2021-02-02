package protohttp

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/golang/protobuf/proto"
	"github.com/linkerd/linkerd2/pkg/k8s"
	metricsPb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/status"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
)

const (
	errorHeader                = "linkerd-error"
	defaultHTTPErrorStatusCode = http.StatusInternalServerError
	contentTypeHeader          = "Content-Type"
	protobufContentType        = "application/octet-stream"
	numBytesForMessageLength   = 4
)

// HTTPError is an error which indicates the HTTP response contained an error
type HTTPError struct {
	Code         int
	WrappedError error
}

// FlushableResponseWriter wraps a ResponseWriter for use in streaming
// responses
type FlushableResponseWriter interface {
	http.ResponseWriter
	http.Flusher
}

// Error satisfies the error interface for HTTPError.
func (e HTTPError) Error() string {
	return fmt.Sprintf("HTTP error, status Code [%d] (%v)", e.Code, e.WrappedError)
}

// HTTPRequestToProto converts an HTTP Request to a protobuf request.
func HTTPRequestToProto(req *http.Request, protoRequestOut proto.Message) error {
	bytes, err := ioutil.ReadAll(req.Body)
	if err != nil {
		return HTTPError{
			Code:         http.StatusBadRequest,
			WrappedError: err,
		}
	}

	err = proto.Unmarshal(bytes, protoRequestOut)
	if err != nil {
		return HTTPError{
			Code:         http.StatusBadRequest,
			WrappedError: err,
		}
	}

	return nil
}

// WriteErrorToHTTPResponse writes a protobuf-encoded error to an HTTP Response.
func WriteErrorToHTTPResponse(w http.ResponseWriter, errorObtained error) {
	statusCode := defaultHTTPErrorStatusCode
	errorToReturn := errorObtained

	if httpErr, ok := errorObtained.(HTTPError); ok {
		statusCode = httpErr.Code
		errorToReturn = httpErr.WrappedError
	}

	w.Header().Set(errorHeader, http.StatusText(statusCode))

	errorMessageToReturn := errorToReturn.Error()
	if grpcError, ok := status.FromError(errorObtained); ok {
		errorMessageToReturn = grpcError.Message()
	}

	errorAsProto := &metricsPb.ApiError{Error: errorMessageToReturn}

	err := WriteProtoToHTTPResponse(w, errorAsProto)
	if err != nil {
		log.Errorf("Error writing error to http response: %v", err)
		w.Header().Set(errorHeader, err.Error())
	}
}

// WriteProtoToHTTPResponse writes a protobuf-encoded message to an HTTP
// Response.
func WriteProtoToHTTPResponse(w http.ResponseWriter, msg proto.Message) error {
	w.Header().Set(contentTypeHeader, protobufContentType)
	marshalledProtobufMessage, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	fullPayload := SerializeAsPayload(marshalledProtobufMessage)
	_, err = w.Write(fullPayload)
	return err
}

// NewStreamingWriter takes a ResponseWriter and returns it wrapped in a
// FlushableResponseWriter.
func NewStreamingWriter(w http.ResponseWriter) (FlushableResponseWriter, error) {
	flushableWriter, ok := w.(FlushableResponseWriter)
	if !ok {
		return nil, fmt.Errorf("streaming not supported by this writer")
	}

	flushableWriter.Header().Set("Connection", "keep-alive")
	flushableWriter.Header().Set("Transfer-Encoding", "chunked")
	return flushableWriter, nil
}

// SerializeAsPayload appends a 4-byte length in front of a byte slice.
func SerializeAsPayload(messageContentsInBytes []byte) []byte {
	lengthOfThePayload := uint32(len(messageContentsInBytes))

	messageLengthInBytes := make([]byte, numBytesForMessageLength)
	binary.LittleEndian.PutUint32(messageLengthInBytes, lengthOfThePayload)

	return append(messageLengthInBytes, messageContentsInBytes...)
}

func deserializePayloadFromReader(reader *bufio.Reader) ([]byte, error) {
	messageLengthAsBytes := make([]byte, numBytesForMessageLength)
	_, err := io.ReadFull(reader, messageLengthAsBytes)
	if err != nil {
		return nil, fmt.Errorf("error while reading message length: %w", err)
	}
	messageLength := int(binary.LittleEndian.Uint32(messageLengthAsBytes))

	messageContentsAsBytes := make([]byte, messageLength)
	_, err = io.ReadFull(reader, messageContentsAsBytes)
	if err != nil {
		return nil, fmt.Errorf("error while reading bytes from message: %w", err)
	}

	return messageContentsAsBytes, nil
}

// CheckIfResponseHasError checks an HTTP Response for errors and returns error
// information with the following precedence:
// 1. "linkerd-error" header, with protobuf-encoded apiError
// 2. non-200 Status Code, with Kubernetes StatusError
// 3. non-200 Status Code
func CheckIfResponseHasError(rsp *http.Response) error {
	// check for protobuf-encoded error
	errorMsg := rsp.Header.Get(errorHeader)
	if errorMsg != "" {
		reader := bufio.NewReader(rsp.Body)
		var apiError metricsPb.ApiError

		err := FromByteStreamToProtocolBuffers(reader, &apiError)
		if err != nil {
			return fmt.Errorf("Response has %s header [%s], but response body didn't contain protobuf error: %v", errorHeader, errorMsg, err)
		}

		return errors.New(apiError.Error)
	}

	// check for JSON-encoded error
	if rsp.StatusCode != http.StatusOK {
		if rsp.Body != nil {
			bytes, err := ioutil.ReadAll(rsp.Body)
			if err == nil && len(bytes) > 0 {
				body := string(bytes)
				obj, err := k8s.ToRuntimeObject(body)
				if err == nil {
					return HTTPError{Code: rsp.StatusCode, WrappedError: kerrors.FromObject(obj)}
				}

				body = fmt.Sprintf("unexpected API response: %s", body)
				return HTTPError{Code: rsp.StatusCode, WrappedError: errors.New(body)}
			}
		}

		return HTTPError{Code: rsp.StatusCode, WrappedError: errors.New("unexpected API response")}
	}

	return nil
}

// FromByteStreamToProtocolBuffers converts a byte stream to a protobuf message.
func FromByteStreamToProtocolBuffers(byteStreamContainingMessage *bufio.Reader, out proto.Message) error {
	messageAsBytes, err := deserializePayloadFromReader(byteStreamContainingMessage)
	if err != nil {
		return fmt.Errorf("error reading byte stream header: %w", err)
	}

	err = proto.Unmarshal(messageAsBytes, out)
	if err != nil {
		return fmt.Errorf("error unmarshalling array of [%d] bytes error: %w", len(messageAsBytes), err)
	}

	return nil
}
