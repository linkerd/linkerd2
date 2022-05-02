// Package metrictest is used for making assertions on metrics
// produced by linkerd proxy.
package metrictest

import (
	"fmt"
	"regexp"

	"github.com/linkerd/linkerd2/testutil/metrictest/promm"
)

var (
	addrRE      = regexp.MustCompile(`[0-9\.]+:[0-9]+`)
	portRE      = regexp.MustCompile(`[0-9]+`)
	ipRE        = regexp.MustCompile(`[0-9\.]+`)
	authorityRE = regexp.MustCompile(`[a-zA-Z\-]+\.[a-zA-Z\-]+\.svc\.cluster\.local:[0-9]+`)
)

func HasNoOutboundHTTPRequest(metrics, ns string) error {
	m := promm.NewMatcher("request_total", promm.Labels{
		"direction": promm.Equals("outbound"),
	})
	ok, err := m.HasMatchInString(metrics)
	if err != nil {
		return fmt.Errorf("failed to run a check of against the provided metrics: %w", err)
	}
	if ok {
		return fmt.Errorf("expected not to find HTTP outbound requests \n%s", metrics)
	}
	return nil
}

func HasOutboundHTTPRequestWithTLS(metrics, ns string) error {
	m := promm.NewMatcher("request_total", promm.Labels{
		"direction":          promm.Equals("outbound"),
		"target_addr":        promm.Like(addrRE),
		"target_ip":          promm.Like(ipRE),
		"tls":                promm.Equals("true"),
		"server_id":          promm.Equals(fmt.Sprintf("default.%s.serviceaccount.identity.linkerd.cluster.local", ns)),
		"dst_namespace":      promm.Equals(ns),
		"dst_serviceaccount": promm.Equals("default"),
	},
		promm.HasPositiveValue())
	ok, err := m.HasMatchInString(metrics)
	if err != nil {
		return fmt.Errorf("failed to run a check of against the provided metrics: %w", err)
	}
	if !ok {
		return fmt.Errorf("expected to find HTTP outbound requests \n%s", metrics)
	}
	return nil
}

func HasOutboundHTTPRequestNoTLS(metrics, ns string) error {
	m := promm.NewMatcher("request_total", promm.Labels{
		"direction":          promm.Equals("outbound"),
		"target_addr":        promm.Like(addrRE),
		"target_ip":          promm.Like(ipRE),
		"tls":                promm.Equals("no_identity"),
		"no_tls_reason":      promm.Equals("not_provided_by_service_discovery"),
		"dst_namespace":      promm.Equals(ns),
		"dst_serviceaccount": promm.Equals("default"),
	},
		promm.HasPositiveValue())
	ok, err := m.HasMatchInString(metrics)
	if err != nil {
		return fmt.Errorf("failed to run a check of against the provided metrics: %w", err)
	}
	if !ok {
		return fmt.Errorf("expected to find HTTP outbound requests \n%s", metrics)
	}
	return nil
}

func HasInboundSecureTCPTraffic(metrics, ns string) error {
	m := promm.NewMatcher(
		"tcp_open_total",
		promm.Labels{
			"direction":   promm.Equals("inbound"),
			"peer":        promm.Equals("src"),
			"tls":         promm.Equals("true"),
			"client_id":   promm.Equals(fmt.Sprintf("default.%s.serviceaccount.identity.linkerd.cluster.local", ns)),
			"srv_kind":    promm.Equals("default"),
			"srv_name":    promm.Equals("all-unauthenticated"),
			"target_addr": promm.Like(addrRE),
			"target_ip":   promm.Like(ipRE),
		},
		promm.HasPositiveValue(),
	)
	ok, err := m.HasMatchInString(metrics)
	if err != nil {
		return fmt.Errorf("failed to run a check of against the provided metrics: %w", err)
	}
	if !ok {
		return fmt.Errorf("failed to find expected metric for inbound TLS TCP traffic\n%s", metrics)
	}
	return nil
}

func HasOutboundTCPWithAuthorityAndNoTLS(metrics, ns string) error {
	m := promm.NewMatcher("tcp_open_total", promm.Labels{
		"direction":     promm.Equals("outbound"),
		"peer":          promm.Equals("dst"),
		"tls":           promm.Equals("no_identity"),
		"no_tls_reason": promm.Equals("not_provided_by_service_discovery"),
		"authority":     promm.Like(authorityRE),
	},
		promm.HasPositiveValue())
	ok, err := m.HasMatchInString(metrics)
	if err != nil {
		return fmt.Errorf("failed to run a check of against the provided metrics: %w", err)
	}
	if !ok {
		return fmt.Errorf("failed to find expected metric for outbound non-TLS TCP traffic\n%s", metrics)
	}
	return nil
}

func HasOutboundTCPWithNoTLSAndNoAuthority(metrics, ns string) error {
	m := promm.NewMatcher("tcp_open_total", promm.Labels{
		"direction":     promm.Equals("outbound"),
		"peer":          promm.Equals("dst"),
		"tls":           promm.Equals("no_identity"),
		"no_tls_reason": promm.Equals("not_provided_by_service_discovery"),
		"authority":     promm.Absent(),
	})
	ok, err := m.HasMatchInString(metrics)
	if err != nil {
		return fmt.Errorf("failed to run a check of against the provided metrics: %w", err)
	}
	if !ok {
		return fmt.Errorf("failed to find expected metric for outbound non-TLS TCP traffic\n%s", metrics)
	}
	return nil
}

func HasOutboundTCPWithTLSAndAuthority(metrics, ns string) error {
	m := promm.NewMatcher("tcp_open_total", promm.Labels{
		"direction":   promm.Equals("outbound"),
		"peer":        promm.Equals("dst"),
		"tls":         promm.Equals("true"),
		"target_addr": promm.Like(addrRE),
		"authority":   promm.Like(authorityRE),
	},
		promm.HasPositiveValue())
	ok, err := m.HasMatchInString(metrics)
	if err != nil {
		return fmt.Errorf("failed to run a check of against the provided metrics: %w", err)
	}
	if !ok {
		return fmt.Errorf("failed to find expected metric for outbound TLS TCP traffic\n%s", metrics)
	}
	return nil
}

func HasOutboundTCPWithTLSAndNoAuthority(metrics, ns string) error {
	m := promm.NewMatcher("tcp_open_total", promm.Labels{
		"direction":   promm.Equals("outbound"),
		"peer":        promm.Equals("dst"),
		"tls":         promm.Equals("true"),
		"target_addr": promm.Like(addrRE),
		"authority":   promm.Absent(),
	},
		promm.HasPositiveValue())
	ok, err := m.HasMatchInString(metrics)
	if err != nil {
		return fmt.Errorf("failed to run a check of against the provided metrics: %w", err)
	}
	if !ok {
		return fmt.Errorf("failed to find expected metric for outbound TLS TCP traffic\n%s", metrics)
	}
	return nil
}
