package util

import (
	"fmt"
	"io"
	"strings"

	httpPb "github.com/linkerd/linkerd2-proxy-api/go/http_types"
)

// KB = Kilobyte
const KB = 1024

// MB = Megabyte
const MB = KB * 1024

// ParseScheme converts a scheme string to protobuf
// TODO: validate scheme
func ParseScheme(scheme string) *httpPb.Scheme {
	value, ok := httpPb.Scheme_Registered_value[strings.ToUpper(scheme)]
	if ok {
		return &httpPb.Scheme{
			Type: &httpPb.Scheme_Registered_{
				Registered: httpPb.Scheme_Registered(value),
			},
		}
	}
	return &httpPb.Scheme{
		Type: &httpPb.Scheme_Unregistered{
			Unregistered: strings.ToUpper(scheme),
		},
	}
}

// ParseMethod converts a method string to protobuf
// TODO: validate method
func ParseMethod(method string) *httpPb.HttpMethod {
	value, ok := httpPb.HttpMethod_Registered_value[strings.ToUpper(method)]
	if ok {
		return &httpPb.HttpMethod{
			Type: &httpPb.HttpMethod_Registered_{
				Registered: httpPb.HttpMethod_Registered(value),
			},
		}
	}
	return &httpPb.HttpMethod{
		Type: &httpPb.HttpMethod_Unregistered{
			Unregistered: strings.ToUpper(method),
		},
	}
}

// ReadAllLimit reads from r until EOF or until limit bytes are read. If EOF is
// reached, the full bytes are returned. If the limit is reached, an error is
// returned.
func ReadAllLimit(r io.Reader, limit int) ([]byte, error) {
	bytes, err := io.ReadAll(io.LimitReader(r, int64(limit)))
	if err != nil {
		return nil, err
	}
	if len(bytes) == limit {
		return nil, fmt.Errorf("limit reached while reading: %d", limit)
	}
	return bytes, nil
}
