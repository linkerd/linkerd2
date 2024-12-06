package servicemirror

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/linkerd/linkerd2/controller/gen/apis/link/v1alpha2"
	"github.com/prometheus/client_golang/prometheus"
	logging "github.com/sirupsen/logrus"
)

// ProbeWorker is responsible for monitoring gateways using a probe specification
type ProbeWorker struct {
	localGatewayName string
	alive            bool
	Liveness         chan bool
	*sync.RWMutex
	probeSpec *v1alpha2.ProbeSpec
	stopCh    chan struct{}
	metrics   *ProbeMetrics
	log       *logging.Entry
}

// NewProbeWorker creates a new probe worker associated with a particular gateway
func NewProbeWorker(localGatewayName string, spec *v1alpha2.ProbeSpec, metrics *ProbeMetrics, probekey string) *ProbeWorker {
	metrics.gatewayEnabled.Set(1)
	return &ProbeWorker{
		localGatewayName: localGatewayName,
		Liveness:         make(chan bool, 10),
		RWMutex:          &sync.RWMutex{},
		probeSpec:        spec,
		stopCh:           make(chan struct{}),
		metrics:          metrics,
		log: logging.WithFields(logging.Fields{
			"probe-key": probekey,
		}),
	}
}

// UpdateProbeSpec is used to update the probe specification when something about the gateway changes
func (pw *ProbeWorker) UpdateProbeSpec(spec *v1alpha2.ProbeSpec) {
	pw.Lock()
	pw.probeSpec = spec
	pw.Unlock()
}

// Stop this probe worker
func (pw *ProbeWorker) Stop() {
	pw.metrics.unregister()
	pw.log.Infof("Stopping probe worker")
	close(pw.stopCh)
}

// Start this probe worker
func (pw *ProbeWorker) Start() {

	pw.log.Infof("Starting probe worker")
	go pw.run()
}

func (pw *ProbeWorker) run() {
	successLabel := prometheus.Labels{probeSuccessfulLabel: "true"}
	notSuccessLabel := prometheus.Labels{probeSuccessfulLabel: "false"}

	if pw.probeSpec == nil {
		pw.log.Error("Probe spec is nil")
		return
	}
	probeTickerPeriodSeconds, err := strconv.ParseInt(pw.probeSpec.Period, 10, 64)
	if err != nil {
		pw.log.Errorf("could not parse probe period: %s", err)
		return
	}
	probeTickerPeriod := time.Duration(probeTickerPeriodSeconds) * time.Second
	maxJitter := probeTickerPeriod / 10 // max jitter is 10% of period
	probeTicker := NewTicker(probeTickerPeriod, maxJitter)
	defer probeTicker.Stop()

	failureThreshold, err := strconv.ParseUint(pw.probeSpec.FailureThreshold, 10, 32)
	if err != nil {
		pw.log.Errorf("could not parse failure threshold: %s", err)
		return
	}
	var failures uint64 = 0

probeLoop:
	for {
		select {
		case <-pw.stopCh:
			break probeLoop
		case <-probeTicker.C:
			start := time.Now()
			if err := pw.doProbe(); err != nil {
				pw.log.Warn(err)
				failures++
				if failures < failureThreshold {
					continue probeLoop
				}

				pw.log.Warnf("Failure threshold (%s) reached - Marking as unhealthy", pw.probeSpec.FailureThreshold)
				pw.metrics.alive.Set(0)
				pw.metrics.probes.With(notSuccessLabel).Inc()
				if pw.alive {
					pw.alive = false
					pw.Liveness <- false
				}
			} else {
				end := time.Since(start)
				failures = 0

				pw.log.Debug("Gateway is healthy")
				pw.metrics.alive.Set(1)
				pw.metrics.latency.Set(float64(end.Milliseconds()))
				pw.metrics.latencies.Observe(float64(end.Milliseconds()))
				pw.metrics.probes.With(successLabel).Inc()
				if !pw.alive {
					pw.alive = true
					pw.Liveness <- true
				}
			}
		}
	}
}

func (pw *ProbeWorker) doProbe() error {
	pw.RLock()
	defer pw.RUnlock()

	timeout, err := time.ParseDuration(pw.probeSpec.Timeout)
	if err != nil {
		return fmt.Errorf("could not parse timeout: %w", err)
	}
	client := http.Client{
		Timeout: timeout,
	}

	urlAddress := net.JoinHostPort(pw.localGatewayName, pw.probeSpec.Port)
	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s%s", urlAddress, pw.probeSpec.Path), nil)
	if err != nil {
		return fmt.Errorf("could not create a GET request to gateway: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("problem connecting with gateway: %w", err)
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("gateway returned unexpected status %d", resp.StatusCode)
	}

	if err := resp.Body.Close(); err != nil {
		pw.log.Warnf("Failed to close response body %s", err)
	}

	return nil
}
