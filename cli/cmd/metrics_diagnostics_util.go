package cmd

import (
	"bytes"
	"crypto/sha256"
	"fmt"

	"github.com/prometheus/common/expfmt"
)

var obfuscationMap = map[string]struct{}{
	"authority":     {},
	"client_id":     {},
	"server_id":     {},
	"target_addr":   {},
	"dst_service":   {},
	"dst_namespace": {},
}

func obfuscateMetrics(metrics []byte) ([]byte, error) {
	reader := bytes.NewReader(metrics)

	var metricsParser expfmt.TextParser

	parsedMetrics, err := metricsParser.TextToMetricFamilies(reader)
	if err != nil {
		return nil, err
	}

	var writer bytes.Buffer
	for _, v := range parsedMetrics {
		for _, m := range v.Metric {
			for _, l := range m.Label {
				if _, ok := obfuscationMap[l.GetName()]; ok {
					obfuscatedValue := obfuscate(l.GetValue())
					l.Value = &obfuscatedValue
				}
			}
		}
		// We'll assume MetricFamilyToText errors are insignificant
		//nolint:errcheck
		expfmt.MetricFamilyToText(&writer, v)
	}

	return writer.Bytes(), nil
}

func obfuscate(s string) string {
	hash := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", hash[:4])
}
