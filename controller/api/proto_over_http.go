package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/golang/protobuf/proto"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	log "github.com/sirupsen/logrus"
)

const (
	ErrorHeader              = "linkerd-error"
	NumBytesForMessageLength = 4
)

func deserializePayloadFromReader(reader *bufio.Reader) ([]byte, error) {
	messageLengthAsBytes := make([]byte, NumBytesForMessageLength)
	_, err := io.ReadFull(reader, messageLengthAsBytes)
	if err != nil {
		return nil, fmt.Errorf("error while reading message length: %v", err)
	}
	messageLength := int(binary.LittleEndian.Uint32(messageLengthAsBytes))

	messageContentsAsBytes := make([]byte, messageLength)
	_, err = io.ReadFull(reader, messageContentsAsBytes)
	if err != nil {
		return nil, fmt.Errorf("error while reading bytes from message: %v", err)
	}

	return messageContentsAsBytes, nil
}

func CheckIfResponseHasError(rsp *http.Response) error {
	errorMsg := rsp.Header.Get(ErrorHeader)

	if errorMsg != "" {
		reader := bufio.NewReader(rsp.Body)
		var apiError pb.ApiError

		err := FromByteStreamToProtocolBuffers(reader, &apiError)
		if err != nil {
			return fmt.Errorf("Response has %s header [%s], but response body didn't contain protobuf error: %v", ErrorHeader, errorMsg, err)
		}

		return errors.New(apiError.Error)
	}

	if rsp.StatusCode != http.StatusOK {
		return fmt.Errorf("Unexpected API response: %s", rsp.Status)
	}

	return nil
}

// HTTPPost marshals a protobuf message and posts it to the url using the httpClient
func HTTPPost(ctx context.Context, httpClient *http.Client, url *url.URL, req proto.Message) (*http.Response, error) {
	reqBytes, err := proto.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequest(
		http.MethodPost,
		url.String(),
		bytes.NewReader(reqBytes),
	)
	if err != nil {
		return nil, err
	}

	rsp, err := httpClient.Do(httpReq.WithContext(ctx))
	if err != nil {
		log.Debugf("Error invoking [%s]: %v", url.String(), err)
	} else {
		log.Debugf("Response from [%s] had headers: %v", url.String(), rsp.Header)
	}

	return rsp, err
}

// FromByteStreamToProtocolBuffers deserializes the payload from the reader and unmarshalls it into out
func FromByteStreamToProtocolBuffers(byteStreamContainingMessage *bufio.Reader, out proto.Message) error {
	messageAsBytes, err := deserializePayloadFromReader(byteStreamContainingMessage)
	if err != nil {
		return fmt.Errorf("error reading byte stream header: %v", err)
	}

	err = proto.Unmarshal(messageAsBytes, out)
	if err != nil {
		return fmt.Errorf("error unmarshalling array of [%d] bytes error: %v", len(messageAsBytes), err)
	}

	return nil
}

// EndpointNameToPublicAPIURL returns the full URL for endpoint, using
// serverURL as a base
func EndpointNameToPublicAPIURL(serverURL *url.URL, endpoint string) *url.URL {
	return serverURL.ResolveReference(&url.URL{Path: endpoint})
}
