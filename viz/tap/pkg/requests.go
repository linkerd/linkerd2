package pkg

import (
	"fmt"

	"github.com/linkerd/linkerd2/controller/api/util"
	"github.com/linkerd/linkerd2/pkg/k8s"
	metricsPb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/linkerd/linkerd2/viz/pkg"
	tapPb "github.com/linkerd/linkerd2/viz/tap/gen/tap"
)

// ValidTapDestinations specifies resource types allowed as a tap destination:
// - destination resource on an outbound 'to' query
var ValidTapDestinations = []string{
	k8s.CronJob,
	k8s.DaemonSet,
	k8s.Deployment,
	k8s.Job,
	k8s.Namespace,
	k8s.Pod,
	k8s.ReplicaSet,
	k8s.ReplicationController,
	k8s.Service,
	k8s.StatefulSet,
}

// TapRequestParams contains parameters that are used to build a
// TapByResourceRequest.
type TapRequestParams struct {
	Resource      string
	Namespace     string
	ToResource    string
	ToNamespace   string
	MaxRps        float32
	Scheme        string
	Method        string
	Authority     string
	Path          string
	Extract       bool
	LabelSelector string
}

// BuildTapByResourceRequest builds a Public API TapByResourceRequest from a
// TapRequestParams.
func BuildTapByResourceRequest(params TapRequestParams) (*tapPb.TapByResourceRequest, error) {
	target, err := util.BuildResource(params.Namespace, params.Resource)
	if err != nil {
		return nil, fmt.Errorf("target resource invalid: %s", err)
	}
	if !contains(pkg.ValidTargets, target.Type) {
		return nil, fmt.Errorf("unsupported resource type [%s]", target.Type)
	}

	matches := []*tapPb.TapByResourceRequest_Match{}

	if params.ToResource != "" {
		destination, err := util.BuildResource(params.ToNamespace, params.ToResource)
		if err != nil {
			return nil, fmt.Errorf("destination resource invalid: %s", err)
		}
		if !contains(ValidTapDestinations, destination.Type) {
			return nil, fmt.Errorf("unsupported resource type [%s]", destination.Type)
		}

		match := tapPb.TapByResourceRequest_Match{
			Match: &tapPb.TapByResourceRequest_Match_Destinations{
				Destinations: &metricsPb.ResourceSelection{
					Resource: destination,
				},
			},
		}
		matches = append(matches, &match)
	}

	if params.Scheme != "" {
		match := buildMatchHTTP(&tapPb.TapByResourceRequest_Match_Http{
			Match: &tapPb.TapByResourceRequest_Match_Http_Scheme{Scheme: params.Scheme},
		})
		matches = append(matches, &match)
	}
	if params.Method != "" {
		match := buildMatchHTTP(&tapPb.TapByResourceRequest_Match_Http{
			Match: &tapPb.TapByResourceRequest_Match_Http_Method{Method: params.Method},
		})
		matches = append(matches, &match)
	}
	if params.Authority != "" {
		match := buildMatchHTTP(&tapPb.TapByResourceRequest_Match_Http{
			Match: &tapPb.TapByResourceRequest_Match_Http_Authority{Authority: params.Authority},
		})
		matches = append(matches, &match)
	}
	if params.Path != "" {
		match := buildMatchHTTP(&tapPb.TapByResourceRequest_Match_Http{
			Match: &tapPb.TapByResourceRequest_Match_Http_Path{Path: params.Path},
		})
		matches = append(matches, &match)
	}

	extract := &tapPb.TapByResourceRequest_Extract{}
	if params.Extract {
		extract = buildExtractHTTP(&tapPb.TapByResourceRequest_Extract_Http{
			Extract: &tapPb.TapByResourceRequest_Extract_Http_Headers_{
				Headers: &tapPb.TapByResourceRequest_Extract_Http_Headers{},
			},
		})
	}

	return &tapPb.TapByResourceRequest{
		Target: &metricsPb.ResourceSelection{
			Resource:      target,
			LabelSelector: params.LabelSelector,
		},
		MaxRps: params.MaxRps,
		Match: &tapPb.TapByResourceRequest_Match{
			Match: &tapPb.TapByResourceRequest_Match_All{
				All: &tapPb.TapByResourceRequest_Match_Seq{
					Matches: matches,
				},
			},
		},
		Extract: extract,
	}, nil
}

func buildMatchHTTP(match *tapPb.TapByResourceRequest_Match_Http) tapPb.TapByResourceRequest_Match {
	return tapPb.TapByResourceRequest_Match{
		Match: &tapPb.TapByResourceRequest_Match_Http_{
			Http: match,
		},
	}
}

func buildExtractHTTP(extract *tapPb.TapByResourceRequest_Extract_Http) *tapPb.TapByResourceRequest_Extract {
	return &tapPb.TapByResourceRequest_Extract{
		Extract: &tapPb.TapByResourceRequest_Extract_Http_{
			Http: extract,
		},
	}
}

func contains(list []string, s string) bool {
	for _, elem := range list {
		if s == elem {
			return true
		}
	}
	return false
}
