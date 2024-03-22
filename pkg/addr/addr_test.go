package addr

import (
	"fmt"
	"testing"

	pb "github.com/linkerd/linkerd2-proxy-api/go/net"
	l5dNetPb "github.com/linkerd/linkerd2/controller/gen/common/net"
	"google.golang.org/protobuf/proto"
)

func TestPublicAddressToString(t *testing.T) {
	cases := []struct {
		name     string
		addr     *l5dNetPb.TcpAddress
		expected string
	}{
		{
			name: "ipv4",
			addr: &l5dNetPb.TcpAddress{
				Ip: &l5dNetPb.IPAddress{
					Ip: &l5dNetPb.IPAddress_Ipv4{
						Ipv4: 3232235521,
					},
				},
				Port: 1234,
			},
			expected: "192.168.0.1:1234",
		},
		{
			name: "ipv6",
			addr: &l5dNetPb.TcpAddress{
				Ip: &l5dNetPb.IPAddress{
					Ip: &l5dNetPb.IPAddress_Ipv6{
						Ipv6: &l5dNetPb.IPv6{
							First: 49320,
							Last:  1,
						},
					},
				},
				Port: 1234,
			},
			expected: "[::c0a8:0:0:0:1]:1234",
		},
		{
			name:     "nil",
			addr:     nil,
			expected: ":0",
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := PublicAddressToString(c.addr)
			if c.expected != got {
				t.Errorf("expected: %v, got: %v", c.expected, got)
			}
		})
	}
}

func TestPublicIPToString(t *testing.T) {
	cases := []struct {
		name     string
		addr     *l5dNetPb.IPAddress
		expected string
	}{
		{
			name: "ipv4",
			addr: &l5dNetPb.IPAddress{
				Ip: &l5dNetPb.IPAddress_Ipv4{
					Ipv4: 3232235521,
				},
			},
			expected: "192.168.0.1",
		},
		{
			name: "normal ipv6",
			addr: &l5dNetPb.IPAddress{
				Ip: &l5dNetPb.IPAddress_Ipv6{
					Ipv6: &l5dNetPb.IPv6{
						First: 2306139570357600256,
						Last:  151930230829876,
					},
				},
			},
			expected: "2001:db8:85a3::8a2e:370:7334",
		},
		{
			name: "ipv6 with zero as prefix",
			addr: &l5dNetPb.IPAddress{
				Ip: &l5dNetPb.IPAddress_Ipv6{
					Ipv6: &l5dNetPb.IPv6{
						First: 49320,
						Last:  1,
					},
				},
			},
			expected: "::c0a8:0:0:0:1",
		},
		{
			name:     "nil",
			addr:     nil,
			expected: "",
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := PublicIPToString(c.addr)
			if c.expected != got {
				t.Errorf("expected: %v, got: %v", c.expected, got)
			}
		})
	}
}

func TestNetToPublic(t *testing.T) {

	type addrExp struct {
		proxyAddr     *pb.TcpAddress
		publicAddress *l5dNetPb.TcpAddress
	}

	expectations := []addrExp{
		{
			proxyAddr:     &pb.TcpAddress{},
			publicAddress: &l5dNetPb.TcpAddress{},
		},
		{
			proxyAddr: &pb.TcpAddress{
				Ip:   &pb.IPAddress{Ip: &pb.IPAddress_Ipv4{Ipv4: 1}},
				Port: 1234,
			},
			publicAddress: &l5dNetPb.TcpAddress{
				Ip:   &l5dNetPb.IPAddress{Ip: &l5dNetPb.IPAddress_Ipv4{Ipv4: 1}},
				Port: 1234,
			},
		},
		{
			proxyAddr: &pb.TcpAddress{
				Ip: &pb.IPAddress{
					Ip: &pb.IPAddress_Ipv6{
						Ipv6: &pb.IPv6{
							First: 2345,
							Last:  6789,
						},
					},
				},
				Port: 1234,
			},
			publicAddress: &l5dNetPb.TcpAddress{
				Ip: &l5dNetPb.IPAddress{
					Ip: &l5dNetPb.IPAddress_Ipv6{
						Ipv6: &l5dNetPb.IPv6{
							First: 2345,
							Last:  6789,
						},
					},
				},
				Port: 1234,
			},
		},
	}

	for i, exp := range expectations {
		exp := exp // pin
		t.Run(fmt.Sprintf("%d returns expected Viz API TCPAddress", i), func(t *testing.T) {
			res := NetToPublic(exp.proxyAddr)
			if !proto.Equal(res, exp.publicAddress) {
				t.Fatalf("Unexpected TCP Address: [%+v] expected: [%+v]", res, exp.publicAddress)
			}
		})
	}
}

