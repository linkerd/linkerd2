package util

import (
	"fmt"
	"strconv"
	"strings"

	pb "github.com/runconduit/conduit/controller/gen/common"
)

func AddressToString(addr *pb.TcpAddress) string {
	octects := decodeIPToOctets(addr.GetIp().GetIpv4())
	return fmt.Sprintf("%d.%d.%d.%d:%d", octects[0], octects[1], octects[2], octects[3], addr.GetPort())
}

func AddressesToString(addrs []pb.TcpAddress) string {
	addrStrs := make([]string, len(addrs))
	for i := range addrs {
		addrStrs[i] = AddressToString(&addrs[i])
	}
	return "[" + strings.Join(addrStrs, ",") + "]"
}

func IPToString(ip *pb.IPAddress) string {
	octets := decodeIPToOctets(ip.GetIpv4())
	return fmt.Sprintf("%d.%d.%d.%d", octets[0], octets[1], octets[2], octets[3])
}

func IPV4(a1, a2, a3, a4 uint8) *pb.IPAddress {
	ip := (uint32(a1) << 24) | (uint32(a2) << 16) | (uint32(a3) << 8) | uint32(a4)
	return &pb.IPAddress{
		Ip: &pb.IPAddress_Ipv4{
			Ipv4: ip,
		},
	}
}

func ParseIPV4(ip string) (*pb.IPAddress, error) {
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
	return IPV4(octets[0], octets[1], octets[2], octets[3]), nil
}

func decodeIPToOctets(ip uint32) [4]uint8 {
	return [4]uint8{
		uint8(ip >> 24 & 255),
		uint8(ip >> 16 & 255),
		uint8(ip >> 8 & 255),
		uint8(ip & 255),
	}
}
