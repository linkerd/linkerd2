package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

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
	promGatewayAlive    = promType("QUERY_GATEWAY_ALIVE")
	promRequests        = promType("QUERY_REQUESTS")
	promAllowedRequests = promType("QUERY_ALLOWED_REQUESTS")
	promDeniedRequests  = promType("QUERY_DENIED_REQUESTS")
	promActualRequests  = promType("QUERY_ACTUAL_REQUESTS")
	promTCPConnections  = promType("QUERY_TCP_CONNECTIONS")
	promTCPReadBytes    = promType("QUERY_TCP_READ_BYTES")
	promTCPWriteBytes   = promType("QUERY_TCP_WRITE_BYTES")
	promLatencyP50      = promType("0.5")
	promLatencyP95      = promType("0.95")
	promLatencyP99      = promType("0.99")
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
	log.Debugf("Query request: %q", query)

	_, span := trace.StartSpan(ctx, "query.prometheus")
	defer span.End()
	span.AddAttributes(trace.StringAttribute("queryString", query))

	if s.prometheusAPI == nil {
		return nil, ErrNoPrometheusInstance
	}

	// single data point (aka summary) query
	res, warn, err := s.prometheusAPI.Query(ctx, query, time.Time{})
	if err != nil {
		return nil, fmt.Errorf("Query failed: %q: %w", query, err)
	}
	if warn != nil {
		log.Warnf("%v", warn)
	}
	log.Debugf("Query response:\n\t%+v", res)

	if res.Type() != model.ValVector {
		return nil, fmt.Errorf("Unexpected query result type (expected Vector): %s", res.Type())
	}

	return res.(model.Vector), nil
}

// insert a not-nil check into a LabelSet to verify that data for a specified
// label name exists. due to the `!=` this must be inserted as a string. the
// structure of this code is taken from the Prometheus labelset.go library.
func generateLabelStringWithExclusion(l model.LabelSet, labelNames ...string) string {
	lstrs := make([]string, 0, len(l))
	for l, v := range l {
		lstrs = append(lstrs, fmt.Sprintf("%s=%q", l, v))
	}
	for _, labelName := range labelNames {
		lstrs = append(lstrs, fmt.Sprintf(`%s!=""`, labelName))
	}

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
	lstrs = append(lstrs, fmt.Sprintf(`%s=~"^%s.*"`, labelName, stringToMatch))

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
