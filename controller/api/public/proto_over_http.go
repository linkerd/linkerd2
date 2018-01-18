package public

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/golang/protobuf/proto"
	pb "github.com/runconduit/conduit/controller/gen/public"
	log "github.com/sirupsen/logrus"
)

const (
	defaultHttpErrorStatusCode = http.StatusInternalServerError
	contentTypeHeader          = "Content-Type"
	protobufContentType        = "application/octet-stream"
	numBytesForMessageLength   = 4
)

type httpError struct {
	Code         int
	WrappedError error
}

type flushableResponseWriter interface {
	http.ResponseWriter
	http.Flusher
}

func (e httpError) Error() string {
	return fmt.Sprintf("HTTP error, status Code [%d], wrapped error is: %v", e.Code, e.WrappedError)
}

func httpRequestToProto(req *http.Request, protoRequestOut proto.Message) error {
	bytes, err := ioutil.ReadAll(req.Body)
	if err != nil {
		return httpError{
			Code:         http.StatusBadRequest,
			WrappedError: err,
		}
	}

	err = proto.Unmarshal(bytes, protoRequestOut)
	if err != nil {
		return httpError{
			Code:         http.StatusBadRequest,
			WrappedError: err,
		}
	}

	return nil
}

func writeErrorToHttpResponse(w http.ResponseWriter, errorObtained error) {
	statusCode := defaultHttpErrorStatusCode
	errorToReturn := errorObtained

	if httpErr, ok := errorObtained.(httpError); ok {
		statusCode = httpErr.Code
		errorToReturn = httpErr.WrappedError
	}

	w.Header().Set(ErrorHeader, http.StatusText(statusCode))

	errorAsProto := &pb.ApiError{Error: errorToReturn.Error()}

	err := writeProtoToHttpResponse(w, errorAsProto)
	if err != nil {
		log.Errorf("Error writing error to http response: %v", err)
		w.Header().Set(ErrorHeader, err.Error())
	}
}

func writeProtoToHttpResponse(w http.ResponseWriter, msg proto.Message) error {
	w.Header().Set(contentTypeHeader, protobufContentType)
	marshalledProtobufMessage, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	fullPayload, err := serializeAsPayload(marshalledProtobufMessage)
	if err != nil {
		return err
	}
	_, err = w.Write(fullPayload)
	return err
}

func newStreamingWriter(w http.ResponseWriter) (flushableResponseWriter, error) {
	flushableWriter, ok := w.(flushableResponseWriter)
	if !ok {
		return nil, fmt.Errorf("streaming not supported by this writer")
	}

	flushableWriter.Header().Set("Connection", "keep-alive")
	flushableWriter.Header().Set("Transfer-Encoding", "chunked")
	return flushableWriter, nil
}

func serializeAsPayload(marshalledProtobuf []byte) ([]byte, error) {
	lengthOfThePayload := uint32(len(marshalledProtobuf))

	messageLengthInBytes := make([]byte, numBytesForMessageLength)
	binary.LittleEndian.PutUint32(messageLengthInBytes, lengthOfThePayload)

	return append(messageLengthInBytes, marshalledProtobuf...), nil
}

func deserializePayloadFromReader(reader *bufio.Reader) ([]byte, error) {
	messageLengthInBytes := make([]byte, numBytesForMessageLength)
	reader.Read(messageLengthInBytes)
	messageLength := binary.LittleEndian.Uint32(messageLengthInBytes)

	messageContentsInBytes := make([]byte, messageLength)
	_, err := reader.Read(messageContentsInBytes)

	return messageContentsInBytes, err
}
