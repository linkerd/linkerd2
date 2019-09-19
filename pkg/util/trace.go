package util

import (
	"log"

	"contrib.go.opencensus.io/exporter/ocagent"
	"go.opencensus.io/trace"
)

// InitialiseTracing initialises trace, exporter and the sampler
func InitialiseTracing(serviceName string, address string, probability float64) {
	oce, err := ocagent.NewExporter(
		ocagent.WithInsecure(),
		ocagent.WithAddress(address),
		ocagent.WithServiceName(serviceName))
	if err != nil {
		log.Fatalf("Failed to create ocagent-exporter: %v", err)
	}
	trace.RegisterExporter(oce)
	trace.ApplyConfig(trace.Config{
		DefaultSampler: trace.ProbabilitySampler(probability),
	})
}
