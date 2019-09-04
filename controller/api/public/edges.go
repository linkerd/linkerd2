package public

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

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
	labelsOutbound := promDirectionLabels("outbound")
	labelsInbound := promDirectionLabels("inbound")

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

	for _, sample := range inbound {
		// skip inbound results without a clientID because we cannot construct edge
		// information
		if clientID, ok := sample.Metric[model.LabelName("client_id")]; ok {
			dstResource := string(sample.Metric[model.LabelName(resourceReplacementInbound)])

			// format of clientId is id.namespace.serviceaccount.cluster.local
			clientIDSlice := strings.Split(string(clientID), ".")
			srcNs := clientIDSlice[1]
			key := model.LabelValue(fmt.Sprintf("%s.%s", dstResource, srcNs))
			dstIndex[key] = sample.Metric
		}
	}

	for _, sample := range outbound {
		dstResource := sample.Metric[model.LabelName(resourceReplacementOutbound)]
		srcNs := sample.Metric[model.LabelName("namespace")]

		key := model.LabelValue(fmt.Sprintf("%s.%s", dstResource, srcNs))
		if _, ok := srcIndex[key]; !ok {
			srcIndex[key] = []model.Metric{}
		}
		srcIndex[key] = append(srcIndex[key], sample.Metric)
	}

	for key, sources := range srcIndex {
		for _, src := range sources {
			srcNamespace := string(src[model.LabelName("namespace")])

			dst, ok := dstIndex[key]

			// if no destination, build edge entirely from source data
			if !ok {
				dstNamespace := string(src[model.LabelName("dst_namespace")])

				// skip if selected namespace is given and neither the source nor
				// destination is in the selected namespace
				if selectedNamespace != "" && srcNamespace != selectedNamespace &&
					dstNamespace != selectedNamespace {
					continue
				}

				srcResource := string(src[model.LabelName(resourceType)])
				dstResource := string(src[model.LabelName(resourceReplacementOutbound)])

				// skip if source or destination resource is not present
				if srcResource == "" || dstResource == "" {
					continue
				}

				msg := formatMsg[string(src[model.LabelName("no_tls_reason")])]
				edge := &pb.Edge{
					Src: &pb.Resource{
						Namespace: srcNamespace,
						Name:      srcResource,
						Type:      resourceType,
					},
					Dst: &pb.Resource{
						Namespace: dstNamespace,
						Name:      dstResource,
						Type:      resourceType,
					},
					NoIdentityMsg: msg,
				}
				edges = append(edges, edge)
				continue
			}

			dstNamespace := string(dst[model.LabelName("namespace")])

			// skip if selected namespace is given and neither the source nor
			// destination is in the selected namespace
			if selectedNamespace != "" && srcNamespace != selectedNamespace &&
				dstNamespace != selectedNamespace {
				continue
			}

			edge := &pb.Edge{
				Src: &pb.Resource{
					Namespace: srcNamespace,
					Name:      string(src[model.LabelName(resourceType)]),
					Type:      resourceType,
				},
				Dst: &pb.Resource{
					Namespace: dstNamespace,
					Name:      string(dst[model.LabelName(resourceType)]),
					Type:      resourceType,
				},
				ClientId: string(dst[model.LabelName("client_id")]),
				ServerId: string(src[model.LabelName("server_id")]),
			}
			edges = append(edges, edge)
		}
	}

	// sort rows before returning in order to have a consistent order for tests
	edges = sortEdgeRows(edges)

	return edges
}

func sortEdgeRows(rows []*pb.Edge) []*pb.Edge {
	sort.Slice(rows, func(i, j int) bool {
		keyI := rows[i].GetSrc().GetNamespace() + rows[i].GetDst().GetNamespace() + rows[i].GetSrc().GetName() + rows[i].GetDst().GetName()
		keyJ := rows[j].GetSrc().GetNamespace() + rows[j].GetDst().GetNamespace() + rows[j].GetSrc().GetName() + rows[j].GetDst().GetName()
		return keyI < keyJ
	})
	return rows
}
