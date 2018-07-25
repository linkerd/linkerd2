package logging

import (
	"flag"

	log "github.com/sirupsen/logrus"
)

// LogLevelFlag must be called before flag.Parse(). In addition to providing
// the -log-level flag, it overrides the default for glog's -logtostderr flag
// so that all glog logs are written to stderr instead of to disk.
func LogLevelFlag() *string {
	flag.Set("logtostderr", "true")
	return flag.String("log-level", log.InfoLevel.String(),
		"log level, must be one of: panic, fatal, error, warn, info, debug")
}

func SetLogLevel(logLevel string) {
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		log.Fatalf("invalid log-level: %s", logLevel)
	}
	log.SetLevel(level)
}
