package destination

import (
	"testing"

	"github.com/golang/protobuf/ptypes/duration"
	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	httpPb "github.com/linkerd/linkerd2-proxy-api/go/http_types"
	"github.com/linkerd/linkerd2-proxy-api/go/meta"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	logging "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	getButNotPrivate = &sp.RequestMatch{
		All: []*sp.RequestMatch{
			{
				Method: "GET",
			},
			{
				Not: &sp.RequestMatch{
					PathRegex: "/private/.*",
				},
			},
		},
	}

	pbGetButNotPrivate = &pb.RequestMatch{
		Match: &pb.RequestMatch_All{
			All: &pb.RequestMatch_Seq{
				Matches: []*pb.RequestMatch{
					{
						Match: &pb.RequestMatch_Method{
							Method: &httpPb.HttpMethod{
								Type: &httpPb.HttpMethod_Registered_{
									Registered: httpPb.HttpMethod_GET,
								},
							},
						},
					},
					{
						Match: &pb.RequestMatch_Not{
							Not: &pb.RequestMatch{
								Match: &pb.RequestMatch_Path{
									Path: &pb.PathMatch{
										Regex: "/private/.*",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	login = &sp.RequestMatch{
		PathRegex: "/login",
	}

	pbLogin = &pb.RequestMatch{
		Match: &pb.RequestMatch_Path{
			Path: &pb.PathMatch{
				Regex: "/login",
			},
		},
	}

	fiveXX = &sp.ResponseMatch{
		Status: &sp.Range{
			Min: 500,
			Max: 599,
		},
	}

	pbFiveXX = &pb.ResponseMatch{
		Match: &pb.ResponseMatch_Status{
			Status: &pb.HttpStatusRange{
				Min: 500,
				Max: 599,
			},
		},
	}

	fiveXXfourTwenty = &sp.ResponseMatch{
		Any: []*sp.ResponseMatch{
			fiveXX,
			{
				Status: &sp.Range{
					Min: 420,
					Max: 420,
				},
			},
		},
	}

	pbFiveXXfourTwenty = &pb.ResponseMatch{
		Match: &pb.ResponseMatch_Any{
			Any: &pb.ResponseMatch_Seq{
				Matches: []*pb.ResponseMatch{
					pbFiveXX,
					{
						Match: &pb.ResponseMatch_Status{
							Status: &pb.HttpStatusRange{
								Min: 420,
								Max: 420,
							},
						},
					},
				},
			},
		},
	}

	route1 = &sp.RouteSpec{
		Name:      "route1",
		Condition: getButNotPrivate,
		ResponseClasses: []*sp.ResponseClass{
			{
				Condition: fiveXX,
				IsFailure: true,
			},
		},
	}

	pbRoute1 = &pb.Route{
		MetricsLabels: map[string]string{
			"route": "route1",
		},
		Condition: pbGetButNotPrivate,
		ResponseClasses: []*pb.ResponseClass{
			{
				Condition: pbFiveXX,
				IsFailure: true,
			},
		},
		Timeout: nil,
	}

	route2 = &sp.RouteSpec{
		Name:      "route2",
		Condition: login,
		ResponseClasses: []*sp.ResponseClass{
			{
				Condition: fiveXXfourTwenty,
				IsFailure: true,
			},
		},
	}

	pbRoute2 = &pb.Route{
		MetricsLabels: map[string]string{
			"route": "route2",
		},
		Condition: pbLogin,
		ResponseClasses: []*pb.ResponseClass{
			{
				Condition: pbFiveXXfourTwenty,
				IsFailure: true,
			},
		},
		Timeout: nil,
	}

	spTypeMeta = metav1.TypeMeta{
		Kind: "ServiceProfile",
	}
	spObjectMeta = metav1.ObjectMeta{
		Name:      "foo.bar.svc.cluster.local",
		Namespace: "bar",
	}

	profile = &sp.ServiceProfile{
		TypeMeta:   spTypeMeta,
		ObjectMeta: spObjectMeta,
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				route1,
				route2,
			},
		},
	}

	pbProfile = &pb.DestinationProfile{
		FullyQualifiedName: "foo.bar.svc.cluster.local",
		ParentRef: &meta.Metadata{
			Kind: &meta.Metadata_Resource{
				Resource: &meta.Resource{
					Group:     "core",
					Kind:      "Service",
					Name:      "foo",
					Namespace: "bar",
					Port:      80,
				},
			},
		},
		ProfileRef: &meta.Metadata{
			Kind: &meta.Metadata_Resource{
				Resource: &meta.Resource{
					Group:     "linkerd.io",
					Kind:      "ServiceProfile",
					Name:      "foo.bar.svc.cluster.local",
					Namespace: "bar",
				},
			},
		},
		Routes: []*pb.Route{
			pbRoute1,
			pbRoute2,
		},
		RetryBudget: defaultRetryBudget(),
	}

	defaultPbProfile = &pb.DestinationProfile{
		Routes:      []*pb.Route{},
		RetryBudget: defaultRetryBudget(),
	}

	multipleRequestMatches = &sp.ServiceProfile{
		TypeMeta:   spTypeMeta,
		ObjectMeta: spObjectMeta,
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				{
					Name: "multipleRequestMatches",
					Condition: &sp.RequestMatch{
						Method:    "GET",
						PathRegex: "/my/path",
					},
				},
			},
		},
	}

	pbRequestMatchAll = &pb.DestinationProfile{
		FullyQualifiedName: pbProfile.FullyQualifiedName,
		ParentRef:          pbProfile.ParentRef,
		ProfileRef:         pbProfile.ProfileRef,
		Routes: []*pb.Route{
			{
				Condition: &pb.RequestMatch{
					Match: &pb.RequestMatch_All{
						All: &pb.RequestMatch_Seq{
							Matches: []*pb.RequestMatch{
								{
									Match: &pb.RequestMatch_Method{
										Method: &httpPb.HttpMethod{
											Type: &httpPb.HttpMethod_Registered_{
												Registered: httpPb.HttpMethod_GET,
											},
										},
									},
								},
								{
									Match: &pb.RequestMatch_Path{
										Path: &pb.PathMatch{
											Regex: "/my/path",
										},
									},
								},
							},
						},
					},
				},
				MetricsLabels: map[string]string{
					"route": "multipleRequestMatches",
				},
				ResponseClasses: []*pb.ResponseClass{},
				Timeout:         nil,
			},
		},
		RetryBudget: defaultRetryBudget(),
	}

	notEnoughRequestMatches = &sp.ServiceProfile{
		TypeMeta:   spTypeMeta,
		ObjectMeta: spObjectMeta,
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				{
					Condition: &sp.RequestMatch{},
				},
			},
		},
	}

	multipleResponseMatches = &sp.ServiceProfile{
		TypeMeta:   spTypeMeta,
		ObjectMeta: spObjectMeta,
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				{
					Name: "multipleResponseMatches",
					Condition: &sp.RequestMatch{
						Method: "GET",
					},
					ResponseClasses: []*sp.ResponseClass{
						{
							Condition: &sp.ResponseMatch{
								Status: &sp.Range{
									Min: 400,
									Max: 499,
								},
								Not: &sp.ResponseMatch{
									Status: &sp.Range{
										Min: 404,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	pbResponseMatchAll = &pb.DestinationProfile{
		FullyQualifiedName: pbProfile.FullyQualifiedName,
		ParentRef:          pbProfile.ParentRef,
		ProfileRef:         pbProfile.ProfileRef,
		Routes: []*pb.Route{
			{
				Condition: &pb.RequestMatch{
					Match: &pb.RequestMatch_Method{
						Method: &httpPb.HttpMethod{
							Type: &httpPb.HttpMethod_Registered_{
								Registered: httpPb.HttpMethod_GET,
							},
						},
					},
				},
				MetricsLabels: map[string]string{
					"route": "multipleResponseMatches",
				},
				ResponseClasses: []*pb.ResponseClass{
					{
						Condition: &pb.ResponseMatch{
							Match: &pb.ResponseMatch_All{
								All: &pb.ResponseMatch_Seq{
									Matches: []*pb.ResponseMatch{
										{
											Match: &pb.ResponseMatch_Status{
												Status: &pb.HttpStatusRange{
													Min: 400,
													Max: 499,
												},
											},
										},
										{
											Match: &pb.ResponseMatch_Not{
												Not: &pb.ResponseMatch{
													Match: &pb.ResponseMatch_Status{
														Status: &pb.HttpStatusRange{
															Min: 404,
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				Timeout: nil,
			},
		},
		RetryBudget: defaultRetryBudget(),
	}

	oneSidedStatusRange = &sp.ServiceProfile{
		TypeMeta:   spTypeMeta,
		ObjectMeta: spObjectMeta,
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				{
					Condition: &sp.RequestMatch{
						Method: "GET",
					},
					ResponseClasses: []*sp.ResponseClass{
						{
							Condition: &sp.ResponseMatch{
								Status: &sp.Range{
									Min: 200,
								},
							},
						},
					},
				},
			},
		},
	}

	invalidStatusRange = &sp.ServiceProfile{
		TypeMeta:   spTypeMeta,
		ObjectMeta: spObjectMeta,
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				{
					Condition: &sp.RequestMatch{
						Method: "GET",
					},
					ResponseClasses: []*sp.ResponseClass{
						{
							Condition: &sp.ResponseMatch{
								Status: &sp.Range{
									Min: 201,
									Max: 200,
								},
							},
						},
					},
				},
			},
		},
	}

	notEnoughResponseMatches = &sp.ServiceProfile{
		TypeMeta:   spTypeMeta,
		ObjectMeta: spObjectMeta,
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				{
					Condition: &sp.RequestMatch{
						Method: "GET",
					},
					ResponseClasses: []*sp.ResponseClass{
						{
							Condition: &sp.ResponseMatch{},
						},
					},
				},
			},
		},
	}

	routeWithTimeout = &sp.RouteSpec{
		Name:            "routeWithTimeout",
		Condition:       login,
		ResponseClasses: []*sp.ResponseClass{},
		Timeout:         "200ms",
	}

	profileWithTimeout = &sp.ServiceProfile{
		TypeMeta:   spTypeMeta,
		ObjectMeta: spObjectMeta,
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				routeWithTimeout,
			},
		},
	}

	pbRouteWithTimeout = &pb.Route{
		MetricsLabels: map[string]string{
			"route": "routeWithTimeout",
		},
		Condition:       pbLogin,
		ResponseClasses: []*pb.ResponseClass{},
		Timeout: &duration.Duration{
			Nanos: 200000000, // 200ms
		},
	}

	pbProfileWithTimeout = &pb.DestinationProfile{
		FullyQualifiedName: pbProfile.FullyQualifiedName,
		ParentRef:          pbProfile.ParentRef,
		ProfileRef:         pbProfile.ProfileRef,
		Routes: []*pb.Route{
			pbRouteWithTimeout,
		},
		RetryBudget: defaultRetryBudget(),
	}
)

func newMockTranslator(t *testing.T) (*profileTranslator, chan *pb.DestinationProfile) {
	t.Helper()
	id := watcher.ServiceID{Namespace: "bar", Name: "foo"}
	server := &mockDestinationGetProfileServer{profilesReceived: make(chan *pb.DestinationProfile, 50)}
	translator, err := newProfileTranslator(id, server, logging.WithField("test", t.Name()), "foo.bar.svc.cluster.local", 80, nil)
	if err != nil {
		t.Fatalf("failed to create profile translator: %s", err)
	}
	return translator, server.profilesReceived
}

func TestProfileTranslator(t *testing.T) {
	t.Run("Sends update", func(t *testing.T) {
		translator, profilesReceived := newMockTranslator(t)
		translator.Start()
		defer translator.Stop()

		translator.Update(profile)

		actualPbProfile := <-profilesReceived
		if !proto.Equal(actualPbProfile, pbProfile) {
			t.Fatalf("Expected profile sent to be [%v] but was [%v]", pbProfile, actualPbProfile)
		}
		numProfiles := len(profilesReceived) + 1
		if numProfiles != 1 {
			t.Fatalf("Expecting [1] profile, got [%d]. Updates: %v", numProfiles, profilesReceived)
		}
	})

	t.Run("Request match with more than one field becomes ALL", func(t *testing.T) {
		translator, profilesReceived := newMockTranslator(t)
		translator.Start()
		defer translator.Stop()

		translator.Update(multipleRequestMatches)

		actualPbProfile := <-profilesReceived
		if !proto.Equal(actualPbProfile, pbRequestMatchAll) {
			t.Fatalf("Expected profile sent to be [%v] but was [%v]", pbRequestMatchAll, actualPbProfile)
		}
		numProfiles := len(profilesReceived) + 1
		if numProfiles != 1 {
			t.Fatalf("Expecting [1] profiles, got [%d]. Updates: %v", numProfiles, profilesReceived)
		}
	})

	t.Run("Ignores request match without any fields", func(t *testing.T) {
		translator, profilesReceived := newMockTranslator(t)
		translator.Start()
		defer translator.Stop()

		translator.Update(notEnoughRequestMatches)

		numProfiles := len(profilesReceived)
		if numProfiles != 0 {
			t.Fatalf("Expecting [0] profiles, got [%d]. Updates: %v", numProfiles, profilesReceived)
		}
	})

	t.Run("Response match with more than one field becomes ALL", func(t *testing.T) {
		translator, profilesReceived := newMockTranslator(t)
		translator.Start()
		defer translator.Stop()

		translator.Update(multipleResponseMatches)

		actualPbProfile := <-profilesReceived
		if !proto.Equal(actualPbProfile, pbResponseMatchAll) {
			t.Fatalf("Expected profile sent to be [%v] but was [%v]", pbResponseMatchAll, actualPbProfile)
		}
		numProfiles := len(profilesReceived) + 1
		if numProfiles != 1 {
			t.Fatalf("Expecting [1] profiles, got [%d]. Updates: %v", numProfiles, profilesReceived)
		}
	})

	t.Run("Ignores response match without any fields", func(t *testing.T) {
		translator, profilesReceived := newMockTranslator(t)
		translator.Start()
		defer translator.Stop()

		translator.Update(notEnoughResponseMatches)

		numProfiles := len(profilesReceived)
		if numProfiles != 0 {
			t.Fatalf("Expecting [0] profiles, got [%d]. Updates: %v", numProfiles, profilesReceived)
		}
	})

	t.Run("Ignores response match with invalid status range", func(t *testing.T) {
		translator, profilesReceived := newMockTranslator(t)
		translator.Start()
		defer translator.Stop()

		translator.Update(invalidStatusRange)

		numProfiles := len(profilesReceived)
		if numProfiles != 0 {
			t.Fatalf("Expecting [0] profiles, got [%d]. Updates: %v", numProfiles, profilesReceived)
		}
	})

	t.Run("Sends update for one sided status range", func(t *testing.T) {
		translator, profilesReceived := newMockTranslator(t)
		translator.Start()
		defer translator.Stop()

		translator.Update(oneSidedStatusRange)

		<-profilesReceived

		numProfiles := len(profilesReceived) + 1
		if numProfiles != 1 {
			t.Fatalf("Expecting [1] profile, got [%d]. Updates: %v", numProfiles, profilesReceived)
		}
	})

	t.Run("Sends empty update", func(t *testing.T) {
		server := &mockDestinationGetProfileServer{profilesReceived: make(chan *pb.DestinationProfile, 50)}
		translator, err := newProfileTranslator(watcher.ID{}, server, logging.WithField("test", t.Name()), "", 80, nil)
		if err != nil {
			t.Fatalf("failed to create profile translator: %s", err)
		}

		translator.Start()
		defer translator.Stop()

		translator.Update(nil)

		actualPbProfile := <-server.profilesReceived
		if !proto.Equal(actualPbProfile, defaultPbProfile) {
			t.Fatalf("Expected profile sent to be [%v] but was [%v]", defaultPbProfile, actualPbProfile)
		}
		numProfiles := len(server.profilesReceived) + 1
		if numProfiles != 1 {
			t.Fatalf("Expecting [1] profile, got [%d]. Updates: %v", numProfiles, server.profilesReceived)
		}
	})

	t.Run("Sends update with custom timeout", func(t *testing.T) {
		translator, profilesReceived := newMockTranslator(t)
		translator.Start()
		defer translator.Stop()

		translator.Update(profileWithTimeout)

		actualPbProfile := <-profilesReceived
		if !proto.Equal(actualPbProfile, pbProfileWithTimeout) {
			t.Fatalf("Expected profile sent to be [%v] but was [%v]", pbProfileWithTimeout, actualPbProfile)
		}
		numProfiles := len(profilesReceived) + 1
		if numProfiles != 1 {
			t.Fatalf("Expecting [1] profile, got [%d]. Updates: %v", numProfiles, profilesReceived)
		}
	})
}
