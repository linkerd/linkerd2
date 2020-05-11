package testutil

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
)

const (
	envFlag  = "GH_ANNOTATION"
	rootPath = "/linkerd2/"
)

func echoAnnotation(t *testing.T, args ...interface{}) {
	if _, ok := os.LookupEnv(envFlag); ok {
		_, fileName, fileLine, ok := runtime.Caller(2)
		if !ok {
			panic("Couldn't recover runtime info")
		}
		fileName = fileName[strings.LastIndex(fileName, rootPath)+len(rootPath):]
		// In case of coming from `t.Run(testName, ...)`, only take the first part
		// of the name; the following parts might not be as generic
		parts := strings.Split(t.Name(), "/")
		testName := parts[0]
		for _, arg := range args {
			msg := fmt.Sprintf("%s - %s", testName, arg)
			fmt.Printf("::error file=%s,line=%d::%s\n", fileName, fileLine, msg)
		}
	}
}

// Error is a wrapper around t.Error()
// args are passed to t.Error(args) and each arg will be sent to stdout formatted
// as a Github annotation when the envFlag environment variable is set
func Error(t *testing.T, args ...interface{}) {
	t.Helper()
	echoAnnotation(t, args...)
	t.Error(args...)
}

// AnnotatedError is similar to Error() but it also admits a msg string that
// will be used as the Github annotation
func AnnotatedError(t *testing.T, msg string, args ...interface{}) {
	t.Helper()
	echoAnnotation(t, msg)
	t.Error(args...)
}

// Errorf is a wrapper around t.Errorf()
// format and args are passed to t.Errorf(format, args) and the formatted
// message will be sent to stdout as a Github annotation when the envFlag
// environment variable is set
func Errorf(t *testing.T, format string, args ...interface{}) {
	t.Helper()
	echoAnnotation(t, fmt.Sprintf(format, args...))
	t.Errorf(format, args...)
}

// AnnotatedErrorf is similar to Errorf() but it also admits a msg string that
// will be used as the Github annotation
func AnnotatedErrorf(t *testing.T, msg, format string, args ...interface{}) {
	t.Helper()
	echoAnnotation(t, msg)
	t.Errorf(format, args...)
}

// Fatal is a wrapper around t.Fatal()
// args are passed to t.Fatal(args) and each arg will be sent to stdout formatted
// as a Github annotation when the envFlag environment variable is set
func Fatal(t *testing.T, args ...interface{}) {
	t.Helper()
	echoAnnotation(t, args)
	t.Fatal(args...)
}

// AnnotatedFatal is similar to Fatal() but it also admits a msg string that
// will be used as the Github annotation
func AnnotatedFatal(t *testing.T, msg string, args ...interface{}) {
	t.Helper()
	echoAnnotation(t, msg)
	t.Fatal(args...)
}

// Fatalf is a wrapper around t.Errorf()
// format and args are passed to t.Fatalf(format, args) and the formatted
// message will be sent to stdout as a Github annotation when the envFlag
// environment variable is set
func Fatalf(t *testing.T, format string, args ...interface{}) {
	t.Helper()
	echoAnnotation(t, fmt.Sprintf(format, args...))
	t.Fatalf(format, args...)
}

// AnnotatedFatalf is similar to Fatalf() but it also admits a msg string that
// will be used as the Github annotation
func AnnotatedFatalf(t *testing.T, msg, format string, args ...interface{}) {
	t.Helper()
	echoAnnotation(t, msg)
	t.Fatalf(format, args...)
}
