package public

import (
	"context"
	"errors"
	"fmt"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
)

const (
	inboundIdentityQuery  = "count(response_total%s) by (%s, client_id, namespace, no_tls_reason)"
	outboundIdentityQuery = "count(response_total%s) by (%s, dst_%s, server_id, namespace, dst_namespace, no_tls_reason)"
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
	log.Debugf("Edges request: %+v", req)
	if req.GetSelector().GetResource() == nil {
		return edgesError(req, "Edges request missing Selector Resource"), nil
	}

	edges, err := s.getEdges(ctx, req)
	if err != nil {
		return edgesError(req, err.Error()), nil
	}

	return &pb.EdgesResponse{
		Response: &pb.EdgesResponse_Ok_{
			Ok: &pb.EdgesResponse_Ok{
				Edges: edges,
			},
		},
	}, nil
}

func edgesError(req *pb.EdgesRequest, message string) *pb.EdgesResponse {
	return &pb.EdgesResponse{
		Response: &pb.EdgesResponse_Error{
			Error: &pb.ResourceError{
				Resource: req.GetSelector().GetResource(),
				Error:    message,
			},
		},
	}
}

func (s *grpcServer) getEdges(ctx context.Context, req *pb.EdgesRequest) ([]*pb.Edge, error) {
	labelNames := promGroupByLabelNames(req.Selector.Resource)
	if len(labelNames) != 2 {
		return nil, errors.New("unexpected resource selector")
	}
	selectedNamespace := req.Selector.Resource.Namespace
	resourceType := string(labelNames[1]) // skipping first name which is always namespace
	labelsOutbound := model.LabelSet(promDirectionLabels("outbound"))
	labelsInbound := model.LabelSet(promDirectionLabels("inbound"))

	// checking that data for the specified resource type exists
	labelsOutboundStr := generateLabelStringWithExclusion(labelsOutbound, resourceType)
	labelsInboundStr := generateLabelStringWithExclusion(labelsInbound, resourceType)

	outboundQuery := fmt.Sprintf(outboundIdentityQuery, labelsOutboundStr, resourceType, resourceType)
	inboundQuery := fmt.Sprintf(inboundIdentityQuery, labelsInboundStr, resourceType)

	inboundResult, err := s.queryProm(ctx, inboundQuery)
	if err != nil {
		return nil, err
	}

	outboundResult, err := s.queryProm(ctx, outboundQuery)
	if err != nil {
		return nil, err
	}

	edge := processEdgeMetrics(inboundResult, outboundResult, resourceType, selectedNamespace)
	return edge, nil
}

func processEdgeMetrics(inbound, outbound model.Vector, resourceType, selectedNamespace string) []*pb.Edge {
	edges := []*pb.Edge{}
	dstIndex := map[model.LabelValue]model.Metric{}
	srcIndex := map[model.LabelValue][]model.Metric{}
	resourceReplacementInbound := resourceType
	resourceReplacementOutbound := "dst_" + resourceType

	allNamespaces := (len(selectedNamespace) == 0)

	for _, sample := range inbound {

		namespace := string(sample.Metric[model.LabelName("namespace")])

		// first, check if namespace matches the request
		if allNamespaces == true || namespace == selectedNamespace {

			// then skip inbound results without a clientID because we cannot
			// construct edge information
			if _, ok := sample.Metric[model.LabelName("client_id")]; ok {
				key := sample.Metric[model.LabelName(resourceReplacementInbound)]
				dstIndex[key] = sample.Metric
			}
		}
	}

	for _, sample := range outbound {

		namespace := string(sample.Metric[model.LabelName("namespace")])
		dstNamespace := string(sample.Metric[model.LabelName("dst_namespace")])
		resource := string(sample.Metric[model.LabelName(resourceType)])
		dstResource := string(sample.Metric[model.LabelName(resourceReplacementOutbound)])

		// first, check if SRC or DST namespaces match the request
		if allNamespaces == true || namespace == selectedNamespace || dstNamespace == selectedNamespace {

			// second, if secured, add key to srcIndex for matching
			if _, ok := sample.Metric[model.LabelName("server_id")]; ok {
				key := sample.Metric[model.LabelName(resourceReplacementOutbound)]
				if _, ok := srcIndex[key]; !ok {
					srcIndex[key] = []model.Metric{}
				}
				srcIndex[key] = append(srcIndex[key], sample.Metric)

				// third, construct unsecured edge if a resource and dstResource are present
			} else if len(resource) > 0 && len(dstResource) > 0 {
				msg := formatMsg[string(sample.Metric[model.LabelName("no_tls_reason")])]
				edge := &pb.Edge{
					Src: &pb.Resource{
						Namespace: string(sample.Metric[model.LabelName("namespace")]),
						Name:      string(sample.Metric[model.LabelName(resourceType)]),
						Type:      resourceType,
					},
					Dst: &pb.Resource{
						Namespace: string(sample.Metric[model.LabelName("dst_namespace")]),
						Name:      string(sample.Metric[model.LabelName(resourceReplacementOutbound)]),
						Type:      resourceType,
					},
					NoIdentityMsg: msg,
				}
				edges = append(edges, edge)
			}
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
					Namespace: string(src[model.LabelName("namespace")]),
					Name:      string(src[model.LabelName(resourceType)]),
					Type:      resourceType,
				},
				Dst: &pb.Resource{
					Namespace: string(dst[model.LabelName("namespace")]),
					Name:      string(dst[model.LabelName(resourceType)]),
					Type:      resourceType,
				},
				ClientId:      string(dst[model.LabelName("client_id")]),
				ServerId:      string(src[model.LabelName("server_id")]),
				NoIdentityMsg: msg,
			}
			edges = append(edges, edge)
		}
	}

	return edges
}
