package servicemirror

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/linkerd/linkerd2/pkg/multicluster"
	"github.com/prometheus/client_golang/prometheus"
	logging "github.com/sirupsen/logrus"
)

const httpGatewayTimeoutMillis = 50000

// ProbeWorker is responsible for monitoring gateways using a probe specification
type ProbeWorker struct {
	localGatewayName string
	alive            bool
	Liveness         chan bool
	*sync.RWMutex
	probeSpec *multicluster.ProbeSpec
	stopCh    chan struct{}
	metrics   *ProbeMetrics
	log       *logging.Entry
}

// NewProbeWorker creates a new probe worker associated with a particular gateway
func NewProbeWorker(localGatewayName string, spec *multicluster.ProbeSpec, metrics *ProbeMetrics, probekey string) *ProbeWorker {
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
func (pw *ProbeWorker) UpdateProbeSpec(spec *multicluster.ProbeSpec) {
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
	probeTickerPeriod := pw.probeSpec.Period
	maxJitter := pw.probeSpec.Period / 10 // max jitter is 10% of period
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

func (pw *ProbeWorker) doProbe() {
	pw.RLock()
	defer pw.RUnlock()

	successLabel := prometheus.Labels{probeSuccessfulLabel: "true"}
	notSuccessLabel := prometheus.Labels{probeSuccessfulLabel: "false"}

	client := http.Client{
		Timeout: httpGatewayTimeoutMillis * time.Millisecond,
	}

	strPort := strconv.Itoa(int(pw.probeSpec.Port))
	urlAddress := net.JoinHostPort(pw.localGatewayName, strPort)
	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s%s", urlAddress, pw.probeSpec.Path), nil)
	if err != nil {
		pw.log.Errorf("Could not create a GET request to gateway: %s", err)
		return
	}

	start := time.Now()
	resp, err := client.Do(req)
	end := time.Since(start)
	if err != nil {
		pw.log.Warnf("Problem connecting with gateway. Marking as unhealthy %s", err)
		pw.metrics.alive.Set(0)
		pw.metrics.probes.With(notSuccessLabel).Inc()
		if pw.alive {
			pw.alive = false
			pw.Liveness <- false
		}
		return
	}
	if resp.StatusCode != 200 {
		pw.log.Warnf("Gateway returned unexpected status %d. Marking as unhealthy", resp.StatusCode)
		pw.metrics.alive.Set(0)
		pw.metrics.probes.With(notSuccessLabel).Inc()
		if pw.alive {
			pw.alive = false
			pw.Liveness <- false
		}
	} else {
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

	if err := resp.Body.Close(); err != nil {
		pw.log.Warnf("Failed to close response body %s", err)
	}

}
