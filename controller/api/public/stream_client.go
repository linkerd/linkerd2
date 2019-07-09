package public

import (
	"bufio"
	"context"
	"net/http"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/metadata"
)

type streamClient struct {
	ctx    context.Context
	reader *bufio.Reader
}

// satisfy the ClientStream interface
func (c streamClient) Header() (metadata.MD, error) { return nil, nil }
func (c streamClient) Trailer() metadata.MD         { return nil }
func (c streamClient) CloseSend() error             { return nil }
func (c streamClient) Context() context.Context     { return c.ctx }
func (c streamClient) SendMsg(interface{}) error    { return nil }
func (c streamClient) RecvMsg(interface{}) error    { return nil }

func getStreamClient(ctx context.Context, httpRsp *http.Response) (streamClient, error) {
	if err := checkIfResponseHasError(httpRsp); err != nil {
		httpRsp.Body.Close()
		return streamClient{}, err
	}

	go func() {
		<-ctx.Done()
		log.Debug("Closing response body after context marked as done")
		httpRsp.Body.Close()
	}()

	return streamClient{ctx: ctx, reader: bufio.NewReader(httpRsp.Body)}, nil
}
