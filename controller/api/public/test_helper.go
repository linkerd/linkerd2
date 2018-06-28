package public

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	common "github.com/runconduit/conduit/controller/gen/common"
	healthcheckPb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"google.golang.org/grpc"
)

//
// Conduit Public API client
//

type MockConduitApiClient struct {
	ErrorToReturn                   error
	VersionInfoToReturn             *pb.VersionInfo
	ListPodsResponseToReturn        *pb.ListPodsResponse
	StatSummaryResponseToReturn     *pb.StatSummaryResponse
	SelfCheckResponseToReturn       *healthcheckPb.SelfCheckResponse
	Api_TapClientToReturn           pb.Api_TapClient
	Api_TapByResourceClientToReturn pb.Api_TapByResourceClient
}

func (c *MockConduitApiClient) StatSummary(ctx context.Context, in *pb.StatSummaryRequest, opts ...grpc.CallOption) (*pb.StatSummaryResponse, error) {
	return c.StatSummaryResponseToReturn, c.ErrorToReturn
}

func (c *MockConduitApiClient) Version(ctx context.Context, in *pb.Empty, opts ...grpc.CallOption) (*pb.VersionInfo, error) {
	return c.VersionInfoToReturn, c.ErrorToReturn
}

func (c *MockConduitApiClient) ListPods(ctx context.Context, in *pb.Empty, opts ...grpc.CallOption) (*pb.ListPodsResponse, error) {
	return c.ListPodsResponseToReturn, c.ErrorToReturn
}

func (c *MockConduitApiClient) Tap(ctx context.Context, in *pb.TapRequest, opts ...grpc.CallOption) (pb.Api_TapClient, error) {
	return c.Api_TapClientToReturn, c.ErrorToReturn
}

func (c *MockConduitApiClient) TapByResource(ctx context.Context, in *pb.TapByResourceRequest, opts ...grpc.CallOption) (pb.Api_TapByResourceClient, error) {
	return c.Api_TapByResourceClientToReturn, c.ErrorToReturn
}

func (c *MockConduitApiClient) SelfCheck(ctx context.Context, in *healthcheckPb.SelfCheckRequest, _ ...grpc.CallOption) (*healthcheckPb.SelfCheckResponse, error) {
	return c.SelfCheckResponseToReturn, c.ErrorToReturn
}

type MockApi_TapClient struct {
	TapEventsToReturn []common.TapEvent
	ErrorsToReturn    []error
	grpc.ClientStream
}

func (a *MockApi_TapClient) Recv() (*common.TapEvent, error) {
	var eventPopped common.TapEvent
	var errorPopped error
	if len(a.TapEventsToReturn) == 0 && len(a.ErrorsToReturn) == 0 {
		return nil, io.EOF
	}
	if len(a.TapEventsToReturn) != 0 {
		eventPopped, a.TapEventsToReturn = a.TapEventsToReturn[0], a.TapEventsToReturn[1:]
	}
	if len(a.ErrorsToReturn) != 0 {
		errorPopped, a.ErrorsToReturn = a.ErrorsToReturn[0], a.ErrorsToReturn[1:]
	}

	return &eventPopped, errorPopped
}

type MockApi_TapByResourceClient struct {
	TapEventsToReturn []common.TapEvent
	ErrorsToReturn    []error
	grpc.ClientStream
}

func (a *MockApi_TapByResourceClient) Recv() (*common.TapEvent, error) {
	var eventPopped common.TapEvent
	var errorPopped error
	if len(a.TapEventsToReturn) == 0 && len(a.ErrorsToReturn) == 0 {
		return nil, io.EOF
	}
	if len(a.TapEventsToReturn) != 0 {
		eventPopped, a.TapEventsToReturn = a.TapEventsToReturn[0], a.TapEventsToReturn[1:]
	}
	if len(a.ErrorsToReturn) != 0 {
		errorPopped, a.ErrorsToReturn = a.ErrorsToReturn[0], a.ErrorsToReturn[1:]
	}

	return &eventPopped, errorPopped
}

//
// Prometheus client
//

type MockProm struct {
	Res             model.Value
	QueriesExecuted []string // expose the queries our Mock Prometheus receives, to test query generation
	rwLock          sync.Mutex
}

type PodCounts struct {
	MeshedPods  uint64
	RunningPods uint64
	FailedPods  uint64
}

// satisfies v1.API
func (m *MockProm) Query(ctx context.Context, query string, ts time.Time) (model.Value, error) {
	m.rwLock.Lock()
	defer m.rwLock.Unlock()
	m.QueriesExecuted = append(m.QueriesExecuted, query)
	return m.Res, nil
}
func (m *MockProm) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, error) {
	m.rwLock.Lock()
	defer m.rwLock.Unlock()
	m.QueriesExecuted = append(m.QueriesExecuted, query)
	return m.Res, nil
}
func (m *MockProm) LabelValues(ctx context.Context, label string) (model.LabelValues, error) {
	return nil, nil
}
func (m *MockProm) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, error) {
	return nil, nil
}

func GenStatSummaryResponse(resName, resType, resNs string, counts *PodCounts) pb.StatSummaryResponse {
	statTableRow := &pb.StatTable_PodGroup_Row{
		Resource: &pb.Resource{
			Namespace: resNs,
			Type:      resType,
			Name:      resName,
		},
		Stats: &pb.BasicStats{
			SuccessCount:    123,
			FailureCount:    0,
			LatencyMsP50:    123,
			LatencyMsP95:    123,
			LatencyMsP99:    123,
			TlsRequestCount: 123,
		},
		TimeWindow: "1m",
	}

	if counts != nil {
		statTableRow.MeshedPodCount = counts.MeshedPods
		statTableRow.RunningPodCount = counts.RunningPods
		statTableRow.FailedPodCount = counts.FailedPods
	}

	resp := pb.StatSummaryResponse{
		Response: &pb.StatSummaryResponse_Ok_{ // https://github.com/golang/protobuf/issues/205
			Ok: &pb.StatSummaryResponse_Ok{
				StatTables: []*pb.StatTable{
					&pb.StatTable{
						Table: &pb.StatTable_PodGroup_{
							PodGroup: &pb.StatTable_PodGroup{
								Rows: []*pb.StatTable_PodGroup_Row{
									statTableRow,
								},
							},
						},
					},
				},
			},
		},
	}

	return resp
}
