package flags

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	"k8s.io/klog"
)

// ConfigureAndParse adds flags that are common to all go processes. This
// func calls flag.Parse(), so it should be called after all other flags have
// been configured.
func ConfigureAndParse() {
	// -stderrthreshold=FATAL forces klog to only log FATAL errors to stderr.
	// -logtostderr to false to not log to stderr by default.
	var klogFlags = flag.NewFlagSet("klog", flag.ExitOnError)

	klog.InitFlags(klogFlags)
	klogFlags.Set("stderrthreshold", "FATAL")
	klogFlags.Set("logtostderr", "false")
	logLevel := flag.String("log-level", log.InfoLevel.String(),
		"log level, must be one of: panic, fatal, error, warn, info, debug")
	printVersion := flag.Bool("version", false, "print version and exit")

	flag.Parse()

	setLogLevel(*logLevel)
	maybePrintVersionAndExit(*printVersion)
}

func setLogLevel(logLevel string) {
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		log.Fatalf("invalid log-level: %s", logLevel)
	}
	log.SetLevel(level)

	klog.SetOutput(ioutil.Discard)
	// Anything lower than the INFO level according to log is sent to /dev/null
	if level == log.DebugLevel {
		// Set stderr to INFO severity see https://github.com/kubernetes/klog/issues/23
		klog.SetOutputBySeverity("INFO", os.Stderr)
	}
}

func maybePrintVersionAndExit(printVersion bool) {
	if printVersion {
		fmt.Println(version.Version)
		os.Exit(0)
	}
	log.Infof("running version %s", version.Version)
}
