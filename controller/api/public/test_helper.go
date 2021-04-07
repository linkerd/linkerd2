package public

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	destinationPb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2-proxy-api/go/net"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"google.golang.org/grpc"
)

// MockAPIClient satisfies the Public API's gRPC interfaces (public.APIClient).
type MockAPIClient struct {
	ErrorToReturn                error
	DestinationGetClientToReturn destinationPb.Destination_GetClient
}

// Get provides a mock of a Public API method.
func (c *MockAPIClient) Get(ctx context.Context, in *destinationPb.GetDestination, opts ...grpc.CallOption) (destinationPb.Destination_GetClient, error) {
	return c.DestinationGetClientToReturn, c.ErrorToReturn
}

// GetProfile provides a mock of a Public API method
func (c *MockAPIClient) GetProfile(ctx context.Context, _ *destinationPb.GetDestination, _ ...grpc.CallOption) (destinationPb.Destination_GetProfileClient, error) {
	// Not implemented through this client. The proxies use the gRPC server directly instead.
	return nil, errors.New("Not implemented")
}

// MockDestinationGetClient satisfies the Destination_GetClient gRPC interface.
type MockDestinationGetClient struct {
	UpdatesToReturn []destinationPb.Update
	ErrorsToReturn  []error
	grpc.ClientStream
	sync.Mutex
}

// Recv satisfies the Destination_GetClient.Recv() gRPC method.
func (a *MockDestinationGetClient) Recv() (*destinationPb.Update, error) {
	a.Lock()
	defer a.Unlock()
	var updatePopped *destinationPb.Update
	var errorPopped error
	if len(a.UpdatesToReturn) == 0 && len(a.ErrorsToReturn) == 0 {
		return nil, io.EOF
	}
	if len(a.UpdatesToReturn) != 0 {
		updatePopped, a.UpdatesToReturn = &a.UpdatesToReturn[0], a.UpdatesToReturn[1:]
	}
	if len(a.ErrorsToReturn) != 0 {
		errorPopped, a.ErrorsToReturn = a.ErrorsToReturn[0], a.ErrorsToReturn[1:]
	}

	return updatePopped, errorPopped
}

// AuthorityEndpoints holds the details for the Endpoints associated to an authority
type AuthorityEndpoints struct {
	Namespace string
	ServiceID string
	Pods      []PodDetails
}

// PodDetails holds the details for pod associated to an Endpoint
type PodDetails struct {
	Name string
	IP   uint32
	Port uint32
}

// BuildAddrSet converts AuthorityEndpoints into its protobuf representation
func BuildAddrSet(endpoint AuthorityEndpoints) *destinationPb.WeightedAddrSet {
	addrs := make([]*destinationPb.WeightedAddr, 0)
	for _, pod := range endpoint.Pods {
		addr := &net.TcpAddress{
			Ip:   &net.IPAddress{Ip: &net.IPAddress_Ipv4{Ipv4: pod.IP}},
			Port: pod.Port,
		}
		labels := map[string]string{"pod": pod.Name}
		weightedAddr := &destinationPb.WeightedAddr{Addr: addr, MetricLabels: labels}
		addrs = append(addrs, weightedAddr)
	}
	labels := map[string]string{"namespace": endpoint.Namespace, "service": endpoint.ServiceID}
	return &destinationPb.WeightedAddrSet{Addrs: addrs, MetricLabels: labels}
}

//
// Prometheus client
//

// MockProm satisfies the promv1.API interface for testing.
// TODO: move this into something shared under /controller, or into /pkg
type MockProm struct {
	Res             model.Value
	QueriesExecuted []string // expose the queries our Mock Prometheus receives, to test query generation
	rwLock          sync.Mutex
}

// Query performs a query for the given time.
func (m *MockProm) Query(ctx context.Context, query string, ts time.Time) (model.Value, promv1.Warnings, error) {
	m.rwLock.Lock()
	defer m.rwLock.Unlock()
	m.QueriesExecuted = append(m.QueriesExecuted, query)
	return m.Res, nil, nil
}

// QueryRange performs a query for the given range.
func (m *MockProm) QueryRange(ctx context.Context, query string, r promv1.Range) (model.Value, promv1.Warnings, error) {
	m.rwLock.Lock()
	defer m.rwLock.Unlock()
	m.QueriesExecuted = append(m.QueriesExecuted, query)
	return m.Res, nil, nil
}

// AlertManagers returns an overview of the current state of the Prometheus alert
// manager discovery.
func (m *MockProm) AlertManagers(ctx context.Context) (promv1.AlertManagersResult, error) {
	return promv1.AlertManagersResult{}, nil
}

// Alerts returns a list of all active alerts.
func (m *MockProm) Alerts(ctx context.Context) (promv1.AlertsResult, error) {
	return promv1.AlertsResult{}, nil
}

// CleanTombstones removes the deleted data from disk and cleans up the existing
// tombstones.
func (m *MockProm) CleanTombstones(ctx context.Context) error {
	return nil
}

// Config returns the current Prometheus configuration.
func (m *MockProm) Config(ctx context.Context) (promv1.ConfigResult, error) {
	return promv1.ConfigResult{}, nil
}

// DeleteSeries deletes data for a selection of series in a time range.
func (m *MockProm) DeleteSeries(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) error {
	return nil
}

// Flags returns the flag values that Prometheus was launched with.
func (m *MockProm) Flags(ctx context.Context) (promv1.FlagsResult, error) {
	return promv1.FlagsResult{}, nil
}

// LabelValues performs a query for the values of the given label.
func (m *MockProm) LabelValues(ctx context.Context, label string, startTime time.Time, endTime time.Time) (model.LabelValues, promv1.Warnings, error) {
	return nil, nil, nil
}

// Series finds series by label matchers.
func (m *MockProm) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, promv1.Warnings, error) {
	return nil, nil, nil
}

// Snapshot creates a snapshot of all current data into
// snapshots/<datetime>-<rand> under the TSDB's data directory and returns the
// directory as response.
func (m *MockProm) Snapshot(ctx context.Context, skipHead bool) (promv1.SnapshotResult, error) {
	return promv1.SnapshotResult{}, nil
}

// Targets returns an overview of the current state of the Prometheus target
// discovery.
func (m *MockProm) Targets(ctx context.Context) (promv1.TargetsResult, error) {
	return promv1.TargetsResult{}, nil
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (m *MockProm) LabelNames(ctx context.Context, startTime time.Time, endTime time.Time) ([]string, promv1.Warnings, error) {
	return []string{}, nil, nil
}

// Runtimeinfo returns the runtime info about Prometheus
func (m *MockProm) Runtimeinfo(ctx context.Context) (promv1.RuntimeinfoResult, error) {
	return promv1.RuntimeinfoResult{}, nil
}

// Metadata returns the metadata of the specified metric
func (m *MockProm) Metadata(ctx context.Context, metric string, limit string) (map[string][]promv1.Metadata, error) {
	return nil, nil
}

// Rules returns a list of alerting and recording rules that are currently loaded.
func (m *MockProm) Rules(ctx context.Context) (promv1.RulesResult, error) {
	return promv1.RulesResult{}, nil
}

// TargetsMetadata returns metadata about metrics currently scraped by the target.
func (m *MockProm) TargetsMetadata(ctx context.Context, matchTarget string, metric string, limit string) ([]promv1.MetricMetadata, error) {
	return []promv1.MetricMetadata{}, nil
}
