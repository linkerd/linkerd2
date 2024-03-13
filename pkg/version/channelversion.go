package version

import (
	"fmt"
	"strconv"
	"strings"
)

// channelVersion is a low-level struct for handling release channels in a
// structured way. It has no dependencies on the rest of the version package.
type channelVersion struct {
	channel string
	version string
}

// hotpatchSuffix is the suffix applied to channel names to indicate that the
// version string includes a hotpatch number (e.g. dev-0.1.2-3)
const hotpatchSuffix = "Hotpatch"

// String returns a string representation of a channelVersion, for example:
// { "channel": "version"} => "channel-version"
func (cv channelVersion) String() string {
	return fmt.Sprintf("%s-%s", strings.TrimSuffix(cv.channel, hotpatchSuffix), cv.version)
}

// parseChannelVersion parses a build string into a channelVersion struct. it
// expects the channel and version to be separated by a hyphen (e.g. dev-0.1.2).
// the version may additionally include a hotpatch number, which is separated
// from the base version by a hyphen (e.g. dev-0.1.2-3). if the hotpatch number
// is present, then the channel name is suffixed with "Hotpatch" to indicate
// that a separate channel should be used when checking if the version is
// up-to-date. if the version is suffixed with any other non-numeric build info
// (e.g. dev-0.1.2-foo), that info is ignored.
func parseChannelVersion(cv string) (channelVersion, error) {
	parts := strings.Split(cv, "-")
	if len(parts) < 2 {
		return channelVersion{}, fmt.Errorf("unsupported version format: %s", cv)
	}

	channel := parts[0]
	version := parts[1]

	for _, part := range parts[2:] {
		if _, err := strconv.ParseInt(part, 10, 64); err == nil {
			version += "-" + part
			if !strings.HasSuffix(channel, hotpatchSuffix) {
				channel += hotpatchSuffix
			}
		}
	}

	return channelVersion{channel, version}, nil
}

// IsReleaseChannel returns true if the channel of the version is "edge" or
// "stable".
func IsReleaseChannel(version string) (bool, error) {
	cv, err := parseChannelVersion(version)
	if err != nil {
		return false, err
	}
	return cv.channel == "edge" || cv.channel == "stable", nil
}
