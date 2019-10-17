package util

import (
	"errors"

	"contrib.go.opencensus.io/exporter/ocagent"
	"go.opencensus.io/trace"
)

// InitializeTracing initiates trace, exporter and the sampler
func InitializeTracing(serviceName string, address string, probability float64) error {
	if address != "" {
		oce, err := ocagent.NewExporter(
			ocagent.WithInsecure(),
			ocagent.WithAddress(address),
			ocagent.WithServiceName(serviceName))
		if err != nil {
			return err
		}
		trace.RegisterExporter(oce)
		trace.ApplyConfig(trace.Config{
			DefaultSampler: trace.ProbabilitySampler(probability),
		})
		return nil
	}
	return errors.New("collector address is empty")
}
