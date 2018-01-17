package public

import (
	"bytes"
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
)

type httpError struct {
	Code         int
	WrappedError error
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

	fullPayload := serializeAsPayload(marshalledProtobufMessage)
	_, err = w.Write(fullPayload)
	return err
}

const numBytesForMessageLength = 4

func serializeAsPayload(marshalledProtobuf []byte) []byte {
	byteMagicNumber := make([]byte, numBytesForMessageLength)
	sizeOfTheProtobufPayload := len(marshalledProtobuf)
	binary.LittleEndian.PutUint32(byteMagicNumber, uint32(sizeOfTheProtobufPayload))
	return append(byteMagicNumber, marshalledProtobuf...)
}

func deserializeFromPayload(protobufPayload []byte) []byte {
	messageLengthinBytes := make([]byte, numBytesForMessageLength)
	reader := bytes.NewReader(protobufPayload)
	reader.Read(messageLengthinBytes)
	messageLength := binary.LittleEndian.Uint32(messageLengthinBytes)

	fromIndex := numBytesForMessageLength
	untilIndex := messageLength + numBytesForMessageLength
	return protobufPayload[fromIndex:untilIndex]
}
