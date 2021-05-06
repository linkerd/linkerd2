package destination

import (
	"errors"
	"time"

	"github.com/golang/protobuf/ptypes/duration"
	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	"github.com/linkerd/linkerd2/pkg/profiles"
	"github.com/linkerd/linkerd2/pkg/util"
	logging "github.com/sirupsen/logrus"
)

const millisPerDecimilli = 10

// implements the ProfileUpdateListener interface
type profileTranslator struct {
	stream             pb.Destination_GetProfileServer
	log                *logging.Entry
	fullyQualifiedName string
	port               uint32
	endpoint           *pb.WeightedAddr
}

func newProfileTranslator(stream pb.Destination_GetProfileServer, log *logging.Entry, fqn string, port uint32, endpoint *pb.WeightedAddr) *profileTranslator {
	return &profileTranslator{
		stream:             stream,
		log:                log.WithField("component", "profile-translator"),
		fullyQualifiedName: fqn,
		port:               port,
		endpoint:           endpoint,
	}
}

func (pt *profileTranslator) Update(profile *sp.ServiceProfile) {
	if profile == nil {
		pt.stream.Send(pt.defaultServiceProfile())
		return
	}
	destinationProfile, err := pt.toServiceProfile(profile)
	if err != nil {
		pt.log.Error(err)
		return
	}
	pt.log.Debugf("Sending profile update: %+v", destinationProfile)
	pt.stream.Send(destinationProfile)
}

func (pt *profileTranslator) defaultServiceProfile() *pb.DestinationProfile {
	return &pb.DestinationProfile{
		Routes:             []*pb.Route{},
		RetryBudget:        defaultRetryBudget(),
		FullyQualifiedName: pt.fullyQualifiedName,
		Endpoint:           pt.endpoint,
	}
}

func defaultRetryBudget() *pb.RetryBudget {
	return &pb.RetryBudget{
		MinRetriesPerSecond: 10,
		RetryRatio:          0.2,
		Ttl: &duration.Duration{
			Seconds: 10,
		},
	}
}

func toDuration(d time.Duration) *duration.Duration {
	if d == 0 {
		return nil
	}
	return &duration.Duration{
		Seconds: int64(d / time.Second),
		Nanos:   int32(d % time.Second),
	}
}

// toServiceProfile returns a Proxy API DestinationProfile, given a
// ServiceProfile.
func (pt *profileTranslator) toServiceProfile(profile *sp.ServiceProfile) (*pb.DestinationProfile, error) {
	routes := make([]*pb.Route, 0)
	for _, route := range profile.Spec.Routes {
		pbRoute, err := toRoute(profile, route)
		if err != nil {
			return nil, err
		}
		routes = append(routes, pbRoute)
	}
	budget := defaultRetryBudget()
	if profile.Spec.RetryBudget != nil {
		budget.MinRetriesPerSecond = profile.Spec.RetryBudget.MinRetriesPerSecond
		budget.RetryRatio = profile.Spec.RetryBudget.RetryRatio
		ttl, err := time.ParseDuration(profile.Spec.RetryBudget.TTL)
		if err != nil {
			return nil, err
		}
		budget.Ttl = toDuration(ttl)
	}
	var opaqueProtocol bool
	if profile.Spec.OpaquePorts != nil {
		_, opaqueProtocol = profile.Spec.OpaquePorts[pt.port]
	}
	return &pb.DestinationProfile{
		Routes:             routes,
		RetryBudget:        budget,
		DstOverrides:       toDstOverrides(profile.Spec.DstOverrides),
		FullyQualifiedName: pt.fullyQualifiedName,
		Endpoint:           pt.endpoint,
		OpaqueProtocol:     opaqueProtocol,
	}, nil
}

func toDstOverrides(dsts []*sp.WeightedDst) []*pb.WeightedDst {
	pbDsts := []*pb.WeightedDst{}
	for _, dst := range dsts {
		pbDst := &pb.WeightedDst{
			Authority: dst.Authority,
			// Weights are expressed in decimillis: 10_000 represents 100%
			Weight: uint32(dst.Weight.MilliValue() * millisPerDecimilli),
		}
		pbDsts = append(pbDsts, pbDst)
	}
	return pbDsts
}

// toRoute returns a Proxy API Route, given a ServiceProfile Route.
func toRoute(profile *sp.ServiceProfile, route *sp.RouteSpec) (*pb.Route, error) {
	cond, err := toRequestMatch(route.Condition)
	if err != nil {
		return nil, err
	}
	rcs := make([]*pb.ResponseClass, 0)
	for _, rc := range route.ResponseClasses {
		pbRc, err := toResponseClass(rc)
		if err != nil {
			return nil, err
		}
		rcs = append(rcs, pbRc)
	}
	var timeout time.Duration // No default timeout
	if route.Timeout != "" {
		timeout, err = time.ParseDuration(route.Timeout)
		if err != nil {
			logging.Errorf(
				"failed to parse duration for route '%s' in service profile '%s' in namespace '%s': %s",
				route.Name,
				profile.Name,
				profile.Namespace,
				err,
			)
		}
	}
	return &pb.Route{
		Condition:       cond,
		ResponseClasses: rcs,
		MetricsLabels:   map[string]string{"route": route.Name},
		IsRetryable:     route.IsRetryable,
		Timeout:         toDuration(timeout),
	}, nil
}

