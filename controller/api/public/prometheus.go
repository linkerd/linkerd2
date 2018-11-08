package public

import (
	"context"
	"fmt"
	"math"
	"time"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
)

type promType string
type promResult struct {
	prom promType
	vec  model.Vector
	err  error
}

const (
	promRequests   = promType("QUERY_REQUESTS")
	promLatencyP50 = promType("0.5")
	promLatencyP95 = promType("0.95")
	promLatencyP99 = promType("0.99")

	namespaceLabel    = model.LabelName("namespace")
	dstNamespaceLabel = model.LabelName("dst_namespace")
)

var promTypes = []promType{promRequests, promLatencyP50, promLatencyP95, promLatencyP99}

func extractSampleValue(sample *model.Sample) uint64 {
	value := uint64(0)
	if !math.IsNaN(float64(sample.Value)) {
		value = uint64(math.Round(float64(sample.Value)))
	}
	return value
}

func isInvalidServiceRequest(selector *pb.ResourceSelection, fromResource *pb.Resource) bool {
	if fromResource != nil {
		return fromResource.Type == k8s.Service
	} else {
		return selector.Resource.Type == k8s.Service
	}
}

func (s *grpcServer) queryProm(ctx context.Context, query string) (model.Vector, error) {
	log.Debugf("Query request:\n\t%+v", query)

	// single data point (aka summary) query
	res, err := s.prometheusAPI.Query(ctx, query, time.Time{})
	if err != nil {
		log.Errorf("Query(%+v) failed with: %+v", query, err)
		return nil, err
	}
	log.Debugf("Query response:\n\t%+v", res)

	if res.Type() != model.ValVector {
		err = fmt.Errorf("Unexpected query result type (expected Vector): %s", res.Type())
		log.Error(err)
		return nil, err
	}

	return res.(model.Vector), nil
}

// add filtering by resource type
// note that metricToKey assumes the label ordering (namespace, name)
func promGroupByLabelNames(resource *pb.Resource) model.LabelNames {
	names := model.LabelNames{namespaceLabel}

	if resource.Type != k8s.Namespace {
		names = append(names, promResourceType(resource))
	}
	return names
}

// add filtering by resource type
// note that metricToKey assumes the label ordering (namespace, name)
func promDstGroupByLabelNames(resource *pb.Resource) model.LabelNames {
	names := model.LabelNames{dstNamespaceLabel}

	if isNonK8sResourceQuery(resource.GetType()) {
		names = append(names, promResourceType(resource))
	} else if resource.Type != k8s.Namespace {
		names = append(names, "dst_"+promResourceType(resource))
	}
	return names
}

// query a named resource
func promQueryLabels(resource *pb.Resource) model.LabelSet {
	set := model.LabelSet{}
	if resource.Name != "" {
		set[promResourceType(resource)] = model.LabelValue(resource.Name)
	}
	if shouldAddNamespaceLabel(resource) {
		set[namespaceLabel] = model.LabelValue(resource.Namespace)
	}
	return set
}

// query a named resource
func promDstQueryLabels(resource *pb.Resource) model.LabelSet {
	set := model.LabelSet{}
	if resource.Name != "" {
		if isNonK8sResourceQuery(resource.GetType()) {
			set[promResourceType(resource)] = model.LabelValue(resource.Name)
		} else {
			set["dst_"+promResourceType(resource)] = model.LabelValue(resource.Name)
			if shouldAddNamespaceLabel(resource) {
				set[dstNamespaceLabel] = model.LabelValue(resource.Namespace)
			}
		}
	}

	return set
}

// determine if we should add "namespace=<namespace>" to a named query
func shouldAddNamespaceLabel(resource *pb.Resource) bool {
	return resource.Type != k8s.Namespace && resource.Namespace != ""
}

// query for inbound or outbound requests
func promDirectionLabels(direction string) model.LabelSet {
	return model.LabelSet{
		model.LabelName("direction"): model.LabelValue(direction),
	}
}

func promResourceType(resource *pb.Resource) model.LabelName {
	return model.LabelName(resource.Type)
}
