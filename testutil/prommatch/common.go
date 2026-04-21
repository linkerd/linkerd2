package prommatch

import "regexp"

var (
	portRe = regexp.MustCompile(`\d+`)
)

// TargetAddrLabels match series with proper target_addr, target_port, and target_ip.
func TargetAddrLabels() Labels {
	return Labels{
		"target_addr": IsAddr(),
		"target_ip":   IsIP(),
		"target_port": Like(portRe),
	}
}
