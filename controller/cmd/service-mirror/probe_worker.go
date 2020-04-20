package servicemirror

import (
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	logging "github.com/sirupsen/logrus"
)

const httpGatewayTimeoutMillis = 50000

type probeSpec struct {
	ips             []string
	path            string
	port            uint32
	periodInSeconds uint32
}

// ProbeWorker is responsible for monitoring gateways using a probe specification
type ProbeWorker struct {
	*sync.RWMutex
	probeSpec      *probeSpec
	pairedServices map[string]struct{}
	stopCh         chan struct{}
	metrics        *probeMetrics
	log            *logging.Entry
}

// NewProbeWorker creates a new probe worker associated with a particular gateway
func NewProbeWorker(spec *probeSpec, metrics *probeMetrics, probekey string) *ProbeWorker {
	return &ProbeWorker{
		RWMutex:        &sync.RWMutex{},
		probeSpec:      spec,
		pairedServices: make(map[string]struct{}),
		stopCh:         make(chan struct{}),
		metrics:        metrics,
		log: logging.WithFields(logging.Fields{
			"probe-key": probekey,
		}),
	}
}

// NumPairedServices returns the number of paired services for this probe worker
func (pw *ProbeWorker) NumPairedServices() int {
	return len(pw.pairedServices)
}

// PairService increments the number of services that are routed by the gateway
func (pw *ProbeWorker) PairService(serviceName, serviceNamespace string) {
	svcKey := fmt.Sprintf("%s-%s", serviceNamespace, serviceName)
	if _, ok := pw.pairedServices[svcKey]; !ok {
		pw.pairedServices[svcKey] = struct{}{}
		pw.metrics.services.Set(float64(len(pw.pairedServices)))
	}
}

// UnPairService decrements the number of services that are routed by the gateway
func (pw *ProbeWorker) UnPairService(serviceName, serviceNamespace string) {
	svcKey := fmt.Sprintf("%s-%s", serviceNamespace, serviceName)
	if _, ok := pw.pairedServices[svcKey]; ok {
		delete(pw.pairedServices, svcKey)
		pw.metrics.services.Set(float64(len(pw.pairedServices)))
	}
}

// UpdateProbeSpec is used to update the probe specification when something about the gateway changes
func (pw *ProbeWorker) UpdateProbeSpec(spec *probeSpec) {
	pw.Lock()
	pw.probeSpec = spec
	pw.Unlock()
}

// Stop this probe worker
func (pw *ProbeWorker) Stop() {
	pw.log.Debug("Stopping probe worker")
	close(pw.stopCh)
}

// Start this probe worker
func (pw *ProbeWorker) Start() {
	pw.log.Debug("Starting probe worker")
	go pw.run()
}

func (pw *ProbeWorker) run() {
	periodInMillis := pw.probeSpec.periodInSeconds * 1000
	probeTickerPeriod := time.Duration(periodInMillis) * time.Millisecond
	maxJitter := time.Duration(periodInMillis/10) * time.Millisecond // max jitter is 10% of period
	probeTicker := NewTicker(probeTickerPeriod, maxJitter)

probeLoop:
	for {
		select {
		case <-pw.stopCh:
			break probeLoop
		case <-probeTicker.C:
			pw.doProbe()
		}
	}
}

func (pw *ProbeWorker) pickAnIP() string {
	numIps := len(pw.probeSpec.ips)
	if numIps == 0 {
		return ""
	}
	return pw.probeSpec.ips[rand.Int()%numIps]
}

func (pw *ProbeWorker) doProbe() {
	pw.RLock()
	defer pw.RUnlock()

	successLabel := prometheus.Labels{probeSuccessfulLabel: "true"}
	notSuccessLabel := prometheus.Labels{probeSuccessfulLabel: "false"}

	ipToTry := pw.pickAnIP()
	if ipToTry == "" {
		pw.log.Debug("No ips. Marking as unhealthy")
		pw.metrics.alive.Set(0)
	} else {
		client := http.Client{
			Timeout: httpGatewayTimeoutMillis * time.Millisecond,
		}
		start := time.Now()
		resp, err := client.Get(fmt.Sprintf("http://%s:%d/%s", ipToTry, pw.probeSpec.port, pw.probeSpec.path))
		end := time.Since(start)
		if err != nil {
			pw.log.Errorf("Problem connecting with gateway. Marking as unhealthy %s", err)
			pw.metrics.alive.Set(0)
			pw.metrics.probes.With(notSuccessLabel).Inc()
			return
		} else if resp.StatusCode != 200 {
			pw.log.Debugf("Gateway returned unexpected status %d. Marking as unhealthy", resp.StatusCode)
			pw.metrics.alive.Set(0)
			pw.metrics.probes.With(notSuccessLabel).Inc()
		} else {
			pw.log.Debug("Gateway is healthy")
			pw.metrics.alive.Set(1)
			pw.metrics.latencies.Observe(float64(end.Milliseconds()))
			pw.metrics.probes.With(successLabel).Inc()
		}

		if err := resp.Body.Close(); err != nil {
			pw.log.Debugf("Failed to close response body %s", err)
		}

	}
}
