package api

import (
	"context"
	"fmt"
	"sort"

	"github.com/linkerd/linkerd2/pkg/k8s"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/linkerd/linkerd2/viz/pkg/prometheus"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
)

const (
	edgesQuery = "sum(%s%s) by (%s, dst_%s, pod, server_id, namespace, dst_namespace, no_tls_reason)"
)

var formatMsg = map[string]string{
	"disabled":                          "Disabled",
	"loopback":                          "Loopback",
	"no_authority_in_http_request":      "No Authority In HTTP Request",
	"not_http":                          "Not HTTP",
	"not_provided_by_remote":            "Not Provided By Remote",
	"not_provided_by_service_discovery": "Not Provided By Service Discovery",
}

type edgeKey struct {
	src   string
	srcNs string
	dst   string
	dstNs string
}

func (s *grpcServer) Edges(ctx context.Context, req *pb.EdgesRequest) (*pb.EdgesResponse, error) {
	log.Debugf("Edges request: %+v", req)
	if req.GetSelector().GetResource() == nil {
		return edgesError(req, "Edges request missing Selector Resource"), nil
	}

	resourceType := prometheus.ResourceType(req.GetSelector().GetResource())
	dstResourceType := "dst_" + resourceType
	labelsOutbound := promDirectionLabels("outbound")
	labelsOutboundStr := generateLabelStringWithExclusion(labelsOutbound, string(resourceType), string(dstResourceType))
	query := fmt.Sprintf(edgesQuery, "tcp_open_connections", labelsOutboundStr, resourceType, resourceType)

	promResult, err := s.queryProm(ctx, query)
	if err != nil {
		return edgesError(req, err.Error()), nil
	}

	edgeMap := make(map[edgeKey]*pb.Edge)

	for _, sample := range promResult {
		if sample.Value == 0.0 {
			continue
		}
		key := edgeKey{
			src:   string(sample.Metric[resourceType]),
			srcNs: string(sample.Metric[model.LabelName("namespace")]),
			dst:   string(sample.Metric[dstResourceType]),
			dstNs: string(sample.Metric[model.LabelName("dst_namespace")]),
		}
		requestedNs := req.GetSelector().GetResource().GetNamespace()
		if requestedNs != v1.NamespaceAll {
			if requestedNs != key.srcNs && requestedNs != key.dstNs {
				continue
			}
		}
		if _, ok := edgeMap[key]; !ok {

			clientID, err := s.getPodIdentity(string(sample.Metric[model.LabelName("pod")]), key.srcNs)
			if err != nil {
				log.Warnf("failed to get pod identity for %s: %v", sample.Metric[model.LabelName("pod")], err)
				continue
			}

			edgeMap[key] = &pb.Edge{
				Src: &pb.Resource{
					Namespace: key.srcNs,
					Name:      key.src,
					Type:      string(resourceType),
				},
				Dst: &pb.Resource{
					Namespace: key.dstNs,
					Name:      key.dst,
					Type:      string(resourceType),
				},
				ServerId:      string(sample.Metric[model.LabelName("server_id")]),
				ClientId:      clientID,
				NoIdentityMsg: formatMsg[string(sample.Metric[model.LabelName("no_tls_reason")])],
			}
		}
	}

	edges := []*pb.Edge{}
	for _, edge := range edgeMap {
		edges = append(edges, edge)
	}
	edges = sortEdgeRows(edges)

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

func (s *grpcServer) getPodIdentity(pod string, namespace string) (string, error) {
	po, err := s.k8sAPI.Pod().Lister().Pods(namespace).Get(pod)
	if err != nil {
		return "", err
	}
	return k8s.PodIdentity(po)
}

func sortEdgeRows(rows []*pb.Edge) []*pb.Edge {
	sort.Slice(rows, func(i, j int) bool {
		keyI := rows[i].GetSrc().GetNamespace() + rows[i].GetDst().GetNamespace() + rows[i].GetSrc().GetName() + rows[i].GetDst().GetName()
		keyJ := rows[j].GetSrc().GetNamespace() + rows[j].GetDst().GetNamespace() + rows[j].GetSrc().GetName() + rows[j].GetDst().GetName()
		return keyI < keyJ
	})
	return rows
}
