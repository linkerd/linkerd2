package version

import (
	"fmt"
	"strconv"
	"strings"
)

// channelVersion is a low-level struct for handling release channels in a
// structured way. It has no dependencies on the rest of the version package.
type channelVersion struct {
	channel  string
	version  string
	hotpatch *int64
	original string
}

// hotpatchSuffix is the suffix applied to channel names to indicate that the
// version string includes a hotpatch number (e.g. dev-0.1.2-3)
const hotpatchSuffix = "Hotpatch"

func (cv channelVersion) String() string {
	return cv.original
}

// updateChannel returns the channel name to check for updates. if there's no
// hotpatch number set, then it returns the channel name itself. otherwise it
// returns the channel name suffixed with "Hotpatch" to indicate that a separate
// update channel should be used.
func (cv channelVersion) updateChannel() string {
	if cv.hotpatch != nil {
		return cv.channel + hotpatchSuffix
	}
	return cv.channel
}

// versionWithHotpatch returns the version string, suffixed with the hotpatch
// number if it exists.
func (cv channelVersion) versionWithHotpatch() string {
	if cv.hotpatch == nil {
		return cv.version
	}
	return fmt.Sprintf("%s-%d", cv.version, *cv.hotpatch)
}

func (cv channelVersion) hotpatchEqual(other channelVersion) bool {
	if cv.hotpatch == nil && other.hotpatch == nil {
		return true
	}
	if cv.hotpatch == nil || other.hotpatch == nil {
		return false
	}
	return *cv.hotpatch == *other.hotpatch
}

// parseChannelVersion parses a build string into a channelVersion struct. it
// expects the channel and version to be separated by a hyphen (e.g. dev-0.1.2).
// the version may additionally include a hotpatch number, which is separated
// from the base version by another hyphen (e.g. dev-0.1.2-3). if the version is
// suffixed with any other non-numeric build info strings (e.g. dev-0.1.2-foo),
// those strings are ignored.
func parseChannelVersion(cv string) (channelVersion, error) {
	parts := strings.Split(cv, "-")
	if len(parts) < 2 {
		return channelVersion{}, fmt.Errorf("unsupported version format: %s", cv)
	}

	channel := parts[0]
	version := parts[1]
	var hotpatch *int64

	for _, part := range parts[2:] {
		if i, err := strconv.ParseInt(part, 10, 64); err == nil {
			hotpatch = &i
			break
		}
	}

	return channelVersion{channel, version, hotpatch, cv}, nil
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
