package util

import (
	"contrib.go.opencensus.io/exporter/ocagent"
	log "github.com/sirupsen/logrus"
	"go.opencensus.io/trace"
)

// InitializeTracing initialises trace, exporter and the sampler
func InitializeTracing(serviceName string, address string, probability float64) {
	if address != "" {
		oce, err := ocagent.NewExporter(
			ocagent.WithInsecure(),
			ocagent.WithAddress(address),
			ocagent.WithServiceName(serviceName))
		if err != nil {
			log.Errorf("Couldn't create a OC Agent exporter:%s", err)
		}
		trace.RegisterExporter(oce)
		trace.ApplyConfig(trace.Config{
			DefaultSampler: trace.ProbabilitySampler(probability),
		})
	}
}
