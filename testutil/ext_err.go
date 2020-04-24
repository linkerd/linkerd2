package testutil

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
)

const envFlag = "GH_ANNOTATION"

// extError is a wrapper to the error type, extending it with the file and line
// that produced the error, and a generic error description that is sent to stdout
// as a Github annotation when the envFlag environment variable is set.
type extError struct {
	err  error
	file string
	line int
	desc string
}

func newExtError(err error, desc string) extError {
	_, fn, ln, ok := runtime.Caller(2)
	if !ok {
		panic("Couldn't recover runtime info")
	}
	fileName := fn[strings.LastIndex(fn, "/linkerd2/")+10:]
	return extError{
		err:  err,
		file: fileName,
		line: ln,
		desc: desc,
	}
}

// Error is a wrapper for test error messages; msg can either be
// a string or an error type.
func Error(msg interface{}) extError {
	switch v := msg.(type) {
	case error:
		return newExtError(v, v.Error())
	case string:
		return newExtError(errors.New(v), v)
	default:
		panic("Invalid type calling testutil.Error()")
	}

}

// Errorf is a wrapper for test error messages, following
// the Printf() signature (using a format specifier)
func Errorf(format string, args ...interface{}) extError {
	err := fmt.Errorf(format, args...)
	return newExtError(err, err.Error())
}

// WithAnn is to be called on a extError type to provide a generic
// error description
func (extErr extError) WithAnn(desc string) extError {
	extErr.desc = desc
	return extErr
}

// Error ensures that the struct extError implements the `error` interface.
// When extError.Error() is called, the underlying Error() is returned, and
// as a side-effect (only if the envFlag environment variable is set) a Github
// annotation string is sent to stdout
func (extErr extError) Error() string {
	if _, ok := os.LookupEnv(envFlag); ok {
		fmt.Printf("::error file=%s,line=%d::%s\n", extErr.file, extErr.line, extErr.desc)
	}
	return extErr.err.Error()
}
