// Package prommatch provides means of checking whether a prometheus metrics
// contain a specific series.
//
// It tries to give a similar look and feel as time series in PromQL.
// So where in PromQL one would write this:
//
//	request_total{direction="outbound", target_port=~"8\d\d\d"} 30
//
// In prommatch, one can write this:
//
//	portRE := regex.MustCompile(`^8\d\d\d$`)
//	prommatch.NewMatcher("request_total", prommatch.Labels{
//		"direction": prommatch.Equals("outbound"),
//		"target_port": prommatch.Like(portRE),
//	})
package prommatch

import (
	"bytes"
	"fmt"
	"net"
	"net/netip"
	"regexp"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// Expression can match or reject one time series.
type Expression interface {
	matches(sp *model.Sample) bool
}

type funcMatcher func(sp *model.Sample) bool

func (fc funcMatcher) matches(sp *model.Sample) bool {
	return fc(sp)
}

// NewMatcher will match series name (exactly) and all the additional matchers.
func NewMatcher(name string, ms ...Expression) *Matcher {
	return &Matcher{
		expressions: append([]Expression{hasName(name)}, ms...),
	}
}

// Matcher contains a list of expressions, which will be checked against each series.
type Matcher struct {
	expressions []Expression
}

// HasMatchInString will return:
// - true, if the provided metrics have a series which matches all expressions,
// - false, if none of the series matches,
// - error, if the provided string is not valid Prometheus metrics,
func (e *Matcher) HasMatchInString(s string) (bool, error) {
	v, err := extractSamplesVectorFromString(s)
	if err != nil {
		return false, fmt.Errorf("failed to parse input string as vector of samples: %w", err)
	}
	return e.hasMatchInVector(v), nil
}

func (e *Matcher) hasMatchInVector(v model.Vector) bool {
	for _, s := range v {
		if e.sampleMatches(s) {
			return true
		}
	}
	return false
}

func (e *Matcher) sampleMatches(s *model.Sample) bool {
	for _, m := range e.expressions {
		if !m.matches(s) {
			return false
		}
	}
	return true
}

// LabelMatcher can match or reject a label's value.
type LabelMatcher func(string) bool

// Labels is used for selecting series with matching labels.
type Labels map[string]LabelMatcher

// Make sure Labels implement Expression.
var _ Expression = Labels{}

func (l Labels) matches(s *model.Sample) bool {
	for k, m := range l {
		labelValue := s.Metric[model.LabelName(k)]
		if !m(string(labelValue)) {
			return false
		}
	}
	return true
}

// Equals is when you want label value to have an exact value.
func Equals(expected string) LabelMatcher {
	return func(s string) bool {
		return expected == s
	}
}

// Like is when you want label value to match a regular expression.
func Like(re *regexp.Regexp) LabelMatcher {
	return func(s string) bool {
		return re.MatchString(s)
	}
}

// Absent is when you want to match series MISSING a specific label.
func Absent() LabelMatcher {
	return func(s string) bool {
		return s == ""
	}
}

// Any is when you want to select a series which has a certain label, but don't care about the value.
func Any() LabelMatcher {
	return func(s string) bool {
		return s != ""
	}
}

// HasValueLike is used for selecting time series based on value.
func HasValueLike(f func(float64) bool) Expression {
	return funcMatcher(func(sp *model.Sample) bool {
		return f(float64(sp.Value))
	})
}

// HasValueOf is used for selecting time series based on a specific value.
func HasValueOf(f float64) Expression {
	return funcMatcher(func(sp *model.Sample) bool {
		return f == float64(sp.Value)
	})
}

// HasPositiveValue is used to select time series with a positive value.
func HasPositiveValue() Expression {
	return HasValueLike(func(f float64) bool {
		return f > 0
	})
}

// IsAddr is used to check if the value is an IP:port combo, where IP can be
// an IPv4 or an IPv6
func IsAddr() LabelMatcher {
	return func(s string) bool {
		if _, err := netip.ParseAddrPort(s); err != nil {
			return false
		}
		return true
	}
}

// IsIP use used to check if the value is an IPv4 or IPv6
func IsIP() LabelMatcher {
	return func(s string) bool {
		return net.ParseIP(s) != nil
	}
}

func hasName(metricName string) Expression {
	return funcMatcher(func(sp *model.Sample) bool {
		return sp.Metric[model.MetricNameLabel] == model.LabelValue(metricName)
	})
}

func extractSamplesVectorFromString(s string) (model.Vector, error) {
	bb := bytes.NewBufferString(s)

	p := expfmt.NewTextParser(model.LegacyValidation)
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
