package destination

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestViewMetrics(t *testing.T) {
	// Reset metrics
	reg := prometheus.NewRegistry()
	reg.MustRegister(endpointViewsActive)

	v1 := newViewMetrics()
	v2 := newViewMetrics()
	v3 := newViewMetrics()

	assertGaugeValue(t, endpointViewsActive, 3.0)

	v1.close()
	assertGaugeValue(t, endpointViewsActive, 2.0)

	v2.close()
	v3.close()
	assertGaugeValue(t, endpointViewsActive, 0.0)
}

func TestSendMetrics(t *testing.T) {
	// Reset metrics
	reg := prometheus.NewRegistry()
	reg.MustRegister(streamSendTimeouts, streamSendDuration)

	initialTimeouts := getCounterValue(t, streamSendTimeouts)
	initialCount := getHistogramCount(t, streamSendDuration)

	// Record a timeout
	observeSendTimeout()
	assertCounterValue(t, streamSendTimeouts, initialTimeouts+1)

	// Record send durations
	observeSendDuration(10 * time.Millisecond)
	observeSendDuration(50 * time.Millisecond)
	assertHistogramCount(t, streamSendDuration, initialCount+2)
}

// Helper functions

func assertGaugeValue(t *testing.T, gauge prometheus.Gauge, expected float64) {
	t.Helper()
	metric := &dto.Metric{}
	if err := gauge.Write(metric); err != nil {
		t.Fatalf("Failed to write gauge metric: %v", err)
	}
	actual := metric.Gauge.GetValue()
	if actual != expected {
		t.Errorf("Expected gauge value %v, got %v", expected, actual)
	}
}

func getCounterValue(t *testing.T, counter prometheus.Counter) float64 {
	t.Helper()
	metric := &dto.Metric{}
	if err := counter.Write(metric); err != nil {
		t.Fatalf("Failed to write counter metric: %v", err)
	}
	return metric.Counter.GetValue()
}

func assertCounterValue(t *testing.T, counter prometheus.Counter, expected float64) {
	t.Helper()
	actual := getCounterValue(t, counter)
	if actual != expected {
		t.Errorf("Expected counter value %v, got %v", expected, actual)
	}
}

func getHistogramCount(t *testing.T, histogram prometheus.Histogram) uint64 {
	t.Helper()
	metric := &dto.Metric{}
	if err := histogram.(prometheus.Metric).Write(metric); err != nil {
		t.Fatalf("Failed to write histogram metric: %v", err)
	}
	return metric.Histogram.GetSampleCount()
}

func assertHistogramCount(t *testing.T, histogram prometheus.Histogram, expected uint64) {
	t.Helper()
	actual := getHistogramCount(t, histogram)
	if actual != expected {
		t.Errorf("Expected histogram count %v, got %v", expected, actual)
	}
}
