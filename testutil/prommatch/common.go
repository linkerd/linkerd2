package prommatch

import "regexp"

var (
	addressRe = regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}:\d+`)
	iPRe      = regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`)
	portRe    = regexp.MustCompile(`\d+`)
)

// TargetAddrLabels match series with proper target_addr, target_port, and target_ip.
func TargetAddrLabels() Labels {
	return Labels{
		"target_addr": Like(addressRe),
		"target_ip":   Like(iPRe),
		"target_port": Like(portRe),
	}
}
