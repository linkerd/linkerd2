package version

import (
	"flag"
	"fmt"
	"os"
)

// DO NOT EDIT
// This const is updated automatically as part of the build process
const Version = "unknown"

func VersionFlag() *bool {
	return flag.Bool("version", false, "print version and exit")
}

func MaybePrintVersionAndExit(printVersion bool) {
	if printVersion {
		fmt.Println(Version)
		os.Exit(0)
	}
}