func TestParseProxyIP(t *testing.T) {
	var testCases = []struct {
		ip      string
		expAddr *pb.IPAddress
		expErr  bool
	}{
		{
			ip:      "10.0",
			expAddr: nil,
			expErr:  true,
		},
		{
			ip:      "x.x.x.x",
			expAddr: nil,
			expErr:  true,
		},
		{
			ip: "10.10.10.10",
			expAddr: &pb.IPAddress{
				Ip: &pb.IPAddress_Ipv4{Ipv4: 168430090},
			},
			expErr: false,
		},
		{
			ip: "2001:db8:85a3::8a2e:370:7334",
			expAddr: &pb.IPAddress{
				Ip: &pb.IPAddress_Ipv6{
					Ipv6: &pb.IPv6{
						First: 2306139570357600256,
						Last:  151930230829876,
					},
				},
			},
			expErr: false,
		},
	}

	for _, testCase := range testCases {
		res, err := ParseProxyIP(testCase.ip)
		if testCase.expErr && err == nil {
			t.Fatalf("expected get err, but get nil")
		}
		if !testCase.expErr {
			if err != nil {
				t.Fatalf("Unexpected err %v", err)
			}
			if !proto.Equal(res, testCase.expAddr) {
				t.Fatalf("Unexpected TCP Address: [%+v] expected: [%+v]", res, testCase.expAddr)
			}
		}
	}
}

func TestParsePublicIP(t *testing.T) {
	var testCases = []struct {
		ip      string
		expAddr *l5dNetPb.IPAddress
		expErr  bool
	}{
		{
			ip:      "10.0",
			expAddr: nil,
			expErr:  true,
		},
		{
			ip:      "x.x.x.x",
			expAddr: nil,
			expErr:  true,
		},
		{
			ip: "10.10.10.11",
			expAddr: &l5dNetPb.IPAddress{
				Ip: &l5dNetPb.IPAddress_Ipv4{Ipv4: 168430091},
			},
			expErr: false,
		},
		{
			ip: "2001:db8:85a3::8a2e:370:7334",
			expAddr: &l5dNetPb.IPAddress{
				Ip: &l5dNetPb.IPAddress_Ipv6{
					Ipv6: &l5dNetPb.IPv6{
						First: 2306139570357600256,
						Last:  151930230829876,
					},
				},
			},
			expErr: false,
		},
	}

	for _, testCase := range testCases {
		res, err := ParsePublicIP(testCase.ip)
		if testCase.expErr && err == nil {
			t.Fatalf("expected get err, but get nil")
		}
		if !testCase.expErr {
			if err != nil {
				t.Fatalf("Unexpected err %v", err)
			}
			if !proto.Equal(res, testCase.expAddr) {
				t.Fatalf("Unexpected TCP Address: [%+v] expected: [%+v]", res, testCase.expAddr)
			}
		}
	}
}

func TestProxyAddressToString(t *testing.T) {
	var testCases = []struct {
		addr   *pb.TcpAddress
		expStr string
	}{
		{
			addr: &pb.TcpAddress{
				Ip:   &pb.IPAddress{Ip: &pb.IPAddress_Ipv4{Ipv4: 1}},
				Port: 1234,
			},
			expStr: "0.0.0.1:1234",
		},
		{
			addr: &pb.TcpAddress{
				Ip:   &pb.IPAddress{Ip: &pb.IPAddress_Ipv4{Ipv4: 65535}},
				Port: 5678,
			},
			expStr: "0.0.255.255:5678",
		},
		{
			addr: &pb.TcpAddress{
				Ip: &pb.IPAddress{
					Ip: &pb.IPAddress_Ipv6{
						Ipv6: &pb.IPv6{
							First: 2306139570357600256,
							Last:  151930230829876,
						},
					},
				},
				Port: 5678,
			},
			expStr: "[2001:db8:85a3::8a2e:370:7334]:5678",
		},
	}

	for _, testCase := range testCases {
		res := ProxyAddressToString(testCase.addr)
		if !(res == testCase.expStr) {
			t.Fatalf("Unexpected string: %s expected: %s", res, testCase.expStr)
		}
	}
}
