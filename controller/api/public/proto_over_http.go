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
	err := serverUnmarshal(req, protoRequestOut)
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

	fullPayload := appendHeaderTo(marshalledProtobufMessage)
	_, err = w.Write(fullPayload)
	return err
}

func appendHeaderTo(marshalledProtobuf []byte) []byte {
	byteMagicNumber := make([]byte, 4)
	sizeOfTheProtobufPayload := len(marshalledProtobuf)
	binary.LittleEndian.PutUint32(byteMagicNumber, uint32(sizeOfTheProtobufPayload))
	return append(byteMagicNumber, marshalledProtobuf...)
}

func removeHeaderFrom(protobufPayload []byte) ([]byte, error) {
	byteMagicNumber := make([]byte, 4)
	reader := bytes.NewReader(protobufPayload)
	reader.Read(byteMagicNumber)
	return ioutil.ReadAll(reader)
}
