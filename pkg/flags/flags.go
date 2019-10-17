package flags

import (
	"flag"
	"fmt"
	"os"

	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	"k8s.io/klog"
)

// ConfigureAndParse adds flags that are common to all go processes. This
// func calls flag.Parse(), so it should be called after all other flags have
// been configured.
func ConfigureAndParse(cmd *flag.FlagSet, args []string) {
	klog.InitFlags(nil)
	flag.Set("stderrthreshold", "FATAL")
	flag.Set("logtostderr", "false")
	flag.Set("log_file", "/dev/null")
	flag.Set("v", "0")
	logLevel := cmd.String("log-level", log.InfoLevel.String(),
		"log level, must be one of: panic, fatal, error, warn, info, debug")
	printVersion := cmd.Bool("version", false, "print version and exit")

	cmd.Parse(args)

	// set log timestamps
	formatter := &log.TextFormatter{FullTimestamp: true}
	log.SetFormatter(formatter)

	setLogLevel(*logLevel)
	maybePrintVersionAndExit(*printVersion)
}

// AddTraceFlags adds the trace-collector flag
// to the flagSet and returns their pointers for usage
func AddTraceFlags(cmd *flag.FlagSet) *string {
	traceCollector := cmd.String("trace-collector", "", "Enables OC Tracing with the specified endpoint as collector")

	return traceCollector
}

func setLogLevel(logLevel string) {
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		log.Fatalf("invalid log-level: %s", logLevel)
	}
	log.SetLevel(level)

	if level == log.DebugLevel {
		flag.Set("stderrthreshold", "INFO")
		flag.Set("logtostderr", "true")
		flag.Set("v", "6") // At 7 and higher, authorization tokens get logged.
		// pipe klog entries to logrus
		klog.SetOutput(log.StandardLogger().Writer())
	}
}

func maybePrintVersionAndExit(printVersion bool) {
	if printVersion {
		fmt.Println(version.Version)
		os.Exit(0)
	}
	log.Infof("running version %s", version.Version)
}
