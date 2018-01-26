package version

import (
	"flag"
	"fmt"
	"os"

	log "github.com/sirupsen/logrus"
)

// DO NOT EDIT
// This var is updated automatically as part of the build process
var Version = "unknown"

func VersionFlag() *bool {
	return flag.Bool("version", false, "print version and exit")
}

func MaybePrintVersionAndExit(printVersion bool) {
	if printVersion {
		fmt.Println(Version)
		os.Exit(0)
	}
	log.Infof("running conduit version %s", Version)
}
