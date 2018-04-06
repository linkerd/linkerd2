package public

import (
	"context"
	"reflect"
	"testing"
	"time"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	tap "github.com/runconduit/conduit/controller/gen/controller/tap"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/k8s"
	"k8s.io/client-go/kubernetes/fake"
)

type fakeProm struct{}

func (f *fakeProm) Query(ctx context.Context, query string, ts time.Time) (model.Value, error) {
	return model.Value(model.Vector{}), nil
}
func (f *fakeProm) QueryRange(ctx context.Context, query string, r promv1.Range) (model.Value, error) {
	return model.Value(model.Vector{}), nil
}
func (f *fakeProm) LabelValues(ctx context.Context, label string) (model.LabelValues, error) {
	return model.LabelValues{}, nil
}
func (f *fakeProm) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, error) {
	return []model.LabelSet{}, nil
}

var (
	fakeGrpcServer = newGrpcServer(
		&mockTelemetry{},
		tap.NewTapClient(nil),
		fake.NewSimpleClientset(),
		&fakeProm{},
		"conduit",
	)
)

func TestStatSummary(t *testing.T) {
	t.Run("Successfully performs a query based on resource type", func(t *testing.T) {
		expectations := map[pb.StatSummaryRequest]pb.StatSummaryResponse{
			pb.StatSummaryRequest{
				Selector: &pb.ResourceSelection{
					Resource: &pb.Resource{
						Type: k8s.KubernetesDeployments,
					},
				},
			}: pb.StatSummaryResponse{
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
		}

		for req, expectedRsp := range expectations {
			rsp, err := fakeGrpcServer.StatSummary(context.TODO(), &req)
			if err != nil {
				t.Fatalf("Unexpected error: %s", err)
			}

			if !reflect.DeepEqual(expectedRsp, *rsp) {
				t.Fatalf("Expected: %+v, Got: %+v", &expectedRsp, rsp)
			}
		}
	})

	t.Run("Given an invalid resource type, returns error", func(t *testing.T) {
		expectations := map[pb.StatSummaryRequest]string{
			pb.StatSummaryRequest{
				Selector: &pb.ResourceSelection{
					Resource: &pb.Resource{
						Type: "badtype",
					},
				},
			}: "Unimplemented resource type: badtype",
			pb.StatSummaryRequest{
				Selector: &pb.ResourceSelection{
					Resource: &pb.Resource{
						Type: "deployment",
					},
				},
			}: "Unimplemented resource type: deployment",
			pb.StatSummaryRequest{
				Selector: &pb.ResourceSelection{
					Resource: &pb.Resource{
						Type: "pod",
					},
				},
			}: "Unimplemented resource type: pod",
		}

		for req, msg := range expectations {
			_, err := fakeGrpcServer.StatSummary(context.TODO(), &req)
			if err == nil {
				t.Fatalf("StatSummary(%+v) unexpectedly succeeded, should have returned %s", req, msg)
			}

			if err.Error() != msg {
				t.Fatalf("StatSummary(%+v) should have returned: %s but got unexpected message: %s", req, msg, err)
			}
		}
	})
}
