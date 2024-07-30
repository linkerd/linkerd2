package flags

import (
	"flag"
	"fmt"
	"os"

	"github.com/linkerd/linkerd2/pkg/version"

	"github.com/bombsimon/logrusr/v4"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"helm.sh/helm/v3/pkg/cli/values"
	klog "k8s.io/klog/v2"
)

const (

	// EnvOverrideNamespace is the environment variable used in the CLI to
	// overidde the control-plane's namespace
	EnvOverrideNamespace = "LINKERD_NAMESPACE"

	// EnvOverrideDockerRegistry is the environment variable used in the
	// CLI to override the docker images' registry in the control-plane
	// manifests
	EnvOverrideDockerRegistry = "LINKERD_DOCKER_REGISTRY"
)

// ConfigureAndParse adds flags that are common to all go processes. This
// func calls flag.Parse(), so it should be called after all other flags have
// been configured.
func ConfigureAndParse(cmd *flag.FlagSet, args []string) {
	logLevel := cmd.String("log-level", log.InfoLevel.String(),
		"log level, must be one of: panic, fatal, error, warn, info, debug, trace")
	logFormat := cmd.String("log-format", "plain",
		"log format, must be one of: plain, json")
	printVersion := cmd.Bool("version", false, "print version and exit")

	// We'll assume the args being passed in by calling functions have already
	// been validated and that parsing does not have errors.
	//nolint:errcheck
	cmd.Parse(args)

	log.SetFormatter(getFormatter(*logFormat))
	klog.InitFlags(nil)
	setLogLevel(*logLevel)
	klog.SetLogger(logrusr.New(log.StandardLogger()))

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

	// Loosely based on k8s logging conventions, except for 'tracing' that we
	// bump to 10 (we can see in client-go source code that level is actually
	// used) and `debug` to 6 (given that at level 7 and higher auth tokens get
	// logged)
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-instrumentation/logging.md
	switch level {
	case log.PanicLevel:
		flag.Set("v", "0")
	case log.FatalLevel:
		flag.Set("v", "0")
	case log.ErrorLevel:
		flag.Set("v", "0")
	case log.WarnLevel:
		flag.Set("v", "0")
	case log.InfoLevel:
		flag.Set("v", "2")
	case log.DebugLevel:
		flag.Set("v", "6")
	case log.TraceLevel:
		flag.Set("v", "10")
	}
}

func maybePrintVersionAndExit(printVersion bool) {
	if printVersion {
		fmt.Println(version.Version)
		os.Exit(0)
	}
	log.Infof("running version %s", version.Version)
}

func getFormatter(format string) log.Formatter {
	switch format {
	case "json":
		return &log.JSONFormatter{}
	default:
		return &log.TextFormatter{FullTimestamp: true}
	}
}

// AddValueOptionsFlags adds flags used to override default values
func AddValueOptionsFlags(f *pflag.FlagSet, v *values.Options) {
	f.StringSliceVarP(&v.ValueFiles, "values", "f", []string{}, "specify values in a YAML file or a URL (can specify multiple)")
	f.StringArrayVar(&v.Values, "set", []string{}, "set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	f.StringArrayVar(&v.StringValues, "set-string", []string{}, "set STRING values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	f.StringArrayVar(&v.FileValues, "set-file", []string{}, "set values from respective files specified via the command line (can specify multiple or separate values with commas: key1=path1,key2=path2)")
}
