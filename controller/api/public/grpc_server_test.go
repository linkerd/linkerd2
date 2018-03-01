package public

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	tap "github.com/runconduit/conduit/controller/gen/controller/tap"
	telemetry "github.com/runconduit/conduit/controller/gen/controller/telemetry"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"google.golang.org/grpc"
)

type mockTelemetry struct {
	test   *testing.T
	client telemetry.TelemetryClient
	tRes   *telemetry.QueryResponse
	mReq   *pb.MetricRequest
	ts     int64
}

// satisfies telemetry.TelemetryClient
func (m *mockTelemetry) Query(ctx context.Context, in *telemetry.QueryRequest, opts ...grpc.CallOption) (*telemetry.QueryResponse, error) {

	if !atomic.CompareAndSwapInt64(&m.ts, 0, in.EndMs) {
		ts := atomic.LoadInt64(&m.ts)
		if ts != in.EndMs {
			m.test.Errorf("Timestamp changed across queries: %+v / %+v / %+v ", in, ts, in.EndMs)
		}
	}

	if in.EndMs == 0 {
		m.test.Errorf("EndMs not set in telemetry request: %+v", in)
	}
	if !m.mReq.Summarize && (in.StartMs == 0 || in.Step == "") {
		m.test.Errorf("Range params not set in timeseries request: %+v", in)
	}
	return m.tRes, nil
}
func (m *mockTelemetry) ListPods(ctx context.Context, in *telemetry.ListPodsRequest, opts ...grpc.CallOption) (*pb.ListPodsResponse, error) {
	return nil, nil
}

// sorting results makes it easier to compare against expected output
type ByHV []*pb.HistogramValue

func (hv ByHV) Len() int           { return len(hv) }
func (hv ByHV) Swap(i, j int)      { hv[i], hv[j] = hv[j], hv[i] }
func (hv ByHV) Less(i, j int) bool { return hv[i].Label <= hv[j].Label }

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
					Summarize: true,
					Window:    pb.TimeWindow_TEN_MIN,
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

			testResponse{
				tRes: &telemetry.QueryResponse{
					Metrics: []*telemetry.Sample{
						&telemetry.Sample{
							Values: []*telemetry.SampleValue{
								&telemetry.SampleValue{Value: 1, TimestampMs: 2},
							},
							Labels: map[string]string{
								sourceDeployLabel: "sourceDeployLabel",
								targetDeployLabel: "targetDeployLabel",
							},
						},
					},
				},
				mReq: &pb.MetricRequest{
					Metrics: []pb.MetricName{
						pb.MetricName_LATENCY,
					},
					Summarize: true,
					Window:    pb.TimeWindow_TEN_MIN,
				},
				mRes: &pb.MetricResponse{
					Metrics: []*pb.MetricSeries{
						&pb.MetricSeries{
							Name: pb.MetricName_LATENCY,
							Metadata: &pb.MetricMetadata{
								SourceDeploy: "sourceDeployLabel",
								TargetDeploy: "targetDeployLabel",
							},
							Datapoints: []*pb.MetricDatapoint{
								&pb.MetricDatapoint{
									Value: &pb.MetricValue{Value: &pb.MetricValue_Histogram{
										Histogram: &pb.Histogram{
											Values: []*pb.HistogramValue{
												&pb.HistogramValue{
													Label: pb.HistogramLabel_P50,
													Value: 1,
												},
												&pb.HistogramValue{
													Label: pb.HistogramLabel_P95,
													Value: 1,
												},
												&pb.HistogramValue{
													Label: pb.HistogramLabel_P99,
													Value: 1,
												},
											},
										},
									}},
									TimestampMs: 2,
								},
							},
						},
					},
				},
			},
		}

		for _, tr := range responses {
			s := newGrpcServer(&mockTelemetry{test: t, tRes: tr.tRes, mReq: tr.mReq}, tap.NewTapClient(nil), "conduit")

			res, err := s.Stat(context.Background(), tr.mReq)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			switch res.Metrics[0].Name {
			case pb.MetricName_LATENCY:
				sort.Sort(ByHV(res.Metrics[0].Datapoints[0].Value.GetHistogram().Values))
			}

			if !reflect.DeepEqual(res, tr.mRes) {
				t.Fatalf("Unexpected response:\n%+v\n!=\n%+v", res, tr.mRes)
			}
		}
	})
}

func TestFormatQueryExclusions(t *testing.T) {
	testCases := []struct {
		input          []string
		expectedOutput string
	}{
		{[]string{"conduit"}, `target_deployment!~"conduit/.*",source_deployment!~"conduit/.*"`},
		{[]string{}, ""},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d:filter out %v metrics", i, tc.input), func(t *testing.T) {
			result, err := formatQuery(countHttpQuery, &pb.MetricRequest{
				Metrics: []pb.MetricName{
					pb.MetricName_REQUEST_RATE,
				},
				Summarize: false,
				FilterBy:  &pb.MetricMetadata{TargetDeploy: "deployment/service1"},
				Window:    pb.TimeWindow_ONE_HOUR,
			}, "", tc.input)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !strings.Contains(result, tc.expectedOutput) {
				t.Fatalf("Expected test output to contain: %s\nbut got: %s\n", tc.expectedOutput, result)
			}
		})

	}
}
