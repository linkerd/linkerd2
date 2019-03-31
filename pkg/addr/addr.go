package addr

import (
	"fmt"
	"strconv"
	"strings"

	pb "github.com/linkerd/linkerd2-proxy-api/go/net"
	"github.com/linkerd/linkerd2/controller/gen/public"
)

// PublicAddressToString formats a Public API TCPAddress as a string.
func PublicAddressToString(addr *public.TcpAddress) string {
	octects := decodeIPToOctets(addr.GetIp().GetIpv4())
	return fmt.Sprintf("%d.%d.%d.%d:%d", octects[0], octects[1], octects[2], octects[3], addr.GetPort())
}

// PublicIPToString formats a Public API IPAddress as a string.
func PublicIPToString(ip *public.IPAddress) string {
	octets := decodeIPToOctets(ip.GetIpv4())
	return fmt.Sprintf("%d.%d.%d.%d", octets[0], octets[1], octets[2], octets[3])
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

// PublicIPV4 encodes 4 octets as a Public API IPAddress.
func PublicIPV4(a1, a2, a3, a4 uint8) *public.IPAddress {
	ip := (uint32(a1) << 24) | (uint32(a2) << 16) | (uint32(a3) << 8) | uint32(a4)
	return &public.IPAddress{
		Ip: &public.IPAddress_Ipv4{
			Ipv4: ip,
		},
	}
}

// ParsePublicIPV4 parses an IP Address string into a Public API IPAddress.
func ParsePublicIPV4(ip string) (*public.IPAddress, error) {
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

// NetToPublic converts a Proxy API TCPAddress to a Public API
// TCPAddress
func NetToPublic(net *pb.TcpAddress) *public.TcpAddress {
	var ip *public.IPAddress

	switch i := net.GetIp().GetIp().(type) {
	case *pb.IPAddress_Ipv6:
		ip = &public.IPAddress{
			Ip: &public.IPAddress_Ipv6{
				Ipv6: &public.IPv6{
					First: i.Ipv6.First,
					Last:  i.Ipv6.Last,
				},
			},
		}
	case *pb.IPAddress_Ipv4:
		ip = &public.IPAddress{
			Ip: &public.IPAddress_Ipv4{
				Ipv4: i.Ipv4,
			},
		}
	}

	return &public.TcpAddress{
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
