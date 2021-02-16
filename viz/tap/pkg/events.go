package pkg

import (
	"encoding/binary"

	netPb "github.com/linkerd/linkerd2/controller/gen/common/net"
	tapPb "github.com/linkerd/linkerd2/viz/tap/gen/tap"
)

// CreateTapEvent generates tap events for use in tests
func CreateTapEvent(eventHTTP *tapPb.TapEvent_Http, dstMeta map[string]string, proxyDirection tapPb.TapEvent_ProxyDirection) *tapPb.TapEvent {
	event := &tapPb.TapEvent{
		ProxyDirection: proxyDirection,
		Source: &netPb.TcpAddress{
			Ip: &netPb.IPAddress{
				Ip: &netPb.IPAddress_Ipv4{
					Ipv4: uint32(1),
				},
			},
		},
		Destination: &netPb.TcpAddress{
			Ip: &netPb.IPAddress{
				Ip: &netPb.IPAddress_Ipv6{
					Ipv6: &netPb.IPv6{
						// All nodes address: https://www.iana.org/assignments/ipv6-multicast-addresses/ipv6-multicast-addresses.xhtml
						First: binary.BigEndian.Uint64([]byte{0xff, 0x01, 0, 0, 0, 0, 0, 0}),
						Last:  binary.BigEndian.Uint64([]byte{0, 0, 0, 0, 0, 0, 0, 0x01}),
					},
				},
			},
		},
		Event: &tapPb.TapEvent_Http_{
			Http: eventHTTP,
		},
		DestinationMeta: &tapPb.TapEvent_EndpointMeta{
			Labels: dstMeta,
		},
	}
	return event
}
