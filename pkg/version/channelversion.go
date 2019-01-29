package version

import (
	"fmt"
	"strings"
)

// channelVersion is a low-level struct for handling release channels in a
// structured way. It has no dependencies on the rest of the version package.
type channelVersion struct {
	channel string
	version string
}

// String returns a string representation of a channelVersion, for example:
// { "channel": "version"} => "channel-version"
func (cv channelVersion) String() string {
	return fmt.Sprintf("%s-%s", cv.channel, cv.version)
}

func parseChannelVersion(cv string) (channelVersion, error) {
	if parts := strings.SplitN(cv, "-", 2); len(parts) == 2 {
		return channelVersion{
			channel: parts[0],
			version: parts[1],
		}, nil
	}
	return channelVersion{}, fmt.Errorf("unsupported version format: %s", cv)
}
