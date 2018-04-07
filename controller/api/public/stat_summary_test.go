package public

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/prometheus/common/model"
	tap "github.com/runconduit/conduit/controller/gen/controller/tap"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/k8s"
	"k8s.io/client-go/kubernetes/fake"
)

type statSumExpected struct {
	err     error
	promRes model.Value
	req     pb.StatSummaryRequest
	res     pb.StatSummaryResponse
}

func TestStatSummary(t *testing.T) {
	t.Run("Successfully performs a query based on resource type", func(t *testing.T) {
		expectations := []statSumExpected{
			statSumExpected{
				err:     nil,
				promRes: model.Value(model.Vector{}),
				req: pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: k8s.KubernetesDeployments,
						},
					},
				},
				res: pb.StatSummaryResponse{
					Response: &pb.StatSummaryResponse_Ok_{ // https://github.com/golang/protobuf/issues/205
						Ok: &pb.StatSummaryResponse_Ok{
							StatTables: []*pb.StatTable{
								&pb.StatTable{
									Table: &pb.StatTable_PodGroup_{
										PodGroup: &pb.StatTable_PodGroup{
											Rows: []*pb.StatTable_PodGroup_Row{},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		for _, exp := range expectations {
			fakeGrpcServer := newGrpcServer(
				&mockTelemetry{},
				tap.NewTapClient(nil),
				fake.NewSimpleClientset(),
				&MockProm{Res: exp.promRes},
				"conduit",
			)

			rsp, err := fakeGrpcServer.StatSummary(context.TODO(), &exp.req)
			if err != exp.err {
				t.Fatalf("Expected error: %s, Got: %s", exp.err, err)
			}

			if !reflect.DeepEqual(exp.res, *rsp) {
				t.Fatalf("Expected: %+v, Got: %+v", &exp.res, rsp)
			}
		}
	})

	t.Run("Given an invalid resource type, returns error", func(t *testing.T) {
		expectations := []statSumExpected{
			statSumExpected{
				err: errors.New("Unimplemented resource type: badtype"),
				req: pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: "badtype",
						},
					},
				},
			},
			statSumExpected{
				err: errors.New("Unimplemented resource type: deployment"),
				req: pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: "deployment",
						},
					},
				},
			},
			statSumExpected{
				err: errors.New("Unimplemented resource type: pod"),
				req: pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: "pod",
						},
					},
				},
			},
		}

		for _, exp := range expectations {
			fakeGrpcServer := newGrpcServer(
				&mockTelemetry{},
				tap.NewTapClient(nil),
				fake.NewSimpleClientset(),
				&MockProm{Res: exp.promRes},
				"conduit",
			)

			_, err := fakeGrpcServer.StatSummary(context.TODO(), &exp.req)
			if err != nil || exp.err != nil {
				if (err == nil && exp.err != nil) ||
					(err != nil && exp.err == nil) ||
					(err.Error() != exp.err.Error()) {
					t.Fatalf("Unexpected error (Expected: %s, Got: %s)", exp.err, err)
				}
			}
		}
	})
}
