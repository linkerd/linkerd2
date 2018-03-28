package destination

import (
	"fmt"

	common "github.com/runconduit/conduit/controller/gen/common"
	"github.com/runconduit/conduit/controller/util"
)

type echoIpResolver struct{}

func (i *echoIpResolver) canResolve(host string, port int) (bool, error) {
	isIP, _ := isIPAddress(host)
	return isIP, nil
}

func (i *echoIpResolver) streamResolution(host string, port int, listener updateListener) error {
	isIP, ip := isIPAddress(host)
	if !isIP {
		return fmt.Errorf("host [%s] isn'' an IP address", host)
	}

	address := common.TcpAddress{
		Ip:   ip,
		Port: uint32(port),
	}

	listener.Update([]common.TcpAddress{address}, []common.TcpAddress{})

	<-listener.Done()
	return nil
}

func isIPAddress(host string) (bool, *common.IPAddress) {
	ip, err := util.ParseIPV4(host)
	return err == nil, ip
}
