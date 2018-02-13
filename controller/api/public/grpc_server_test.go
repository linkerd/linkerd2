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
								&telemetry.SampleValue{Value: 3, TimestampMs: 4},
							},
							Labels: map[string]string{
								sourceDeployLabel: "sourceDeployLabel",
								targetDeployLabel: "targetDeployLabel",
							},
						},
						&telemetry.Sample{
							Values: []*telemetry.SampleValue{
								&telemetry.SampleValue{Value: 5, TimestampMs: 6},
								&telemetry.SampleValue{Value: 7, TimestampMs: 8},
							},
							Labels: map[string]string{
								sourceDeployLabel: "sourceDeployLabel2",
								targetDeployLabel: "targetDeployLabel2",
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
								SourceDeploy: "sourceDeployLabel",
								TargetDeploy: "targetDeployLabel",
							},
							Datapoints: []*pb.MetricDatapoint{
								&pb.MetricDatapoint{
									Value:       &pb.MetricValue{Value: &pb.MetricValue_Gauge{Gauge: 1}},
									TimestampMs: 2,
								},
								&pb.MetricDatapoint{
									Value:       &pb.MetricValue{Value: &pb.MetricValue_Gauge{Gauge: 3}},
									TimestampMs: 4,
								},
							},
						},
						&pb.MetricSeries{
							Name: pb.MetricName_REQUEST_RATE,
							Metadata: &pb.MetricMetadata{
								SourceDeploy: "sourceDeployLabel2",
								TargetDeploy: "targetDeployLabel2",
							},
							Datapoints: []*pb.MetricDatapoint{
								&pb.MetricDatapoint{
									Value:       &pb.MetricValue{Value: &pb.MetricValue_Gauge{Gauge: 5}},
									TimestampMs: 6,
								},
								&pb.MetricDatapoint{
									Value:       &pb.MetricValue{Value: &pb.MetricValue_Gauge{Gauge: 7}},
									TimestampMs: 8,
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
