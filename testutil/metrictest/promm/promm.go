// Package promm provides means of checking whether a prometheus metrics
// contain a specific series.
//
// It tries to give a similar look and feel as time series in PromQL.
// So where in PromQL one would write this:
//
//	request_total{direction="outbound", target_port~="8\d\d\d"} 30
//
// In promm, one can write this:
//
//	portRE := regex.MustCompile(`8\d\d\d`)
//	promm.NewMatcher("request_total", promm.Labels{
//		"direction": promm.Equals("outbound"),
//		"target_port": promm.Like(portRE),
//	})
package promm

import (
	"bytes"
	"fmt"
	"regexp"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// expression can match or reject one time series.
type expression interface {
	matches(sp *model.Sample) bool
}

type funcMatcher func(sp *model.Sample) bool

func (fc funcMatcher) matches(sp *model.Sample) bool {
	return fc(sp)
}

type labelMatcher func(string) bool

func NewMatcher(name string, ms ...expression) *matcher {
	return &matcher{
		expressions: append([]expression{hasName(name)}, ms...),
	}
}

type matcher struct {
	expressions []expression
}

func (e *matcher) HasMatchInString(s string) (bool, error) {
	v, err := extractSamplesVectorFromString(s)
	if err != nil {
		return false, fmt.Errorf("failed to parse input string as vector of samples: %w", err)
	}
	return e.hasMatchInVector(v), nil
}

func (e *matcher) hasMatchInVector(v model.Vector) bool {
	for _, s := range v {
		if e.sampleMatches(s) {
			return true
		}
	}
	return false
}

func (e *matcher) sampleMatches(s *model.Sample) bool {
	for _, m := range e.expressions {
		if !m.matches(s) {
			return false
		}
	}
	return true
}

type Labels map[string]labelMatcher

var _ expression = &Labels{}

func (l Labels) matches(s *model.Sample) bool {
	for k, m := range l {
		labelValue := s.Metric[model.LabelName(k)]
		if !m(string(labelValue)) {
			return false
		}
	}
	return true
}

func Equals(expected string) labelMatcher {
	return func(s string) bool {
		return expected == s
	}
}

func Like(re *regexp.Regexp) labelMatcher {
	return func(s string) bool {
		return re.MatchString(s)
	}
}

func HasValueLike(f func(float64) bool) funcMatcher {
	return func(sp *model.Sample) bool {
		return f(float64(sp.Value))
	}
}

func Absent() labelMatcher {
	return func(s string) bool {
		return s == ""
	}
}

func HasPositiveValue() expression {
	return HasValueLike(func(f float64) bool {
		return f > 0
	})
}

func hasName(metricName string) funcMatcher {
	return func(sp *model.Sample) bool {
		return sp.Metric[model.MetricNameLabel] == model.LabelValue(metricName)
	}
}

func extractSamplesVectorFromString(s string) (model.Vector, error) {
	bb := bytes.NewBufferString(s)

	p := &expfmt.TextParser{}
	metricFamilies, err := p.TextToMetricFamilies(bb)
	if err != nil {
		return nil, fmt.Errorf("failed to parse input as metrics: %w", err)
	}
	var mfs []*dto.MetricFamily
	for _, m := range metricFamilies {
		mfs = append(mfs, m)
	}
	v, err := expfmt.ExtractSamples(&expfmt.DecodeOptions{}, mfs...)
	if err != nil {
		return nil, fmt.Errorf("failed to extract samples from input: %w", err)
	}
	return v, nil
}
