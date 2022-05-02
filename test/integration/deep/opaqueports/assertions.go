package opaqueports

import (
	"fmt"
	"regexp"
)

var (
	tcpMetricRE = regexp.MustCompile(
		`tcp_open_total\{direction="inbound",peer="src",target_addr="[0-9\.]+:[0-9]+",target_ip="[0-9\.]+",target_port="[0-9]+",tls="true",client_id="default\.linkerd-opaque-ports[\-a-z]+-test\.serviceaccount\.identity\.linkerd\.cluster\.local",srv_kind="default",srv_name="all-unauthenticated".*} [0-9]+`,
	)
	tcpMetricOutUnmeshedAuthorityRE = regexp.MustCompile(
		`tcp_open_total\{direction="outbound",peer="dst",authority="[a-zA-Z\-]+\.[a-zA-Z\-]+\.svc\.cluster\.local:[0-9]+",target_addr="[0-9\.]+:[0-9]+",target_ip="[0-9\.]+",target_port="[0-9]+",tls="no_identity",no_tls_reason="not_provided_by_service_discovery",.*\} [0-9]+`,
	)
	tcpMetricOutUnmeshedNoAuthorityRE = regexp.MustCompile(
		`tcp_open_total\{direction="outbound",peer="dst",target_addr="[0-9\.]+:[0-9]+",target_ip="[0-9\.]+",target_port="[0-9]+",tls="no_identity",no_tls_reason="not_provided_by_service_discovery",.*\} [0-9]+`,
	)
	httpRequestTotalMetricRE = regexp.MustCompile(
		`request_total\{direction="outbound",authority="[a-zA-Z\-]+\.[a-zA-Z\-]+\.svc\.cluster\.local:8080",target_addr="[0-9\.]+:8080",target_ip="[0-9\.]+",target_port="8080",tls="true",.*`,
	)
	httpRequestTotalUnmeshedRE = regexp.MustCompile(
		`request_total\{direction="outbound",authority="[a-zA-Z\-]+\.[a-zA-Z\-]+\.svc\.cluster\.local:8080",target_addr="[0-9\.]+:8080",target_ip="[0-9\.]+",target_port="8080",tls="no_identity",.*`,
	)
	tcpMetricOutboundMeshedNoAuthorityRE = regexp.MustCompile(
		`tcp_open_total\{direction="outbound",peer="dst",target_addr="[0-9\.]+:8080",target_ip="[0-9\.]+",target_port="8080",tls="true",server_id="default.linkerd-opaque-ports[\-a-z]+\-test.serviceaccount.identity.linkerd.cluster.local",dst_control_plane_ns="linkerd".*`,
	)
	tcpMetricOutboundMeshedAuthorityRE = regexp.MustCompile(
		`tcp_open_total\{direction="outbound",peer="dst",authority="[a-zA-Z\-]+\.linkerd-opaque-ports[\-a-z]+\-test\.svc.cluster\.local:8080",target_addr="[0-9\.]+:8080",target_ip="[0-9\.]+",target_port="8080",tls="true".*`,
	)
)

func hasNoOutbondHTTPRequest(metrics string) error {
	if httpRequestTotalMetricRE.MatchString(metrics) {
		return fmt.Errorf("expected not to find HTTP outbound requests when pod is opaque\n%s", metrics)
	}
	if httpRequestTotalUnmeshedRE.MatchString(metrics) {
		return fmt.Errorf("expected not to find HTTP outbound requests when pod is opaque\n%s", metrics)
	}
	return nil
}

func hasInboundTCPTraffic(metrics string) error {
	if !tcpMetricRE.MatchString(metrics) {
		return fmt.Errorf("failed to find expected metric for inbound TLS TCP traffic\n%s", metrics)
	}
	return nil
}

func hasOutboundTCPWithAuthorityAndNoTLS(metrics string) error {
	if !tcpMetricOutUnmeshedAuthorityRE.MatchString(metrics) {
		return fmt.Errorf("failed to find expected metric for outbound non-TLS TCP traffic\n%s", metrics)
	}
	return nil
}

func hasOutboundTCPWithNoTLSAndNoAuthority(metrics string) error {
	if !tcpMetricOutUnmeshedNoAuthorityRE.MatchString(metrics) {
		return fmt.Errorf("failed to find expected metric for outbound non-TLS TCP traffic\n%s", metrics)
	}
	return nil
}

func hasOutboundTCPWithTLSAndAuthority(metrics string) error {
	if !tcpMetricOutboundMeshedAuthorityRE.MatchString(metrics) {
		return fmt.Errorf("failed to find expected metric for outbound TLS TCP traffic\n%s", metrics)
	}
	return nil
}

func hasOutboundTCPWithTLSAndNoAuthority(metrics string) error {
	if !tcpMetricOutboundMeshedNoAuthorityRE.MatchString(metrics) {
		return fmt.Errorf("failed to find expected metric for outbound TLS TCP traffic\n%s", metrics)
	}
	return nil
}
