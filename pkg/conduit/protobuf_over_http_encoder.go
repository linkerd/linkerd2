package conduit

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/golang/protobuf/proto"
	pb "github.com/runconduit/conduit/controller/gen/public"
)

func fromByteStreamToProtocolBuffers(byteStreamContainingMessage *bufio.Reader, errorMessageReturnedAsMetadata string, out proto.Message) error {
	//TODO: why the magic number 4?
	byteSize := make([]byte, 4)

	//TODO: why is this necessary?
	_, err := byteStreamContainingMessage.Read(byteSize)
	if err != nil {
		return fmt.Errorf("error reading byte stream header: %v", err)
	}

	size := binary.LittleEndian.Uint32(byteSize)
	bytes := make([]byte, size)
	_, err = io.ReadFull(byteStreamContainingMessage, bytes)
	if err != nil {
		return fmt.Errorf("error reading byte stream content: %v", err)
	}

	if errorMessageReturnedAsMetadata != "" {
		var apiError pb.ApiError
		err = proto.Unmarshal(bytes, &apiError)
		if err != nil {
			return fmt.Errorf("error unmarshalling error from byte stream: %v", err)
		}
		return fmt.Errorf("%s: %s", errorMessageReturnedAsMetadata, apiError.Error)
	}

	err = proto.Unmarshal(bytes, out)
	if err != nil {
		return fmt.Errorf("error unmarshalling bytes: %v", err)
	} else {
		return nil
	}
}
