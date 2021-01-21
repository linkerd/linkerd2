package addr

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"

	pb "github.com/linkerd/linkerd2-proxy-api/go/net"
	l5dNetPb "github.com/linkerd/linkerd2/controller/gen/common/net"
)

// DefaultWeight is the default address weight sent by the Destination service
// to the Linkerd proxies.
const DefaultWeight = 1

// PublicAddressToString formats a Viz API TCPAddress as a string.
//
// If Ipv6, the bytes should be ordered big-endian. When formatted as a
// string, the IP address should be enclosed in square brackets followed by
// the port.
func PublicAddressToString(addr *l5dNetPb.TcpAddress) string {
	var s string
	if addr.GetIp().GetIpv6() != nil {
		s = "[%s]:%d"
	} else {
		s = "%s:%d"
	}
	return fmt.Sprintf(s, PublicIPToString(addr.GetIp()), addr.GetPort())
}

// PublicIPToString formats a Viz API IPAddress as a string.
func PublicIPToString(ip *l5dNetPb.IPAddress) string {
	var b []byte
	if ip.GetIpv6() != nil {
		b = make([]byte, 16)
		binary.BigEndian.PutUint64(b[:8], ip.GetIpv6().GetFirst())
		binary.BigEndian.PutUint64(b[8:], ip.GetIpv6().GetLast())
	} else if ip.GetIpv4() != 0 {
		b = make([]byte, 4)
		binary.BigEndian.PutUint32(b, ip.GetIpv4())
	}
	return net.IP(b).String()
}

// ProxyAddressToString formats a Proxy API TCPAddress as a string.
func ProxyAddressToString(addr *pb.TcpAddress) string {
	octects := decodeIPToOctets(addr.GetIp().GetIpv4())
	return fmt.Sprintf("%d.%d.%d.%d:%d", octects[0], octects[1], octects[2], octects[3], addr.GetPort())
}

// ProxyAddressesToString formats a list of Proxy API TCPAddresses as a string.
func ProxyAddressesToString(addrs []pb.TcpAddress) string {
	addrStrs := make([]string, len(addrs))
	for i := range addrs {
		addrStrs[i] = ProxyAddressToString(&addrs[i])
	}
	return "[" + strings.Join(addrStrs, ",") + "]"
}

// ProxyIPToString formats a Proxy API IPAddress as a string.
func ProxyIPToString(ip *pb.IPAddress) string {
	octets := decodeIPToOctets(ip.GetIpv4())
	return fmt.Sprintf("%d.%d.%d.%d", octets[0], octets[1], octets[2], octets[3])
}

// ProxyIPV4 encodes 4 octets as a Proxy API IPAddress.
func ProxyIPV4(a1, a2, a3, a4 uint8) *pb.IPAddress {
	ip := (uint32(a1) << 24) | (uint32(a2) << 16) | (uint32(a3) << 8) | uint32(a4)
	return &pb.IPAddress{
		Ip: &pb.IPAddress_Ipv4{
			Ipv4: ip,
		},
	}
}

// ParseProxyIPV4 parses an IP Address string into a Proxy API IPAddress.
func ParseProxyIPV4(ip string) (*pb.IPAddress, error) {
	segments := strings.Split(ip, ".")
	if len(segments) != 4 {
		return nil, fmt.Errorf("Invalid IP address: %s", ip)
	}
	octets := [4]uint8{0, 0, 0, 0}
	for i, segment := range segments {
		octet, err := strconv.ParseUint(segment, 10, 8)
		if err != nil {
			return nil, fmt.Errorf("Invalid IP segment: %s", segment)
		}
		octets[i] = uint8(octet)
	}
	return ProxyIPV4(octets[0], octets[1], octets[2], octets[3]), nil
}

// PublicIPV4 encodes 4 octets as a Viz API IPAddress.
func PublicIPV4(a1, a2, a3, a4 uint8) *l5dNetPb.IPAddress {
	ip := (uint32(a1) << 24) | (uint32(a2) << 16) | (uint32(a3) << 8) | uint32(a4)
	return &l5dNetPb.IPAddress{
		Ip: &l5dNetPb.IPAddress_Ipv4{
			Ipv4: ip,
		},
	}
}

// ParsePublicIPV4 parses an IP Address string into a Viz API IPAddress.
func ParsePublicIPV4(ip string) (*l5dNetPb.IPAddress, error) {
	segments := strings.Split(ip, ".")
	if len(segments) != 4 {
		return nil, fmt.Errorf("Invalid IP address: %s", ip)
	}
	octets := [4]uint8{0, 0, 0, 0}
	for i, segment := range segments {
		octet, err := strconv.ParseUint(segment, 10, 8)
		if err != nil {
			return nil, fmt.Errorf("Invalid IP segment: %s", segment)
		}
		octets[i] = uint8(octet)
	}
	return PublicIPV4(octets[0], octets[1], octets[2], octets[3]), nil
}

// NetToPublic converts a Proxy API TCPAddress to a Viz API
// TCPAddress
func NetToPublic(net *pb.TcpAddress) *l5dNetPb.TcpAddress {
	var ip *l5dNetPb.IPAddress

	switch i := net.GetIp().GetIp().(type) {
	case *pb.IPAddress_Ipv6:
		ip = &l5dNetPb.IPAddress{
			Ip: &l5dNetPb.IPAddress_Ipv6{
				Ipv6: &l5dNetPb.IPv6{
					First: i.Ipv6.First,
					Last:  i.Ipv6.Last,
				},
			},
		}
	case *pb.IPAddress_Ipv4:
		ip = &l5dNetPb.IPAddress{
			Ip: &l5dNetPb.IPAddress_Ipv4{
				Ipv4: i.Ipv4,
			},
		}
	}

	return &l5dNetPb.TcpAddress{
		Ip:   ip,
		Port: net.GetPort(),
	}
}

func decodeIPToOctets(ip uint32) [4]uint8 {
	return [4]uint8{
		uint8(ip >> 24 & 255),
		uint8(ip >> 16 & 255),
		uint8(ip >> 8 & 255),
		uint8(ip & 255),
	}
}
