package profiles

import (
	"strings"
	"testing"

	"github.com/emicklei/proto"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestProtoToServiceProfile(t *testing.T) {
	var (
		namespace     = "myns"
		name          = "mysvc"
		clusterDomain = "mycluster.local"
	)

	t.Run("rpc", func(t *testing.T) {
		protobuf := `syntax = "proto3";
	
	package emojivoto.v1;
	
	message VoteRequest {
	}
	
	message VotingResult {
		string Shortcode = 1;
		int32 Votes = 2;
	}
	
	service VotingService {
		rpc VotePoop (VoteRequest) returns (VoteResponse);
	}`

		parser := proto.NewParser(strings.NewReader(protobuf))

		expectedServiceProfile := sp.ServiceProfile{
			TypeMeta: ServiceProfileMeta,
			ObjectMeta: metav1.ObjectMeta{
				Name:      name + "." + namespace + ".svc." + clusterDomain,
				Namespace: namespace,
			},
			Spec: sp.ServiceProfileSpec{
				Routes: []*sp.RouteSpec{
					{
						Name: "VotePoop",
						Condition: &sp.RequestMatch{
							PathRegex: `/emojivoto\.v1\.VotingService/VotePoop`,
							Method:    "POST",
						},
						IsRetryable: false,
					},
				},
			},
		}

		actualServiceProfile, err := protoToServiceProfile(parser, namespace, name, clusterDomain)
		if err != nil {
			t.Fatalf("Failed to create ServiceProfile: %v", err)
		}

		err = ServiceProfileYamlEquals(*actualServiceProfile, expectedServiceProfile)
		if err != nil {
			t.Fatalf("ServiceProfiles are not equal: %v", err)
		}
	})

	t.Run("rpc unknown idempotency level", func(t *testing.T) {
		protobuf := `syntax = "proto3";
	
	package emojivoto.v1;
	
	message VoteRequest {
	}
	
	message VotingResult {
		string Shortcode = 1;
		int32 Votes = 2;
	}
	
	service VotingService {
		rpc VotePoop (VoteRequest) returns (VoteResponse){
			option idempotency_level = "UNKNOWN";
		};
	}`

		parser := proto.NewParser(strings.NewReader(protobuf))

		expectedServiceProfile := sp.ServiceProfile{
			TypeMeta: ServiceProfileMeta,
			ObjectMeta: metav1.ObjectMeta{
				Name:      name + "." + namespace + ".svc." + clusterDomain,
				Namespace: namespace,
			},
			Spec: sp.ServiceProfileSpec{
				Routes: []*sp.RouteSpec{
					{
						Name: "VotePoop",
						Condition: &sp.RequestMatch{
							PathRegex: `/emojivoto\.v1\.VotingService/VotePoop`,
							Method:    "POST",
						},
						IsRetryable: false,
					},
				},
			},
		}

		actualServiceProfile, err := protoToServiceProfile(parser, namespace, name, clusterDomain)
		if err != nil {
			t.Fatalf("Failed to create ServiceProfile: %v", err)
		}

		err = ServiceProfileYamlEquals(*actualServiceProfile, expectedServiceProfile)
		if err != nil {
			t.Fatalf("ServiceProfiles are not equal: %v", err)
		}
	})

	t.Run("rpc idempotent", func(t *testing.T) {
		protobuf := `syntax = "proto3";
	
	package emojivoto.v1;
	
	message VoteRequest {
	}
	
	message VotingResult {
		string Shortcode = 1;
		int32 Votes = 2;
	}
	
	service VotingService {
		rpc VotePoop (VoteRequest) returns (VoteResponse){
			option idempotency_level = "IDEMPOTENT";
		};
	}`

		parser := proto.NewParser(strings.NewReader(protobuf))

		expectedServiceProfile := sp.ServiceProfile{
			TypeMeta: ServiceProfileMeta,
			ObjectMeta: metav1.ObjectMeta{
				Name:      name + "." + namespace + ".svc." + clusterDomain,
				Namespace: namespace,
			},
			Spec: sp.ServiceProfileSpec{
				Routes: []*sp.RouteSpec{
					{
						Name: "VotePoop",
						Condition: &sp.RequestMatch{
							PathRegex: `/emojivoto\.v1\.VotingService/VotePoop`,
							Method:    "POST",
						},
						IsRetryable: true,
					},
				},
			},
		}

		actualServiceProfile, err := protoToServiceProfile(parser, namespace, name, clusterDomain)
		if err != nil {
			t.Fatalf("Failed to create ServiceProfile: %v", err)
		}

		err = ServiceProfileYamlEquals(*actualServiceProfile, expectedServiceProfile)
		if err != nil {
			t.Fatalf("ServiceProfiles are not equal: %v", err)
		}
	})

	t.Run("rpc no side effects", func(t *testing.T) {
		protobuf := `syntax = "proto3";
	
	package emojivoto.v1;
	
	message VoteRequest {
	}
	
	message VotingResult {
		string Shortcode = 1;
		int32 Votes = 2;
	}
	
	service VotingService {
		rpc VotePoop (VoteRequest) returns (VoteResponse){
			option (idempotency_level) = "NO_SIDE_EFFECTS";
		};
	}`

		parser := proto.NewParser(strings.NewReader(protobuf))

		expectedServiceProfile := sp.ServiceProfile{
			TypeMeta: ServiceProfileMeta,
			ObjectMeta: metav1.ObjectMeta{
				Name:      name + "." + namespace + ".svc." + clusterDomain,
				Namespace: namespace,
			},
			Spec: sp.ServiceProfileSpec{
				Routes: []*sp.RouteSpec{
					{
						Name: "VotePoop",
						Condition: &sp.RequestMatch{
							PathRegex: `/emojivoto\.v1\.VotingService/VotePoop`,
							Method:    "POST",
						},
						IsRetryable: true,
					},
				},
			},
		}

		actualServiceProfile, err := protoToServiceProfile(parser, namespace, name, clusterDomain)
		if err != nil {
			t.Fatalf("Failed to create ServiceProfile: %v", err)
		}

		err = ServiceProfileYamlEquals(*actualServiceProfile, expectedServiceProfile)
		if err != nil {
			t.Fatalf("ServiceProfiles are not equal: %v", err)
		}
	})
}
