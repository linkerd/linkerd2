package cmd

import (
	"testing"

	"github.com/golang/protobuf/ptypes/duration"
	netPb "github.com/linkerd/linkerd2/controller/gen/common/net"
	tapPb "github.com/linkerd/linkerd2/viz/tap/gen/tap"
)

func TestStripPort(t *testing.T) {
	for _, tc := range []struct {
		address  string
		expected string
	}{
		{"10.1.2.3:8080", "10.1.2.3"},
		{"[2001:db8::1]:8080", "2001:db8::1"},
		{"[::1]:80", "::1"},
		{"10.1.2.3", "10.1.2.3"},
		{":80", ""},
		{"", ""},
	} {
		if actual := stripPort(tc.address); actual != tc.expected {
			t.Errorf("stripPort(%q): expected %q, got %q", tc.address, tc.expected, actual)
		}
	}
}

func TestNewRowIPAddresses(t *testing.T) {
	ipv4 := func(ip uint32, port uint32) *netPb.TcpAddress {
		return &netPb.TcpAddress{
			Ip:   &netPb.IPAddress{Ip: &netPb.IPAddress_Ipv4{Ipv4: ip}},
			Port: port,
		}
	}
	// 2001:db8::<last>
	ipv6 := func(last uint64, port uint32) *netPb.TcpAddress {
		return &netPb.TcpAddress{
			Ip: &netPb.IPAddress{
				Ip: &netPb.IPAddress_Ipv6{
					Ipv6: &netPb.IPv6{First: 0x20010db800000000, Last: last},
				},
			},
			Port: port,
		}
	}

	for _, tc := range []struct {
		name           string
		source         *netPb.TcpAddress
		destination    *netPb.TcpAddress
		sourcePod      string
		destinationPod string
		expectedSource string
		expectedDest   string
	}{
		{
			name:           "ipv4 addresses",
			source:         ipv4(0x0a000001, 40000),
			destination:    ipv4(0x0a000002, 8080),
			expectedSource: "10.0.0.1",
			expectedDest:   "10.0.0.2",
		},
		{
			name:           "ipv6 addresses",
			source:         ipv6(1, 40000),
			destination:    ipv6(2, 8080),
			expectedSource: "2001:db8::1",
			expectedDest:   "2001:db8::2",
		},
		{
			name:           "pod labels take precedence over ipv6 addresses",
			source:         ipv6(1, 40000),
			destination:    ipv6(2, 8080),
			sourcePod:      "web-0",
			destinationPod: "api-0",
			expectedSource: "web-0",
			expectedDest:   "api-0",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sourceLabels := map[string]string{}
			if tc.sourcePod != "" {
				sourceLabels["pod"] = tc.sourcePod
			}
			destLabels := map[string]string{}
			if tc.destinationPod != "" {
				destLabels["pod"] = tc.destinationPod
			}

			row, err := newRow(topRequest{
				event: &tapPb.TapEvent{
					Source:          tc.source,
					Destination:     tc.destination,
					SourceMeta:      &tapPb.TapEvent_EndpointMeta{Labels: sourceLabels},
					DestinationMeta: &tapPb.TapEvent_EndpointMeta{Labels: destLabels},
				},
				reqInit: &tapPb.TapEvent_Http_RequestInit{Path: "/"},
				rspInit: &tapPb.TapEvent_Http_ResponseInit{HttpStatus: 200},
				rspEnd: &tapPb.TapEvent_Http_ResponseEnd{
					SinceRequestInit: &duration.Duration{Seconds: 1},
				},
			})
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if row.source != tc.expectedSource {
				t.Errorf("expected source %q, got %q", tc.expectedSource, row.source)
			}
			if row.destination != tc.expectedDest {
				t.Errorf("expected destination %q, got %q", tc.expectedDest, row.destination)
			}
		})
	}
}

func TestTopTableKeepsIPv6PeersApart(t *testing.T) {
	// Two clients in the same IPv6 prefix calling the same destination must
	// end up in two rows.
	request := func(client uint64) topRequest {
		return topRequest{
			event: &tapPb.TapEvent{
				Source: &netPb.TcpAddress{
					Ip: &netPb.IPAddress{
						Ip: &netPb.IPAddress_Ipv6{Ipv6: &netPb.IPv6{First: 0x20010db800000000, Last: client}},
					},
					Port: 40000,
				},
				Destination: &netPb.TcpAddress{
					Ip: &netPb.IPAddress{
						Ip: &netPb.IPAddress_Ipv6{Ipv6: &netPb.IPv6{First: 0x20010db800000000, Last: 100}},
					},
					Port: 8080,
				},
				SourceMeta:      &tapPb.TapEvent_EndpointMeta{Labels: map[string]string{}},
				DestinationMeta: &tapPb.TapEvent_EndpointMeta{Labels: map[string]string{}},
			},
			reqInit: &tapPb.TapEvent_Http_RequestInit{Path: "/"},
			rspInit: &tapPb.TapEvent_Http_ResponseInit{HttpStatus: 200},
			rspEnd: &tapPb.TapEvent_Http_ResponseEnd{
				SinceRequestInit: &duration.Duration{Seconds: 1},
			},
		}
	}

	table := newTopTable()
	table.insert(request(1))
	table.insert(request(2))

	if len(table.rows) != 2 {
		t.Fatalf("expected 2 rows, got %d: %+v", len(table.rows), table.rows)
	}
}
