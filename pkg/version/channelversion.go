package version

import (
	"fmt"
	"strings"
)

// channelVersion is a low-level struct for handling release channels in a
// structured way. It has no dependencies on the rest of the version package.
type channelVersion struct {
	channel  string
	version  string
	original string
}

func (cv channelVersion) String() string {
	return cv.original
}

// updateChannel returns the channel name to check for updates, returning the
// channel name.
func (cv channelVersion) updateChannel() string {
	return cv.channel
}

// parseChannelVersion parses a build string into a channelVersion struct. it
// expects the channel and version to be separated by a hyphen (e.g. dev-0.1.2).
// if the version is suffixed with any other non-numeric build info strings (e.g. dev-0.1.2-foo),
// those strings are ignored.
func parseChannelVersion(cv string) (channelVersion, error) {
	parts := strings.Split(cv, "-")
	if len(parts) < 2 {
		return channelVersion{}, fmt.Errorf("unsupported version format: %s", cv)
	}

	channel := parts[0]
	version := parts[1]

	return channelVersion{channel, version, cv}, nil
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
