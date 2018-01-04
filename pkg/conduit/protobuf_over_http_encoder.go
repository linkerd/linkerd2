package conduit

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/golang/protobuf/proto"
	pb "github.com/runconduit/conduit/controller/gen/public"
)

//todo: make object mockable and test client
func clientUnmarshal(r *bufio.Reader, errorMsg string, msg proto.Message) error {
	byteSize := make([]byte, 4)
	_, err := r.Read(byteSize)
	if err != nil {
		return err
	}

	size := binary.LittleEndian.Uint32(byteSize)
	bytes := make([]byte, size)
	_, err = io.ReadFull(r, bytes)
	if err != nil {
		return err
	}

	if errorMsg != "" {
		var apiError pb.ApiError
		err = proto.Unmarshal(bytes, &apiError)
		if err != nil {
			return err
		}
		return fmt.Errorf("%s: %s", errorMsg, apiError.Error)
	}

	return proto.Unmarshal(bytes, msg)
}
