package tap

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/golang/protobuf/proto"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/protohttp"
	log "github.com/sirupsen/logrus"
)

// Reader initiates a TapByResourceRequest and returns a buffered Reader.
// It is the caller's responsibility to call Close() on the io.ReadCloser.
func Reader(k8sAPI *k8s.KubernetesAPI, req *pb.TapByResourceRequest) (*bufio.Reader, io.ReadCloser, error) {
	client, err := k8sAPI.NewClient()
	if err != nil {
		return nil, nil, err
	}

	reqBytes, err := proto.Marshal(req)
	if err != nil {
		return nil, nil, err
	}

	url := protohttp.TapReqToURL(req)
	httpReq, err := http.NewRequest(
		http.MethodPost,
		fmt.Sprintf("%s%s", k8sAPI.Host, url),
		bytes.NewReader(reqBytes),
	)
	if err != nil {
		return nil, nil, err
	}

	httpRsp, err := client.Do(httpReq)
	if err != nil {
		log.Debugf("Error invoking [%s]: %v", "taps", err)
		return nil, nil, err
	}

	log.Debugf("Response from [%s] had headers: %v", "taps", httpRsp.Header)

	if err := protohttp.CheckIfResponseHasError(httpRsp); err != nil {
		httpRsp.Body.Close()
		return nil, nil, err
	}

	reader := bufio.NewReader(httpRsp.Body)

	return reader, httpRsp.Body, nil
}
