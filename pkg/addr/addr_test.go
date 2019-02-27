package addr

import (
	"fmt"
	"testing"

	"github.com/golang/protobuf/proto"
	proxy "github.com/linkerd/linkerd2-proxy-api/go/net"
	"github.com/linkerd/linkerd2/controller/gen/public"
)

func TestNetToPublic(t *testing.T) {

	type addrExp struct {
		proxyAddr     *proxy.TcpAddress
		publicAddress *public.TcpAddress
	}

	expectations := []addrExp{
		{
			proxyAddr:     &proxy.TcpAddress{},
			publicAddress: &public.TcpAddress{},
		},
		{
			proxyAddr: &proxy.TcpAddress{
				Ip:   &proxy.IPAddress{Ip: &proxy.IPAddress_Ipv4{Ipv4: 1}},
				Port: 1234,
			},
			publicAddress: &public.TcpAddress{
				Ip:   &public.IPAddress{Ip: &public.IPAddress_Ipv4{Ipv4: 1}},
				Port: 1234,
			},
		},
		{
			proxyAddr: &proxy.TcpAddress{
				Ip: &proxy.IPAddress{
					Ip: &proxy.IPAddress_Ipv6{
						Ipv6: &proxy.IPv6{
							First: 2345,
							Last:  6789,
						},
					},
				},
				Port: 1234,
			},
			publicAddress: &public.TcpAddress{
				Ip: &public.IPAddress{
					Ip: &public.IPAddress_Ipv6{
						Ipv6: &public.IPv6{
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
		t.Run(fmt.Sprintf("%d returns expected public API TCPAddress", i), func(t *testing.T) {
			res := NetToPublic(exp.proxyAddr)
			if !proto.Equal(res, exp.publicAddress) {
				t.Fatalf("Unexpected TCP Address: [%+v] expected: [%+v]", res, exp.publicAddress)
			}
		})
	}
}
