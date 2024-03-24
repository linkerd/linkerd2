package addr

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"strconv"

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
	strIP := PublicIPToString(addr.GetIp())
	strPort := strconv.Itoa(int(addr.GetPort()))
	return net.JoinHostPort(strIP, strPort)
}

// PublicIPToString formats a Viz API IPAddress as a string.
func PublicIPToString(ip *l5dNetPb.IPAddress) string {
	var netIP net.IP
	if ip.GetIpv6() != nil {
		b := make([]byte, net.IPv6len)
		binary.BigEndian.PutUint64(b[:8], ip.GetIpv6().GetFirst())
		binary.BigEndian.PutUint64(b[8:], ip.GetIpv6().GetLast())
		netIP = net.IP(b)
	} else if ip.GetIpv4() != 0 {
		netIP = decodeIPv4ToNetIP(ip.GetIpv4())
	}
	if netIP == nil {
		return ""
	}
	return netIP.String()
}

// ProxyAddressToString formats a Proxy API TCPAddress as a string.
func ProxyAddressToString(addr *pb.TcpAddress) string {
	vizIP := proxyToVizIPAddr(addr.GetIp())
	if vizIP == nil {
		return ""
	}
	strIP := PublicIPToString(vizIP)
	strPort := strconv.Itoa(int(addr.GetPort()))
	return net.JoinHostPort(strIP, strPort)
}

// ParseProxyIP parses an IP Address string into a Proxy API IPAddress.
func ParseProxyIP(ip string) (*pb.IPAddress, error) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return nil, fmt.Errorf("invalid IP address: %s", ip)
	}

	if addr.Is4() {
		ipBytes := addr.As4()
		return &pb.IPAddress{
			Ip: &pb.IPAddress_Ipv4{
				Ipv4: binary.BigEndian.Uint32(ipBytes[:]),
			},
		}, nil
	} else if addr.Is6() {
		ipBytes := addr.As16()
		return &pb.IPAddress{
			Ip: &pb.IPAddress_Ipv6{
				Ipv6: &pb.IPv6{
					First: binary.BigEndian.Uint64(ipBytes[:8]),
					Last:  binary.BigEndian.Uint64(ipBytes[8:]),
				},
			},
		}, nil
	}

	return nil, fmt.Errorf("invalid IP address: %s", ip)
}

// ParsePublicIP parses an IP Address string into a Viz API IPAddress.
func ParsePublicIP(ip string) (*l5dNetPb.IPAddress, error) {
	addr, err := ParseProxyIP(ip)
	if err != nil {
		return nil, err
	}
	return proxyToVizIPAddr(addr), nil
}

// NetToPublic converts a Proxy API TCPAddress to a Viz API
// TCPAddress.
func NetToPublic(net *pb.TcpAddress) *l5dNetPb.TcpAddress {
	ip := proxyToVizIPAddr(net.GetIp())

	return &l5dNetPb.TcpAddress{
		Ip:   ip,
		Port: net.GetPort(),
	}
}

func proxyToVizIPAddr(net *pb.IPAddress) *l5dNetPb.IPAddress {
	switch ip := net.GetIp().(type) {
	case *pb.IPAddress_Ipv6:
		return &l5dNetPb.IPAddress{
			Ip: &l5dNetPb.IPAddress_Ipv6{
				Ipv6: &l5dNetPb.IPv6{
					First: ip.Ipv6.First,
					Last:  ip.Ipv6.Last,
				},
			},
		}
	case *pb.IPAddress_Ipv4:
		return &l5dNetPb.IPAddress{
			Ip: &l5dNetPb.IPAddress_Ipv4{
				Ipv4: ip.Ipv4,
			},
		}
	}

	return nil
}

// decodeIPv4ToNetIP converts IPv4 uint32 to an IPv4 net IP.
func decodeIPv4ToNetIP(ip uint32) net.IP {
	oBigInt := big.NewInt(0)
	oBigInt = oBigInt.SetUint64(uint64(ip))
	return IntToIPv4(oBigInt)
}

// IPToInt converts net.IP to bigInt
// It can support both IPv4 and IPv6.
func IPToInt(ip net.IP) *big.Int {
	oBigInt := big.NewInt(0)
	oBigInt.SetBytes(ip)
	return oBigInt
}

// IntToIPv4 converts IPv4 bigInt into an IPv4 net IP.
func IntToIPv4(intip *big.Int) net.IP {
	ipByte := make([]byte, net.IPv4len)
	uint32IP := intip.Uint64()
	binary.BigEndian.PutUint32(ipByte, uint32(uint32IP))
	return net.IP(ipByte)
}