// toResponseClass returns a Proxy API ResponseClass, given a ServiceProfile
// ResponseClass.
func toResponseClass(rc *sp.ResponseClass) (*pb.ResponseClass, error) {
	cond, err := toResponseMatch(rc.Condition)
	if err != nil {
		return nil, err
	}
	return &pb.ResponseClass{
		Condition: cond,
		IsFailure: rc.IsFailure,
	}, nil
}

// toResponseMatch returns a Proxy API ResponseMatch, given a ServiceProfile
// ResponseMatch.
func toResponseMatch(rspMatch *sp.ResponseMatch) (*pb.ResponseMatch, error) {
	if rspMatch == nil {
		return nil, errors.New("missing response match")
	}
	err := profiles.ValidateResponseMatch(rspMatch)
	if err != nil {
		return nil, err
	}

	matches := make([]*pb.ResponseMatch, 0)

	if rspMatch.All != nil {
		all := make([]*pb.ResponseMatch, 0)
		for _, m := range rspMatch.All {
			pbM, err := toResponseMatch(m)
			if err != nil {
				return nil, err
			}
			all = append(all, pbM)
		}
		matches = append(matches, &pb.ResponseMatch{
			Match: &pb.ResponseMatch_All{
				All: &pb.ResponseMatch_Seq{
					Matches: all,
				},
			},
		})
	}

	if rspMatch.Any != nil {
		any := make([]*pb.ResponseMatch, 0)
		for _, m := range rspMatch.Any {
			pbM, err := toResponseMatch(m)
			if err != nil {
				return nil, err
			}
			any = append(any, pbM)
		}
		matches = append(matches, &pb.ResponseMatch{
			Match: &pb.ResponseMatch_Any{
				Any: &pb.ResponseMatch_Seq{
					Matches: any,
				},
			},
		})
	}

	if rspMatch.Status != nil {
		matches = append(matches, &pb.ResponseMatch{
			Match: &pb.ResponseMatch_Status{
				Status: &pb.HttpStatusRange{
					Max: rspMatch.Status.Max,
					Min: rspMatch.Status.Min,
				},
			},
		})
	}

	if rspMatch.Not != nil {
		not, err := toResponseMatch(rspMatch.Not)
		if err != nil {
			return nil, err
		}
		matches = append(matches, &pb.ResponseMatch{
			Match: &pb.ResponseMatch_Not{
				Not: not,
			},
		})
	}

	if len(matches) == 0 {
		return nil, errors.New("A response match must have a field set")
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return &pb.ResponseMatch{
		Match: &pb.ResponseMatch_All{
			All: &pb.ResponseMatch_Seq{
				Matches: matches,
			},
		},
	}, nil
}

// toRequestMatch returns a Proxy API RequestMatch, given a ServiceProfile
// RequestMatch.
func toRequestMatch(reqMatch *sp.RequestMatch) (*pb.RequestMatch, error) {
	if reqMatch == nil {
		return nil, errors.New("missing request match")
	}
	err := profiles.ValidateRequestMatch(reqMatch)
	if err != nil {
		return nil, err
	}

	matches := make([]*pb.RequestMatch, 0)

	if reqMatch.All != nil {
		all := make([]*pb.RequestMatch, 0)
		for _, m := range reqMatch.All {
			pbM, err := toRequestMatch(m)
			if err != nil {
				return nil, err
			}
			all = append(all, pbM)
		}
		matches = append(matches, &pb.RequestMatch{
			Match: &pb.RequestMatch_All{
				All: &pb.RequestMatch_Seq{
					Matches: all,
				},
			},
		})
	}

	if reqMatch.Any != nil {
		any := make([]*pb.RequestMatch, 0)
		for _, m := range reqMatch.Any {
			pbM, err := toRequestMatch(m)
			if err != nil {
				return nil, err
			}
			any = append(any, pbM)
		}
		matches = append(matches, &pb.RequestMatch{
			Match: &pb.RequestMatch_Any{
				Any: &pb.RequestMatch_Seq{
					Matches: any,
				},
			},
		})
	}

	if reqMatch.Method != "" {
		matches = append(matches, &pb.RequestMatch{
			Match: &pb.RequestMatch_Method{
				Method: util.ParseMethod(reqMatch.Method),
			},
		})
	}

	if reqMatch.Not != nil {
		not, err := toRequestMatch(reqMatch.Not)
		if err != nil {
			return nil, err
		}
		matches = append(matches, &pb.RequestMatch{
			Match: &pb.RequestMatch_Not{
				Not: not,
			},
		})
	}

	if reqMatch.PathRegex != "" {
		matches = append(matches, &pb.RequestMatch{
			Match: &pb.RequestMatch_Path{
				Path: &pb.PathMatch{
					Regex: reqMatch.PathRegex,
				},
			},
		})
	}

	if len(matches) == 0 {
		return nil, errors.New("A request match must have a field set")
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return &pb.RequestMatch{
		Match: &pb.RequestMatch_All{
			All: &pb.RequestMatch_Seq{
				Matches: matches,
			},
		},
	}, nil
}
