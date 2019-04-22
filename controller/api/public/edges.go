package public

import (
	"context"
	"fmt"
	"strings"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/prometheus/common/model"
)

type edgeResult struct {
	res *pb.Edge
	err error
}

const (
	inboundIdentityQuery  = "label_replace(sum(increase(response_total%s[1m])) by (%s,client_id), \"destination_%s\", \"$1\", \"%s\", \"(.+)\")"
	outboundIdentityQuery = "label_replace(sum(increase(response_total%s[1m])) by (%s, dst_%s, server_id, no_tls_reason), \"destination_%s\", \"$1\", \"dst_%s\", \"(.+)\")"
)

func (s *grpcServer) Edges(ctx context.Context, req *pb.StatSummaryRequest) (*pb.EdgesResponse, error) {
	if req.GetSelector().GetResource() == nil {
		return edgesError(req, "StatSummary request missing Selector Resource"), nil
	}

	edges, err := s.getEdges(ctx, req, req.TimeWindow)
	if err != nil {
		return nil, err
	}

	return &pb.EdgesResponse{
		Response: &pb.EdgesResponse_Ok_{
			Ok: &pb.EdgesResponse_Ok{
				Edges: edges,
			},
		},
	}, nil
}

func edgesError(req *pb.StatSummaryRequest, message string) *pb.EdgesResponse {
	return &pb.EdgesResponse{
		Response: &pb.EdgesResponse_Error{
			Error: &pb.ResourceError{
				Resource: req.GetSelector().GetResource(),
				Error:    message,
			},
		},
	}
}

func (s *grpcServer) getEdges(ctx context.Context, req *pb.StatSummaryRequest, timeWindow string) ([]*pb.Edge, error) {
	resourceType := string(promGroupByLabelNames(req.Selector.Resource)[1]) // skipping first name which is always namespace
	labels := promQueryLabels(req.Selector.Resource)
	labelsOutbound := labels.Merge(promDirectionLabels("outbound"))
	labelsInbound := labels.Merge(promDirectionLabels("inbound"))

	inboundQuery := fmt.Sprintf(inboundIdentityQuery, labelsInbound, resourceType, resourceType, resourceType)
	outboundQuery := fmt.Sprintf(outboundIdentityQuery, labelsOutbound, resourceType, resourceType, resourceType, resourceType)

	inboundResult, err := s.queryProm(ctx, inboundQuery)
	if err != nil {
		return nil, err
	}

	outboundResult, err := s.queryProm(ctx, outboundQuery)
	if err != nil {
		return nil, err
	}

	edge := processEdgeMetrics(req, inboundResult, outboundResult, resourceType)
	return edge, nil
}

func processEdgeMetrics(req *pb.StatSummaryRequest, inbound, outbound model.Vector, resourceType string) []*pb.Edge {
	edges := []*pb.Edge{}
	dstIndex := map[model.LabelValue]model.Metric{}
	srcIndex := map[model.LabelValue][]model.Metric{}
	keys := map[model.LabelValue]struct{}{}
	resourceLabelReplacement := "destination_" + resourceType

	formatMsg := map[string]string{
		"disabled":                          "Disabled",
		"loopback":                          "Loopback",
		"no_authority_in_http_request":      "No Authority In HTTP Request",
		"not_http":                          "Not HTTP",
		"not_provided_by_remote":            "Not Provided By Remote",
		"not_provided_by_service_discovery": "Not Provided By Service Discovery",
	}

	for _, sample := range inbound {
		// skip any inbound results that do not have a client_id, because this means
		// the communication was one-sided (i.e. a probe or another instance where the src/dst are not both known)
		// in future the edges command will support one-sided edges
		if _, ok := sample.Metric[model.LabelName("client_id")]; ok {
			key := sample.Metric[model.LabelName(resourceLabelReplacement)]
			dstIndex[key] = sample.Metric
			keys[key] = struct{}{}
		}
	}

	for _, sample := range outbound {
		// skip any outbound results that do not have a server_id for same reason as above section
		if _, ok := sample.Metric[model.LabelName("server_id")]; ok {
			key := sample.Metric[model.LabelName(resourceLabelReplacement)]
			if _, ok := srcIndex[key]; !ok {
				srcIndex[key] = []model.Metric{}
			}
			srcIndex[key] = append(srcIndex[key], sample.Metric)
			keys[key] = struct{}{}
		}
	}

	for key := range keys {
		for _, src := range srcIndex[key] {
			dst := dstIndex[key]
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

func getEdgeResultKeys(
	req *pb.StatSummaryRequest,
	k8sObjects map[rKey]k8sStat,
	metricResults []*pb.Edge,
) []rKey {
	var keys []rKey

	for key := range k8sObjects {
		keys = append(keys, key)
	}
	return keys
}
