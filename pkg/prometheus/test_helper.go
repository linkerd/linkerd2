package prometheus

import (
	"context"
	"sync"
	"time"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

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
func (m *MockProm) Query(ctx context.Context, query string, ts time.Time, opts ...promv1.Option) (model.Value, promv1.Warnings, error) {
	m.rwLock.Lock()
	defer m.rwLock.Unlock()
	m.QueriesExecuted = append(m.QueriesExecuted, query)
	return m.Res, nil, nil
}

// QueryRange performs a query for the given range.
func (m *MockProm) QueryRange(ctx context.Context, query string, r promv1.Range, opts ...promv1.Option) (model.Value, promv1.Warnings, error) {
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

// LabelValues performs a query for the values of the given label, time range and matchers.
func (m *MockProm) LabelValues(ctx context.Context, label string, matches []string, startTime time.Time, endTime time.Time, opts ...promv1.Option) (model.LabelValues, promv1.Warnings, error) {
	return nil, nil, nil
}

// Series finds series by label matchers.
func (m *MockProm) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time, opts ...promv1.Option) ([]model.LabelSet, promv1.Warnings, error) {
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

// LabelNames returns the unique label names present in the block in sorted order by given time range and matchers.
func (m *MockProm) LabelNames(ctx context.Context, matches []string, startTime time.Time, endTime time.Time, opts ...promv1.Option) ([]string, promv1.Warnings, error) {
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

// Buildinfo returns various build information properties about the Prometheus server
func (m *MockProm) Buildinfo(ctx context.Context) (promv1.BuildinfoResult, error) {
	return promv1.BuildinfoResult{}, nil
}

// QueryExemplars performs a query for exemplars by the given query and time range.
func (m *MockProm) QueryExemplars(ctx context.Context, query string, startTime time.Time, endTime time.Time) ([]promv1.ExemplarQueryResult, error) {
	return []promv1.ExemplarQueryResult{}, nil
}

// TSDB returns the cardinality statistics.
func (m *MockProm) TSDB(ctx context.Context, opts ...promv1.Option) (promv1.TSDBResult, error) {
	return promv1.TSDBResult{}, nil
}

// WalReplay returns the current replay status of the wal.
func (m *MockProm) WalReplay(ctx context.Context) (promv1.WalReplayStatus, error) {
	return promv1.WalReplayStatus{}, nil
}
