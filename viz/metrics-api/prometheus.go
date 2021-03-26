package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	"go.opencensus.io/trace"
)

type promType string
type promResult struct {
	prom promType
	vec  model.Vector
	err  error
}

const (
	promGatewayAlive   = promType("QUERY_GATEWAY_ALIVE")
	promRequests       = promType("QUERY_REQUESTS")
	promActualRequests = promType("QUERY_ACTUAL_REQUESTS")
	promTCPConnections = promType("QUERY_TCP_CONNECTIONS")
	promTCPReadBytes   = promType("QUERY_TCP_READ_BYTES")
	promTCPWriteBytes  = promType("QUERY_TCP_WRITE_BYTES")
	promLatencyP50     = promType("0.5")
	promLatencyP95     = promType("0.95")
	promLatencyP99     = promType("0.99")

	namespaceLabel         = model.LabelName("namespace")
	dstNamespaceLabel      = model.LabelName("dst_namespace")
	gatewayNameLabel       = model.LabelName("gateway_name")
	gatewayNamespaceLabel  = model.LabelName("gateway_namespace")
	remoteClusterNameLabel = model.LabelName("target_cluster_name")
)

var (
	// ErrNoPrometheusInstance is returned when there is no prometheus instance configured
	ErrNoPrometheusInstance = errors.New("No prometheus instance to connect")
)

func extractSampleValue(sample *model.Sample) uint64 {
	value := uint64(0)
	if !math.IsNaN(float64(sample.Value)) {
		value = uint64(math.Round(float64(sample.Value)))
	}
	return value
}

func (s *grpcServer) queryProm(ctx context.Context, query string) (model.Vector, error) {
	log.Debugf("Query request:\n\t%+v", query)

	_, span := trace.StartSpan(ctx, "query.prometheus")
	defer span.End()
	span.AddAttributes(trace.StringAttribute("queryString", query))

	if s.prometheusAPI == nil {
		return nil, ErrNoPrometheusInstance
	}

	// single data point (aka summary) query
	res, warn, err := s.prometheusAPI.Query(ctx, query, time.Time{})
	if err != nil {
		log.Errorf("Query(%+v) failed with: %+v", query, err)
		return nil, err
	}
	if warn != nil {
		log.Warnf("%v", warn)
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
	if resource != nil {
		if resource.Name != "" && resource.GetType() != k8s.Service {
			set[promResourceType(resource)] = model.LabelValue(resource.Name)
		}
		if shouldAddNamespaceLabel(resource) {
			set[namespaceLabel] = model.LabelValue(resource.Namespace)
		}
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

// insert a not-nil check into a LabelSet to verify that data for a specified
// label name exists. due to the `!=` this must be inserted as a string. the
// structure of this code is taken from the Prometheus labelset.go library.
func generateLabelStringWithExclusion(l model.LabelSet, labelName string) string {
	lstrs := make([]string, 0, len(l))
	for l, v := range l {
		lstrs = append(lstrs, fmt.Sprintf("%s=%q", l, v))
	}
	lstrs = append(lstrs, fmt.Sprintf(`%s!=""`, labelName))

	sort.Strings(lstrs)
	return fmt.Sprintf("{%s}", strings.Join(lstrs, ", "))
}

// insert a regex-match check into a LabelSet for labels that match the provided
// string. this is modeled on generateLabelStringWithExclusion().
func generateLabelStringWithRegex(l model.LabelSet, labelName string, stringToMatch string) string {
	lstrs := make([]string, 0, len(l))
	for l, v := range l {
		lstrs = append(lstrs, fmt.Sprintf("%s=%q", l, v))
	}
	lstrs = append(lstrs, fmt.Sprintf(`%s=~"^%s.+"`, labelName, stringToMatch))

	sort.Strings(lstrs)
	return fmt.Sprintf("{%s}", strings.Join(lstrs, ", "))
}

// generate Prometheus queries for latency quantiles, based on a quantile query
// template, query labels, a time window and grouping.
func generateQuantileQueries(quantileQuery, labels, timeWindow, groupBy string) map[promType]string {
	return map[promType]string{
		promLatencyP50: fmt.Sprintf(quantileQuery, promLatencyP50, labels, timeWindow, groupBy),
		promLatencyP95: fmt.Sprintf(quantileQuery, promLatencyP95, labels, timeWindow, groupBy),
		promLatencyP99: fmt.Sprintf(quantileQuery, promLatencyP99, labels, timeWindow, groupBy),
	}
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

func promPeerLabel(peer string) model.LabelSet {
	return model.LabelSet{
		model.LabelName("peer"): model.LabelValue(peer),
	}
}

func promResourceType(resource *pb.Resource) model.LabelName {
	l5dLabel := k8s.KindToL5DLabel(resource.Type)
	return model.LabelName(l5dLabel)
}

func (s *grpcServer) getPrometheusMetrics(ctx context.Context, requestQueries map[promType]string, latencyQueries map[promType]string) ([]promResult, error) {
	resultChan := make(chan promResult)

	for pt, query := range requestQueries {
		go func(typ promType, promQuery string) {
			resultVector, err := s.queryProm(ctx, promQuery)
			resultChan <- promResult{
				prom: typ,
				vec:  resultVector,
				err:  err,
			}
		}(pt, query)
	}

	for quantile, query := range latencyQueries {
		go func(qt promType, promQuery string) {
			resultVector, err := s.queryProm(ctx, promQuery)
			resultChan <- promResult{
				prom: qt,
				vec:  resultVector,
				err:  err,
			}
		}(quantile, query)
	}
	// process results, receive one message per prometheus query type
	var err error
	results := []promResult{}
	for i := 0; i < len(latencyQueries)+len(requestQueries); i++ {
		result := <-resultChan
		if result.err != nil {
			log.Errorf("queryProm failed with: %s", result.err)
			err = result.err
		} else {
			results = append(results, result)
		}
	}
	if err != nil {
		return nil, err
	}

	return results, nil
}
