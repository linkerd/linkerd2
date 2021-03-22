package pkg

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/golang/protobuf/proto"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/protohttp"
	pb "github.com/linkerd/linkerd2/viz/tap/gen/tap"
	log "github.com/sirupsen/logrus"
)

const (
	// ErrClosedResponseBody is returned when response body is closed in http2
	ErrClosedResponseBody = "http2: response body closed"
)

// TapRbacURL is the link users should visit to remedy issues when attempting
// to tap resources with missing authorizations
const TapRbacURL = "https://linkerd.io/tap-rbac"

// Reader initiates a TapByResourceRequest and returns a buffered Reader.
// It is the caller's responsibility to call Close() on the io.ReadCloser.
func Reader(ctx context.Context, k8sAPI *k8s.KubernetesAPI, req *pb.TapByResourceRequest) (*bufio.Reader, io.ReadCloser, error) {
	client, err := k8sAPI.NewClient()
	if err != nil {
		return nil, nil, err
	}

	reqBytes, err := proto.Marshal(req)
	if err != nil {
		return nil, nil, err
	}

	url, err := url.Parse(k8sAPI.Host)
	if err != nil {
		return nil, nil, err
	}
	url.Path = fmt.Sprintf("%s%s", url.Path, TapReqToURL(req))

	httpReq, err := http.NewRequest(
		http.MethodPost,
		url.String(),
		bytes.NewReader(reqBytes),
	)
	if err != nil {
		return nil, nil, err
	}

	httpRsp, err := client.Do(httpReq.WithContext(ctx))
	if err != nil {
		log.Debugf("Error invoking [%s]: %v", url, err)
		return nil, nil, err
	}

	log.Debugf("Response from [%s] had headers: %v", url, httpRsp.Header)

	if err := protohttp.CheckIfResponseHasError(httpRsp); err != nil {
		httpRsp.Body.Close()
		return nil, nil, err
	}

	reader := bufio.NewReader(httpRsp.Body)

	return reader, httpRsp.Body, nil
}
