package public

import (
	"context"

	"github.com/linkerd/linkerd2/controller/gen/controller/discovery"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	log "github.com/sirupsen/logrus"
)

type (
	discoveryServer struct {
		client discovery.ApiClient
		log    *log.Entry
	}
)

func newDiscoveryServer(discoveryClient discovery.ApiClient) *discoveryServer {
	server := discoveryServer{
		client: discoveryClient,
		log: log.WithFields(log.Fields{
			"server": "discovery",
		}),
	}

	discovery.RegisterApiServer(prometheus.NewGrpcServer(), &server)

	return &server
}

func (d *discoveryServer) Endpoints(ctx context.Context, params *discovery.EndpointsParams) (*discovery.EndpointsResponse, error) {
	d.log.Debugf("Endpoints(%+v)", params)

	rsp, err := d.client.Endpoints(ctx, params)
	if err != nil {
		d.log.Errorf("endpoints request to proxy API failed: %s", err)
		return nil, err
	}
	d.log.Debugf("Endpoints(%+v): %+v", params, rsp)

	return rsp, nil
}
