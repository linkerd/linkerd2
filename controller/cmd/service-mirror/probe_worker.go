package servicemirror

import (
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"

	logging "github.com/sirupsen/logrus"
)

// ProbeWorker is responsible for monitoring gateways using a probe specification
type ProbeWorker struct {
	*sync.RWMutex
	probe                 *GatewayProbeSpec
	numAssociatedServices int32
	stopCh                chan struct{}
	metrics               *probeMetrics
	log                   *logging.Entry
}

// NewProbeWorker creates a new probe worker associated with a particular gateway
func NewProbeWorker(probe *GatewayProbeSpec, metrics *probeMetrics, probekey string) *ProbeWorker {
	return &ProbeWorker{
		RWMutex:               &sync.RWMutex{},
		probe:                 probe,
		numAssociatedServices: 0,
		stopCh:                make(chan struct{}, 1),
		metrics:               metrics,
		log: logging.WithFields(logging.Fields{
			"probe-key": probekey,
		}),
	}
}

// IncNumServices increments the number of services that are routed by the gateway
func (pw *ProbeWorker) IncNumServices() {
	pw.numAssociatedServices = pw.numAssociatedServices + 1
	pw.metrics.services.Inc()
}

// DcrNumServices decrements the number of services that are routed by the gateway
func (pw *ProbeWorker) DcrNumServices() {
	pw.numAssociatedServices = pw.numAssociatedServices - 1
	pw.metrics.services.Dec()
}

// UpdateProbeSpec is used to update the probe specification when something about the gateway changes
func (pw *ProbeWorker) UpdateProbeSpec(spec *GatewayProbeSpec) {
	pw.Lock()
	pw.probe = spec
	pw.Unlock()
}

// Stop this probe worker
func (pw *ProbeWorker) Stop() {
	pw.log.Debug("Stopping probe worker for")
	close(pw.stopCh)
}

// Start this probe worker
func (pw *ProbeWorker) Start() {
	pw.log.Debug("Starting probe worker")
	go pw.run()
}

func (pw *ProbeWorker) run() {
	probeTickerPeriod := time.Duration(pw.probe.periodSeconds) * time.Second

	// Introduce some randomness to avoid bursts of probes
	time.Sleep(time.Duration(rand.Float64() * float64(probeTickerPeriod)))

	probeTicker := time.NewTicker(probeTickerPeriod)

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
	numIps := len(pw.probe.gatewayIps)
	if numIps == 0 {
		return ""
	}
	return pw.probe.gatewayIps[rand.Int()%numIps]
}

func (pw *ProbeWorker) doProbe() {
	pw.RLock()
	defer pw.RUnlock()

	ipToTry := pw.pickAnIP()
	if ipToTry == "" {
		pw.log.Debug("No ips. Marking as unhealthy")
		pw.metrics.alive.Set(0)
	} else {
		start := time.Now()
		resp, err := http.Get(fmt.Sprintf("http://%s:%d/%s", ipToTry, pw.probe.port, pw.probe.path))
		end := time.Since(start)
		if err != nil {
			pw.log.Errorf("Problem connecting with gateway. Marking as unhealthy %s", err)
			pw.metrics.alive.Set(0)
		} else if resp.StatusCode != 200 {
			pw.log.Debugf("Gateway returned unexpected status %d. Marking as unhealthy", resp.StatusCode)
			pw.metrics.alive.Set(0)
		} else {
			pw.log.Debug("Gateway is healthy")
			pw.metrics.alive.Set(1)
			pw.metrics.latencies.Observe(float64(end.Milliseconds()))
		}

		if err := resp.Body.Close(); err != nil {
			pw.log.Debugf("Failed to close response body %s", err)
		}

	}
}
