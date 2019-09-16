package util

import (
	"log"
	"os"

	"contrib.go.opencensus.io/exporter/ocagent"
	"go.opencensus.io/trace"
)

var (
	ocagentHost = os.Getenv("OC_AGENT_HOST")
)

func SetExporter(serviceName string) {
	oce, err := ocagent.NewExporter(
		ocagent.WithInsecure(),
		ocagent.WithAddress(ocagentHost),
		ocagent.WithServiceName(serviceName))
	if err != nil {
		log.Fatalf("Failed to create ocagent-exporter: %v", err)
	}
	trace.RegisterExporter(oce)
	trace.ApplyConfig(trace.Config{
		DefaultSampler: trace.AlwaysSample(),
	})
}
