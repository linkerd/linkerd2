package trace

import (
	"contrib.go.opencensus.io/exporter/ocagent"
	"go.opencensus.io/trace"
)

// InitializeTracing initiates trace, exporter and the sampler
func InitializeTracing(serviceName string, address string) error {
	oce, err := ocagent.NewExporter(
		ocagent.WithInsecure(),
		ocagent.WithAddress(address),
		ocagent.WithServiceName(serviceName))
	if err != nil {
		return err
	}
	trace.RegisterExporter(oce)
	trace.ApplyConfig(trace.Config{
		DefaultSampler: trace.AlwaysSample(),
	})
	return nil
}
