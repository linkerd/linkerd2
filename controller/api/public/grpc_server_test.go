package public

import (
	"context"
	"reflect"
	"testing"

	tap "github.com/runconduit/conduit/controller/gen/controller/tap"
	telemetry "github.com/runconduit/conduit/controller/gen/controller/telemetry"
	conduit_public "github.com/runconduit/conduit/controller/gen/public"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"google.golang.org/grpc"
)

type mockTelemetry struct {
	client telemetry.TelemetryClient
	res    *telemetry.QueryResponse
}

// satisfies telemetry.TelemetryClient
func (m *mockTelemetry) Query(ctx context.Context, in *telemetry.QueryRequest, opts ...grpc.CallOption) (*telemetry.QueryResponse, error) {
	return m.res, nil
}
func (m *mockTelemetry) ListPods(ctx context.Context, in *telemetry.ListPodsRequest, opts ...grpc.CallOption) (*conduit_public.ListPodsResponse, error) {
	return nil, nil
}

type testResponse struct {
	tRes *telemetry.QueryResponse
	mReq *pb.MetricRequest
	mRes *pb.MetricResponse
}

func TestStat(t *testing.T) {
	t.Run("Stat returns the expected responses", func(t *testing.T) {

		responses := []testResponse{
			testResponse{
				tRes: &telemetry.QueryResponse{
					Metrics: []*telemetry.Sample{
						&telemetry.Sample{
							Values: []*telemetry.SampleValue{
								&telemetry.SampleValue{Value: 1, TimestampMs: 2},
							},
							Labels: map[string]string{
								pathLabel:         "pathLabel",
								sourceDeployLabel: "sourceDeployLabel",
								targetDeployLabel: "targetDeployLabel",
							},
						},
					},
				},
				mReq: &pb.MetricRequest{
					Metrics: []pb.MetricName{
						pb.MetricName_REQUEST_RATE,
					},
				},
				mRes: &pb.MetricResponse{
					Metrics: []*pb.MetricSeries{
						&pb.MetricSeries{
							Name: pb.MetricName_REQUEST_RATE,
							Metadata: &pb.MetricMetadata{
								Path:         "pathLabel",
								SourceDeploy: "sourceDeployLabel",
								TargetDeploy: "targetDeployLabel",
							},
							Datapoints: []*pb.MetricDatapoint{
								&pb.MetricDatapoint{
									Value:       &pb.MetricValue{Value: &pb.MetricValue_Gauge{Gauge: 1}},
									TimestampMs: 2,
								},
							},
						},
					},
				},
			},
		}

		for _, tr := range responses {
			s := newGrpcServer(&mockTelemetry{res: tr.tRes}, tap.NewTapClient(nil))

			res, err := s.Stat(context.Background(), tr.mReq)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !reflect.DeepEqual(res, tr.mRes) {
				t.Fatalf("Unexpected response:\n%+v\n!=\n%+v", res, tr.mRes)
			}
		}
	})
}
