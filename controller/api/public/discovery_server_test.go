package public

import (
	"context"
	"reflect"
	"testing"

	"github.com/golang/protobuf/proto"
	"github.com/linkerd/linkerd2/controller/api/proxy"
	"github.com/linkerd/linkerd2/controller/gen/controller/discovery"
	"github.com/linkerd/linkerd2/controller/k8s"
)

type endpointsExpected struct {
	err error
	req *discovery.EndpointsParams
	res *discovery.EndpointsResponse
}

func TestEndpoints(t *testing.T) {
	t.Run("Queries to the Endpoints endpoint", func(t *testing.T) {
		expectations := []endpointsExpected{
			endpointsExpected{
				err: nil,
				req: &discovery.EndpointsParams{},
				res: &discovery.EndpointsResponse{},
			},
		}

		for _, exp := range expectations {
			k8sAPI, err := k8s.NewFakeAPI("")
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}
			k8sAPI.Sync()

			proxyAPIClient, gRPCServer, proxyAPIConn := proxy.InitFakeDiscoveryServer(t, k8sAPI)
			defer gRPCServer.GracefulStop()
			defer proxyAPIConn.Close()

			fakeDiscoveryServer := newDiscoveryServer(proxyAPIClient)

			rsp, err := fakeDiscoveryServer.Endpoints(context.TODO(), exp.req)
			if !reflect.DeepEqual(err, exp.err) {
				t.Fatalf("Expected error: %s, Got: %s", exp.err, err)
			}

			if !proto.Equal(exp.res, rsp) {
				t.Fatalf("Unexpected response: [%+v] != [%+v]", exp.res, rsp)
			}
		}
	})
}
