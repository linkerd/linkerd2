package servicemirror

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/linkerd/linkerd2/controller/gen/apis/link/v1alpha3"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/prometheus/client_golang/prometheus"
	logging "github.com/sirupsen/logrus"
	corelisters "k8s.io/client-go/listers/core/v1"
)

// ProbeWorker is responsible for monitoring gateways using a probe specification
type ProbeWorker struct {
	localGatewayName string
	localServices    corelisters.ServiceNamespaceLister
	alive            bool
	Liveness         chan bool
	*sync.RWMutex
	probeSpec *v1alpha3.ProbeSpec
	stopCh    chan struct{}
	metrics   *ProbeMetrics
	log       *logging.Entry
}

// NewProbeWorker creates a new probe worker associated with a particular
// gateway. localGatewayName is the name of the local probe service mirroring
// the gateway, and localServices is used to look it up in order to find the
// port it exposes.
func NewProbeWorker(localGatewayName string, localServices corelisters.ServiceNamespaceLister, spec *v1alpha3.ProbeSpec, metrics *ProbeMetrics, probekey string) *ProbeWorker {
	metrics.gatewayEnabled.Set(1)
	return &ProbeWorker{
		localGatewayName: localGatewayName,
		localServices:    localServices,
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
func (pw *ProbeWorker) UpdateProbeSpec(spec *v1alpha3.ProbeSpec) {
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
	probeTickerPeriod, err := time.ParseDuration(pw.probeSpec.Period)
	if err != nil {
		pw.log.Errorf("could not parse probe period: %s", err)
		return
	}
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

				counter, err := pw.metrics.probes.GetMetricWith(notSuccessLabel)
				if err != nil {
					pw.log.Errorf("failed to get probe metric: %q", err)
				} else {
					counter.Inc()
				}
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
				counter, err := pw.metrics.probes.GetMetricWith(successLabel)
				if err != nil {
					pw.log.Errorf("failed to get probe metric: %q", err)
				} else {
					counter.Inc()
				}
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

	urlAddress := net.JoinHostPort(pw.localGatewayName, pw.probePort())
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

// probePort returns the port to send probe requests to. Probes are sent to
// the local probe service, whose mc-probe port is mapped by its Endpoints to
// the gateway's probe port in the target cluster (i.e. the probe spec's
// port). These ports can differ, e.g. when the gateway is exposed through a
// NodePort service, in which case the probe spec's port is the node port.
// Since the proxy rejects connections to ports that are not defined by the
// service, the local service's port must be used. The probe spec's port is
// only used as a fallback when the local service's port cannot be determined.
func (pw *ProbeWorker) probePort() string {
	if pw.localServices == nil {
		return pw.probeSpec.Port
	}

	svc, err := pw.localServices.Get(pw.localGatewayName)
	if err != nil {
		pw.log.Debugf("Failed to get probe service %s, using probe spec port %s: %s", pw.localGatewayName, pw.probeSpec.Port, err)
		return pw.probeSpec.Port
	}

	for _, port := range svc.Spec.Ports {
		if port.Name == consts.ProbePortName {
			return strconv.Itoa(int(port.Port))
		}
	}

	pw.log.Debugf("Probe service %s has no port named %s, using probe spec port %s", pw.localGatewayName, consts.ProbePortName, pw.probeSpec.Port)
	return pw.probeSpec.Port
}
