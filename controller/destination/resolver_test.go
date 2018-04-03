package destination

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/runconduit/conduit/controller/util"

	common "github.com/runconduit/conduit/controller/gen/common"
)

func TestEchoIpV4Resolver(t *testing.T) {
	somePort := 666
	thingsThatAreIpV4s := []string{"127.0.0.1", "0.0.0.0", "200.0.12.12"}
	thingsThatAreNotIpV4s := []string{"some.service.name", "example.org", "conduit.io", "1", "-",
		"fe80::a0ca:e86c:898e:52d5%utun0", "::1"}

	t.Run("Says it can resolve only if host is parseable as an IPv4", func(t *testing.T) {
		resolver := &echoIpV4Resolver{}

		for _, ip := range thingsThatAreIpV4s {
			canResolve, err := resolver.canResolve(ip, somePort)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !canResolve {
				t.Fatalf("Expected IP resolver to resolve host [%s], but it couldnt", ip)
			}
		}

		for _, ip := range thingsThatAreNotIpV4s {
			canResolve, err := resolver.canResolve(ip, somePort)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if canResolve {
				t.Fatalf("Expected IPv4 resolver to NOT resolve host [%s], but it could", ip)
			}
		}

	})

	t.Run("Resolves by returning IP and port sent in as parameters until context is cancelled", func(t *testing.T) {
		resolver := &echoIpV4Resolver{}

		for _, expectedIpAdded := range thingsThatAreIpV4s {
			done := make(chan bool, 1)
			listener, cancelFn := newCollectUpdateListener()

			go func() {
				err := resolver.streamResolution(expectedIpAdded, somePort, listener)
				done <- true

				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}

			}()

			cancelFn()
			<-done

			if len(listener.added) != 1 {
				t.Fatalf("Expected added to contain 1 element, got: %v", listener.added)
			}

			actualTcpAddressAdded := listener.added[0]

			expectedIp, err := util.ParseIPV4(expectedIpAdded)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			expectedTcpAddressAdded := common.TcpAddress{
				Ip:   expectedIp,
				Port: uint32(somePort),
			}

			if !reflect.DeepEqual(actualTcpAddressAdded, expectedTcpAddressAdded) {
				t.Fatalf("Expected [%+v] added addresses for IPv4 resolver, got: %+v", expectedTcpAddressAdded, listener)
			}

			if len(listener.removed) != 0 {
				t.Fatalf("Expected no removed addresses for IPv4 resolver, got: %+v", listener)
			}
		}
	})
}

func TestIsIPAddress(t *testing.T) {
	testCases := []struct {
		host   string
		result bool
	}{
		{"8.8.8.8", true},
		{"example.com", false},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: %+v", i, tc.host), func(t *testing.T) {
			isIP, _ := isIPAddress(tc.host)
			if isIP != tc.result {
				t.Fatalf("Unexpected result: %+v", isIP)
			}
		})
	}
}
