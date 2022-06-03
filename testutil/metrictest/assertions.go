// Package metrictest is used for making assertions on metrics
// produced by linkerd proxy.
package metrictest

import (
	"fmt"
	"regexp"

	"github.com/linkerd/linkerd2/testutil/metrictest/prommatch"
)

var (
	addrRE      = regexp.MustCompile(`[0-9\.]+:[0-9]+`)
	ipRE        = regexp.MustCompile(`[0-9\.]+`)
	authorityRE = regexp.MustCompile(`[a-zA-Z\-]+\.[a-zA-Z\-]+\.svc\.cluster\.local:[0-9]+`)
)

// HasNoOutboundHTTPRequest returns error if there is any
// series matching request_total{direction="outbound"}
func HasNoOutboundHTTPRequest(metrics, ns string) error {
	m := prommatch.NewMatcher("request_total", prommatch.Labels{
		"direction": prommatch.Equals("outbound"),
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

// HasOutboundHTTPRequestWithTLS checks there is a series matching:
// request_total{
//   direction="outbound",
//   target_addr=~"[0-9\.]+:[0-9]+",
//   target_ip=~"[0-9.]+",
//   tls="true",
//   dst_namespace="default.${ns}.serviceaccount.identity.linkerd.cluster.local",
//   dst_serviceaccount="default"
// }
func HasOutboundHTTPRequestWithTLS(metrics, ns string) error {
	m := prommatch.NewMatcher("request_total", prommatch.Labels{
		"direction":          prommatch.Equals("outbound"),
		"target_addr":        prommatch.Like(addrRE),
		"target_ip":          prommatch.Like(ipRE),
		"tls":                prommatch.Equals("true"),
		"server_id":          prommatch.Equals(fmt.Sprintf("default.%s.serviceaccount.identity.linkerd.cluster.local", ns)),
		"dst_namespace":      prommatch.Equals(ns),
		"dst_serviceaccount": prommatch.Equals("default"),
	},
		prommatch.HasPositiveValue())
	ok, err := m.HasMatchInString(metrics)
	if err != nil {
		return fmt.Errorf("failed to run a check of against the provided metrics: %w", err)
	}
	if !ok {
		return fmt.Errorf("expected to find HTTP TLS outbound requests \n%s", metrics)
	}
	return nil
}

// HasOutboundHTTPRequestNoTLS checks there is a series matching:
// request_total{
//   direction="outbound",
//   target_addr=~"[0-9\.]+:[0-9]+",
//   target_ip=~"[0-9.]+",
//   tls="no_identity",
//   no_tls_reason="not_provided_by_service_discovery",
//   dst_namespace="default.${ns}.serviceaccount.identity.linkerd.cluster.local",
//   dst_serviceaccount="default"
// }
func HasOutboundHTTPRequestNoTLS(metrics, ns string) error {
	m := prommatch.NewMatcher("request_total", prommatch.Labels{
		"direction":          prommatch.Equals("outbound"),
		"target_addr":        prommatch.Like(addrRE),
		"target_ip":          prommatch.Like(ipRE),
		"tls":                prommatch.Equals("no_identity"),
		"no_tls_reason":      prommatch.Equals("not_provided_by_service_discovery"),
		"dst_namespace":      prommatch.Equals(ns),
		"dst_serviceaccount": prommatch.Equals("default"),
	},
		prommatch.HasPositiveValue())
	ok, err := m.HasMatchInString(metrics)
	if err != nil {
		return fmt.Errorf("failed to run a check of against the provided metrics: %w", err)
	}
	if !ok {
		return fmt.Errorf("expected to find HTTP non-TLS outbound requests\n%s", metrics)
	}
	return nil
}

// HasInboundTCPTrafficWithTLS checks there is a series matching:
// tcp_open_total{
//   direction="inbound",
//   peer="src",
//   tls="true",
//   client_id="default.${ns}.serviceaccount.identity.linkerd.cluster.local",
//   srv_kind="default",
//   srv_name="all-unauthenticated",
//   target_addr=~"[0-9\.]+:[0-9]+",
//   target_ip=~"[0-9\.]+"
// }
func HasInboundTCPTrafficWithTLS(metrics, ns string) error {
	m := prommatch.NewMatcher(
		"tcp_open_total",
		prommatch.Labels{
			"direction":   prommatch.Equals("inbound"),
			"peer":        prommatch.Equals("src"),
			"tls":         prommatch.Equals("true"),
			"client_id":   prommatch.Equals(fmt.Sprintf("default.%s.serviceaccount.identity.linkerd.cluster.local", ns)),
			"srv_kind":    prommatch.Equals("default"),
			"srv_name":    prommatch.Equals("all-unauthenticated"),
			"target_addr": prommatch.Like(addrRE),
			"target_ip":   prommatch.Like(ipRE),
		},
		prommatch.HasPositiveValue(),
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

// HasOutboundTCPWithAuthorityAndNoTLS checks there is a series matching:
// tcp_open_total{
//   direction="outbound",
//   peer="dst",
//   tls="no_identity",
//   no_tls_reason="not_provided_by_service_discovery",
//   authority=~"[a-zA-Z\-]+\.[a-zA-Z\-]+\.svc\.cluster\.local:[0-9]+"
// }
func HasOutboundTCPWithAuthorityAndNoTLS(metrics, ns string) error {
	m := prommatch.NewMatcher("tcp_open_total", prommatch.Labels{
		"direction":     prommatch.Equals("outbound"),
		"peer":          prommatch.Equals("dst"),
		"tls":           prommatch.Equals("no_identity"),
		"no_tls_reason": prommatch.Equals("not_provided_by_service_discovery"),
		"authority":     prommatch.Like(authorityRE),
	},
		prommatch.HasPositiveValue())
	ok, err := m.HasMatchInString(metrics)
	if err != nil {
		return fmt.Errorf("failed to run a check of against the provided metrics: %w", err)
	}
	if !ok {
		return fmt.Errorf("failed to find expected metric for outbound non-TLS TCP traffic\n%s", metrics)
	}
	return nil
}

// HasOutboundTCPWithNoTLSAndNoAuthority checks there is a series matching:
// tcp_open_total{
//   direction="outbound",
//   peer="dst",
//   tls="no_identity",
//   no_tls_reason="not_provided_by_service_discovery",
//   authority=""
// }
func HasOutboundTCPWithNoTLSAndNoAuthority(metrics, ns string) error {
	m := prommatch.NewMatcher("tcp_open_total", prommatch.Labels{
		"direction":     prommatch.Equals("outbound"),
		"peer":          prommatch.Equals("dst"),
		"tls":           prommatch.Equals("no_identity"),
		"no_tls_reason": prommatch.Equals("not_provided_by_service_discovery"),
		"authority":     prommatch.Absent(),
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

// HasOutboundTCPWithTLSAndAuthority checks there is a series matching:
// tcp_open_total{
//   direction="outbound",
//   peer="dst",
//   tls="true",
//   target_addr=~"[0-9\.]+:[0-9]+",
//   authority=~"[a-zA-Z\-]+\.[a-zA-Z\-]+\.svc\.cluster\.local:[0-9]+"
// }
func HasOutboundTCPWithTLSAndAuthority(metrics, ns string) error {
	m := prommatch.NewMatcher("tcp_open_total", prommatch.Labels{
		"direction":   prommatch.Equals("outbound"),
		"peer":        prommatch.Equals("dst"),
		"tls":         prommatch.Equals("true"),
		"target_addr": prommatch.Like(addrRE),
		"authority":   prommatch.Like(authorityRE),
	},
		prommatch.HasPositiveValue())
	ok, err := m.HasMatchInString(metrics)
	if err != nil {
		return fmt.Errorf("failed to run a check against the provided metrics: %w", err)
	}
	if !ok {
		return fmt.Errorf("failed to find expected metric for outbound TLS TCP traffic\n%s", metrics)
	}
	return nil
}

// HasOutboundTCPWithTLSAndNoAuthority checks there is a series matching:
// tcp_open_total{
//   direction="outbound",
//   peer="dst",
//   tls="true",
//   target_addr=~"[0-9\.]+:[0-9]+",
//   authority=""
// }
func HasOutboundTCPWithTLSAndNoAuthority(metrics, ns string) error {
	m := prommatch.NewMatcher("tcp_open_total", prommatch.Labels{
		"direction":   prommatch.Equals("outbound"),
		"peer":        prommatch.Equals("dst"),
		"tls":         prommatch.Equals("true"),
		"target_addr": prommatch.Like(addrRE),
		"authority":   prommatch.Absent(),
	},
		prommatch.HasPositiveValue())
	ok, err := m.HasMatchInString(metrics)
	if err != nil {
		return fmt.Errorf("failed to run a check of against the provided metrics: %w", err)
	}
	if !ok {
		return fmt.Errorf("failed to find expected metric for outbound TLS TCP traffic\n%s", metrics)
	}
	return nil
}
