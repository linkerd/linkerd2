package util

import (
	"strings"

	httpPb "github.com/linkerd/linkerd2-proxy-api/go/http_types"
)

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
