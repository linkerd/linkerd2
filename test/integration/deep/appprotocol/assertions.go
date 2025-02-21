package appprotocol

import (
	"fmt"
	"regexp"

	"github.com/linkerd/linkerd2/testutil/prommatch"
)

var (
	authorityRE = regexp.MustCompile(`[a-zA-Z\-]+\.[a-zA-Z\-]+\.svc\.cluster\.local:[0-9]+`)
)

// hasNoOutboundHTTPRequest returns error if there is any
// series matching request_total{direction="outbound"}
func hasNoOutboundHTTPRequest(metrics, ns string) error {
	m := prommatch.NewMatcher("request_total",
		prommatch.Labels{
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

// hasOutboundHTTPRequestWithTLS checks there is a series matching:
//
//	request_total{
//	  direction="outbound",
//	  target_addr=~"[0-9\.]+:[0-9]+",
//	  target_ip=~"[0-9.]+",
//	  tls="true",
//	  dst_namespace="default.${ns}.serviceaccount.identity.linkerd.cluster.local",
//	  dst_serviceaccount="default"
//	}
func hasOutboundHTTPRequestWithTLS(metrics, ns string) error {
	m := prommatch.NewMatcher("request_total",
		prommatch.Labels{
			"direction":          prommatch.Equals("outbound"),
			"tls":                prommatch.Equals("true"),
			"server_id":          prommatch.Equals(fmt.Sprintf("default.%s.serviceaccount.identity.linkerd.cluster.local", ns)),
			"dst_namespace":      prommatch.Equals(ns),
			"dst_serviceaccount": prommatch.Equals("default"),
		},
		prommatch.TargetAddrLabels(),
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

// hasInboundTCPTrafficWithTLS checks there is a series matching:
//
//	tcp_open_total{
//	  direction="inbound",
//	  peer="src",
//	  tls="true",
//	  client_id="default.${ns}.serviceaccount.identity.linkerd.cluster.local",
//	  srv_kind="default",
//	  srv_name="all-unauthenticated",
//	  target_addr=~"[0-9\.]+:[0-9]+",
//	  target_ip=~"[0-9\.]+"
//	}
func hasInboundTCPTrafficWithTLS(metrics, ns string) error {
	m := prommatch.NewMatcher(
		"tcp_open_total",
		prommatch.Labels{
			"direction": prommatch.Equals("inbound"),
			"peer":      prommatch.Equals("src"),
			"tls":       prommatch.Equals("true"),
			"client_id": prommatch.Equals(fmt.Sprintf("default.%s.serviceaccount.identity.linkerd.cluster.local", ns)),
			"srv_kind":  prommatch.Equals("default"),
			"srv_name":  prommatch.Equals("all-unauthenticated"),
		},
		prommatch.TargetAddrLabels(),
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

// hasOutboundTCPWithTLSAndAuthority checks there is a series matching:
//
//	tcp_open_total{
//	  direction="outbound",
//	  peer="dst",
//	  tls="true",
//	  target_addr=~"[0-9\.]+:[0-9]+",
//	  authority=~"[a-zA-Z\-]+\.[a-zA-Z\-]+\.svc\.cluster\.local:[0-9]+"
//	}
func hasOutboundTCPWithTLSAndAuthority(metrics, ns string) error {
	m := prommatch.NewMatcher("tcp_open_total",
		prommatch.Labels{
			"direction": prommatch.Equals("outbound"),
			"peer":      prommatch.Equals("dst"),
			"tls":       prommatch.Equals("true"),
			"authority": prommatch.Like(authorityRE),
		},
		prommatch.TargetAddrLabels(),
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

// hasOutboundTCPWithTLSAndNoAuthority checks there is a series matching:
//
//	tcp_open_total{
//	  direction="outbound",
//	  peer="dst",
//	  tls="true",
//	  target_addr=~"[0-9\.]+:[0-9]+",
//	  authority=""
//	}
func hasOutboundTCPWithTLSAndNoAuthority(metrics, ns string) error {
	m := prommatch.NewMatcher("tcp_open_total",
		prommatch.Labels{
			"direction": prommatch.Equals("outbound"),
			"peer":      prommatch.Equals("dst"),
			"tls":       prommatch.Equals("true"),
			"authority": prommatch.Absent(),
		},
		prommatch.TargetAddrLabels(),
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
