package externalworkload

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"k8s.io/client-go/util/workqueue"
)

// Code is functionally the same as usptream metrics provider. The difference is that
// we rely on promauto instead of init() method for metrics registering as the logic
// used to register the metrics in the upstream implementation uses k8s.io/component-base/metrics/legacyregistry
// The latter does not work with our metrics registry and hence these metrics do not
// end up exposed via our admin's server /metrics endpoint.
//
// https://github.com/kubernetes/component-base/blob/68f947b04ec3a353e63bbef2c1f935bb9ce0061d/metrics/prometheus/workqueue/metrics.go

const (
	WorkQueueSubsystem         = "workqueue"
	DepthKey                   = "depth"
	AddsKey                    = "adds_total"
	QueueLatencyKey            = "queue_duration_seconds"
	WorkDurationKey            = "work_duration_seconds"
	UnfinishedWorkKey          = "unfinished_work_seconds"
	LongestRunningProcessorKey = "longest_running_processor_seconds"
	RetriesKey                 = "retries_total"
	DropsTotalKey              = "drops_total"
)

type queueMetricsProvider struct {
	depth                   *prometheus.GaugeVec
	adds                    *prometheus.CounterVec
	latency                 *prometheus.HistogramVec
	workDuration            *prometheus.HistogramVec
	unfinished              *prometheus.GaugeVec
	longestRunningProcessor *prometheus.GaugeVec
	retries                 *prometheus.CounterVec
	drops                   *prometheus.CounterVec
}

func newWorkQueueMetricsProvider() *queueMetricsProvider {
	return &queueMetricsProvider{
		depth: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Subsystem: WorkQueueSubsystem,
			Name:      DepthKey,
			Help:      "Current depth of workqueue",
		}, []string{"name"}),

		adds: promauto.NewCounterVec(prometheus.CounterOpts{
			Subsystem: WorkQueueSubsystem,
			Name:      AddsKey,
			Help:      "Total number of adds handled by workqueue",
		}, []string{"name"}),

		latency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Subsystem: WorkQueueSubsystem,
			Name:      QueueLatencyKey,
			Help:      "How long in seconds an item stays in workqueue before being requested.",
			Buckets:   prometheus.ExponentialBuckets(10e-9, 10, 10),
		}, []string{"name"}),

		workDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Subsystem: WorkQueueSubsystem,
			Name:      WorkDurationKey,
			Help:      "How long in seconds processing an item from workqueue takes.",
			Buckets:   prometheus.ExponentialBuckets(10e-9, 10, 10),
		}, []string{"name"}),

		unfinished: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Subsystem: WorkQueueSubsystem,
			Name:      UnfinishedWorkKey,
			Help: "How many seconds of work has done that " +
				"is in progress and hasn't been observed by work_duration. Large " +
				"values indicate stuck threads. One can deduce the number of stuck " +
				"threads by observing the rate at which this increases.",
		}, []string{"name"}),

		longestRunningProcessor: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Subsystem: WorkQueueSubsystem,
			Name:      LongestRunningProcessorKey,
			Help: "How many seconds has the longest running " +
				"processor for workqueue been running.",
		}, []string{"name"}),

		retries: promauto.NewCounterVec(prometheus.CounterOpts{
			Subsystem: WorkQueueSubsystem,
			Name:      RetriesKey,
			Help:      "Total number of retries handled by workqueue",
		}, []string{"name"}),
		drops: promauto.NewCounterVec(prometheus.CounterOpts{
			Subsystem: WorkQueueSubsystem,
			Name:      DropsTotalKey,
			Help:      "Total number of dropped items from the queue due to exceeding retry threshold",
		}, []string{"name"}),
	}
}

func (p queueMetricsProvider) NewDepthMetric(name string) workqueue.GaugeMetric {
	return p.depth.WithLabelValues(name)
}

func (p queueMetricsProvider) NewAddsMetric(name string) workqueue.CounterMetric {
	return p.adds.WithLabelValues(name)
}

func (p queueMetricsProvider) NewLatencyMetric(name string) workqueue.HistogramMetric {
	return p.latency.WithLabelValues(name)
}

func (p queueMetricsProvider) NewWorkDurationMetric(name string) workqueue.HistogramMetric {
	return p.workDuration.WithLabelValues(name)
}

func (p queueMetricsProvider) NewUnfinishedWorkSecondsMetric(name string) workqueue.SettableGaugeMetric {
	return p.unfinished.WithLabelValues(name)
}

func (p queueMetricsProvider) NewLongestRunningProcessorSecondsMetric(name string) workqueue.SettableGaugeMetric {
	return p.longestRunningProcessor.WithLabelValues(name)
}

func (p queueMetricsProvider) NewRetriesMetric(name string) workqueue.CounterMetric {
	return p.retries.WithLabelValues(name)
}

func (p queueMetricsProvider) NewDropsMetric(name string) workqueue.CounterMetric {
	return p.drops.WithLabelValues(name)
}

type noopCounterMetric struct{}

func (noopCounterMetric) Inc() {}
