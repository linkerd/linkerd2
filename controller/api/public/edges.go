package public

import (
	"context"
	"errors"
	"fmt"
	"strings"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
)

const (
	inboundIdentityQuery  = "count(response_total%s) by (%s, client_id)"
	outboundIdentityQuery = "count(response_total%s) by (%s, dst_%s, server_id, no_tls_reason)"
)

var formatMsg = map[string]string{
	"disabled":                          "Disabled",
	"loopback":                          "Loopback",
	"no_authority_in_http_request":      "No Authority In HTTP Request",
	"not_http":                          "Not HTTP",
	"not_provided_by_remote":            "Not Provided By Remote",
	"not_provided_by_service_discovery": "Not Provided By Service Discovery",
}

func (s *grpcServer) Edges(ctx context.Context, req *pb.EdgesRequest) (*pb.EdgesResponse, error) {
	if req.GetSelector().GetResource() == nil {
		log.Debugf("Edges request: %+v", req)
		return &pb.EdgesResponse{
			Response: &pb.EdgesResponse_Error{
				Error: &pb.ResourceError{
					Resource: req.GetSelector().GetResource(),
					Error:    "Edges request missing Selector Resource",
				},
			},
		}, nil
	}

	edges, err := s.getEdges(ctx, req)
	if err != nil {
		return &pb.EdgesResponse{
			Response: &pb.EdgesResponse_Error{
				Error: &pb.ResourceError{
					Resource: req.GetSelector().GetResource(),
					Error:    err.Error(),
				},
			},
		}, nil
	}

	return &pb.EdgesResponse{
		Response: &pb.EdgesResponse_Ok_{
			Ok: &pb.EdgesResponse_Ok{
				Edges: edges,
			},
		},
	}, nil
}

func (s *grpcServer) getEdges(ctx context.Context, req *pb.EdgesRequest) ([]*pb.Edge, error) {
	labelNames := promGroupByLabelNames(req.Selector.Resource)
	if len(labelNames) != 2 {
		return nil, errors.New("unexpected resource selector")
	}
	resourceType := string(labelNames[1]) // skipping first name which is always namespace
	labels := promQueryLabels(req.Selector.Resource)
	labelsOutbound := labels.Merge(promDirectionLabels("outbound"))
	labelsInbound := labels.Merge(promDirectionLabels("inbound"))

	inboundQuery := fmt.Sprintf(inboundIdentityQuery, labelsInbound, resourceType)
	outboundQuery := fmt.Sprintf(outboundIdentityQuery, labelsOutbound, resourceType, resourceType)

	inboundResult, err := s.queryProm(ctx, inboundQuery)
	if err != nil {
		return nil, err
	}

	outboundResult, err := s.queryProm(ctx, outboundQuery)
	if err != nil {
		return nil, err
	}

	edge := processEdgeMetrics(inboundResult, outboundResult, resourceType)
	return edge, nil
}

func processEdgeMetrics(inbound, outbound model.Vector, resourceType string) []*pb.Edge {
	edges := []*pb.Edge{}
	dstIndex := map[model.LabelValue]model.Metric{}
	srcIndex := map[model.LabelValue][]model.Metric{}
	keys := map[model.LabelValue]struct{}{}
	resourceReplacementInbound := resourceType
	resourceReplacementOutbound := "dst_" + resourceType

	for _, sample := range inbound {
		// skip any inbound results that do not have a client_id, because this means
		// the communication was one-sided (i.e. a probe or another instance where the src/dst are not both known)
		// in future the edges command will support one-sided edges
		if _, ok := sample.Metric[model.LabelName("client_id")]; ok {
			key := sample.Metric[model.LabelName(resourceReplacementInbound)]
			dstIndex[key] = sample.Metric
			keys[key] = struct{}{}
		}
	}

	for _, sample := range outbound {
		// skip any outbound results that do not have a server_id for same reason as above section
		if _, ok := sample.Metric[model.LabelName("server_id")]; ok {
			key := sample.Metric[model.LabelName(resourceReplacementOutbound)]
			if _, ok := srcIndex[key]; !ok {
				srcIndex[key] = []model.Metric{}
			}
			srcIndex[key] = append(srcIndex[key], sample.Metric)
			keys[key] = struct{}{}
		}
	}

	for key, sources := range srcIndex {
		for _, src := range sources {
			dst, ok := dstIndex[key]
			if !ok {
				log.Errorf("missing resource in destination metrics: %s", key)
				continue
			}
			msg := ""
			if val, ok := src[model.LabelName("no_tls_reason")]; ok {
				msg = formatMsg[string(val)]
			}
			edge := &pb.Edge{
				Src: &pb.Resource{
					Name: string(src[model.LabelName(resourceType)]),
					Type: resourceType,
				},
				Dst: &pb.Resource{
					Name: string(dst[model.LabelName(resourceType)]),
					Type: resourceType,
				},
				ClientId: strings.Split(string(dst[model.LabelName("client_id")]), ".")[0],
				ServerId: strings.Split(string(src[model.LabelName("server_id")]), ".")[0],
				Msg:      msg,
			}
			edges = append(edges, edge)
		}
	}

	return edges
}
