package destination

import (
	"context"
	"reflect"
	"testing"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	httpPb "github.com/linkerd/linkerd2-proxy-api/go/http_types"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
)

var (
	getButNotPrivate = &sp.RequestMatch{
		All: []*sp.RequestMatch{
			&sp.RequestMatch{
				Method: "GET",
			},
			&sp.RequestMatch{
				Not: &sp.RequestMatch{
					Path: "/private/.*",
				},
			},
		},
	}

	pbGetButNotPrivate = &pb.RequestMatch{
		Match: &pb.RequestMatch_All{
			All: &pb.RequestMatch_Seq{
				Matches: []*pb.RequestMatch{
					&pb.RequestMatch{
						Match: &pb.RequestMatch_Method{
							Method: &httpPb.HttpMethod{
								Type: &httpPb.HttpMethod_Registered_{
									Registered: httpPb.HttpMethod_GET,
								},
							},
						},
					},
					&pb.RequestMatch{
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
		Path: "/login",
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
			&sp.ResponseMatch{
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
					&pb.ResponseMatch{
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
		Responses: []*sp.ResponseClass{
			&sp.ResponseClass{
				Condition: fiveXX,
				IsSuccess: false,
			},
		},
	}

	pbRoute1 = &pb.Route{
		MetricsLabels: map[string]string{
			"route": "route1",
		},
		Condition: pbGetButNotPrivate,
		ResponseClasses: []*pb.ResponseClass{
			&pb.ResponseClass{
				Condition: pbFiveXX,
				IsFailure: true,
			},
		},
	}

	route2 = &sp.RouteSpec{
		Name:      "route2",
		Condition: login,
		Responses: []*sp.ResponseClass{
			&sp.ResponseClass{
				Condition: fiveXXfourTwenty,
				IsSuccess: false,
			},
		},
	}

	pbRoute2 = &pb.Route{
		MetricsLabels: map[string]string{
			"route": "route2",
		},
		Condition: pbLogin,
		ResponseClasses: []*pb.ResponseClass{
			&pb.ResponseClass{
				Condition: pbFiveXXfourTwenty,
				IsFailure: true,
			},
		},
	}

	profile = &sp.ServiceProfile{
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				route1,
				route2,
			},
		},
	}

	pbProfile = &pb.DestinationProfile{
		Routes: []*pb.Route{
			pbRoute1,
			pbRoute2,
		},
	}

	tooManyRequestMatches = &sp.ServiceProfile{
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				&sp.RouteSpec{
					Condition: &sp.RequestMatch{
						Method: "GET",
						Path:   "/uh/oh",
					},
				},
			},
		},
	}

	notEnoughRequestMatches = &sp.ServiceProfile{
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				&sp.RouteSpec{
					Condition: &sp.RequestMatch{},
				},
			},
		},
	}

	tooManyResponseMatches = &sp.ServiceProfile{
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				&sp.RouteSpec{
					Condition: &sp.RequestMatch{
						Method: "GET",
					},
					Responses: []*sp.ResponseClass{
						&sp.ResponseClass{
							Condition: &sp.ResponseMatch{
								Status: &sp.Range{
									Min: 200,
									Max: 200,
								},
								Not: &sp.ResponseMatch{},
							},
						},
					},
				},
			},
		},
	}

	oneSidedStatusRange = &sp.ServiceProfile{
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				&sp.RouteSpec{
					Condition: &sp.RequestMatch{
						Method: "GET",
					},
					Responses: []*sp.ResponseClass{
						&sp.ResponseClass{
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
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				&sp.RouteSpec{
					Condition: &sp.RequestMatch{
						Method: "GET",
					},
					Responses: []*sp.ResponseClass{
						&sp.ResponseClass{
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
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				&sp.RouteSpec{
					Condition: &sp.RequestMatch{
						Method: "GET",
					},
					Responses: []*sp.ResponseClass{
						&sp.ResponseClass{
							Condition: &sp.ResponseMatch{},
						},
					},
				},
			},
		},
	}
)

func TestProfileListener(t *testing.T) {
	t.Run("Sends update", func(t *testing.T) {
		mockGetProfileServer := &mockDestination_GetProfileServer{profilesReceived: []*pb.DestinationProfile{}}

		listener := &profileListener{
			stream: mockGetProfileServer,
		}

		listener.Update(profile)

		numProfiles := len(mockGetProfileServer.profilesReceived)
		if numProfiles != 1 {
			t.Fatalf("Expecting [1] profile, got [%d]. Updates: %v", numProfiles, mockGetProfileServer.profilesReceived)
		}
		actualPbProfile := mockGetProfileServer.profilesReceived[0]
		if !reflect.DeepEqual(actualPbProfile, pbProfile) {
			t.Fatalf("Expected profile sent to be [%v] but was [%v]", pbProfile, actualPbProfile)
		}
	})

	t.Run("Ignores request match with too many fields", func(t *testing.T) {
		mockGetProfileServer := &mockDestination_GetProfileServer{profilesReceived: []*pb.DestinationProfile{}}

		listener := &profileListener{
			stream: mockGetProfileServer,
		}

		listener.Update(tooManyRequestMatches)

		numProfiles := len(mockGetProfileServer.profilesReceived)
		if numProfiles != 0 {
			t.Fatalf("Expecting [0] profiles, got [%d]. Updates: %v", numProfiles, mockGetProfileServer.profilesReceived)
		}
	})

	t.Run("Ignores request match without any fields", func(t *testing.T) {
		mockGetProfileServer := &mockDestination_GetProfileServer{profilesReceived: []*pb.DestinationProfile{}}

		listener := &profileListener{
			stream: mockGetProfileServer,
		}

		listener.Update(notEnoughRequestMatches)

		numProfiles := len(mockGetProfileServer.profilesReceived)
		if numProfiles != 0 {
			t.Fatalf("Expecting [0] profiles, got [%d]. Updates: %v", numProfiles, mockGetProfileServer.profilesReceived)
		}
	})

	t.Run("Ignores response match with too many fields", func(t *testing.T) {
		mockGetProfileServer := &mockDestination_GetProfileServer{profilesReceived: []*pb.DestinationProfile{}}

		listener := &profileListener{
			stream: mockGetProfileServer,
		}

		listener.Update(tooManyResponseMatches)

		numProfiles := len(mockGetProfileServer.profilesReceived)
		if numProfiles != 0 {
			t.Fatalf("Expecting [0] profiles, got [%d]. Updates: %v", numProfiles, mockGetProfileServer.profilesReceived)
		}
	})

	t.Run("Ignores response match without any fields", func(t *testing.T) {
		mockGetProfileServer := &mockDestination_GetProfileServer{profilesReceived: []*pb.DestinationProfile{}}

		listener := &profileListener{
			stream: mockGetProfileServer,
		}

		listener.Update(notEnoughResponseMatches)

		numProfiles := len(mockGetProfileServer.profilesReceived)
		if numProfiles != 0 {
			t.Fatalf("Expecting [0] profiles, got [%d]. Updates: %v", numProfiles, mockGetProfileServer.profilesReceived)
		}
	})

	t.Run("Ignores response match with invalid status range", func(t *testing.T) {
		mockGetProfileServer := &mockDestination_GetProfileServer{profilesReceived: []*pb.DestinationProfile{}}

		listener := &profileListener{
			stream: mockGetProfileServer,
		}

		listener.Update(invalidStatusRange)

		numProfiles := len(mockGetProfileServer.profilesReceived)
		if numProfiles != 0 {
			t.Fatalf("Expecting [0] profiles, got [%d]. Updates: %v", numProfiles, mockGetProfileServer.profilesReceived)
		}
	})

	t.Run("Sends update for one sided status range", func(t *testing.T) {
		mockGetProfileServer := &mockDestination_GetProfileServer{profilesReceived: []*pb.DestinationProfile{}}

		listener := &profileListener{
			stream: mockGetProfileServer,
		}

		listener.Update(oneSidedStatusRange)

		numProfiles := len(mockGetProfileServer.profilesReceived)
		if numProfiles != 1 {
			t.Fatalf("Expecting [1] profile, got [%d]. Updates: %v", numProfiles, mockGetProfileServer.profilesReceived)
		}
	})

	t.Run("It returns when the underlying context is done", func(t *testing.T) {
		context, cancelFn := context.WithCancel(context.Background())
		mockGetProfileServer := &mockDestination_GetProfileServer{
			profilesReceived: []*pb.DestinationProfile{},
			mockDestination_Server: mockDestination_Server{
				contextToReturn: context,
			},
		}
		listener := &profileListener{
			stream: mockGetProfileServer,
		}

		completed := make(chan bool)
		go func() {
			<-listener.ClientClose()
			completed <- true
		}()

		cancelFn()

		c := <-completed

		if !c {
			t.Fatalf("Expected function to be completed after the cancel()")
		}
	})
}
