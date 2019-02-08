package public

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	healthcheckPb "github.com/linkerd/linkerd2/controller/gen/common/healthcheck"
	"github.com/linkerd/linkerd2/controller/gen/controller/discovery"
	tap "github.com/linkerd/linkerd2/controller/gen/controller/tap"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/addr"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"google.golang.org/grpc"
)

// MockAPIClient satisfies the Public API's gRPC interfaces (public.APIClient).
type MockAPIClient struct {
	ErrorToReturn                  error
	VersionInfoToReturn            *pb.VersionInfo
	ListPodsResponseToReturn       *pb.ListPodsResponse
	ListServicesResponseToReturn   *pb.ListServicesResponse
	StatSummaryResponseToReturn    *pb.StatSummaryResponse
	TopRoutesResponseToReturn      *pb.TopRoutesResponse
	SelfCheckResponseToReturn      *healthcheckPb.SelfCheckResponse
	APITapClientToReturn           pb.Api_TapClient
	APITapByResourceClientToReturn pb.Api_TapByResourceClient
	EndpointsResponseToReturn      *discovery.EndpointsResponse
}

// StatSummary provides a mock of a Public API method.
func (c *MockAPIClient) StatSummary(ctx context.Context, in *pb.StatSummaryRequest, opts ...grpc.CallOption) (*pb.StatSummaryResponse, error) {
	return c.StatSummaryResponseToReturn, c.ErrorToReturn
}

// TopRoutes provides a mock of a Public API method.
func (c *MockAPIClient) TopRoutes(ctx context.Context, in *pb.TopRoutesRequest, opts ...grpc.CallOption) (*pb.TopRoutesResponse, error) {
	return c.TopRoutesResponseToReturn, c.ErrorToReturn
}

// Version provides a mock of a Public API method.
func (c *MockAPIClient) Version(ctx context.Context, in *pb.Empty, opts ...grpc.CallOption) (*pb.VersionInfo, error) {
	return c.VersionInfoToReturn, c.ErrorToReturn
}

// ListPods provides a mock of a Public API method.
func (c *MockAPIClient) ListPods(ctx context.Context, in *pb.ListPodsRequest, opts ...grpc.CallOption) (*pb.ListPodsResponse, error) {
	return c.ListPodsResponseToReturn, c.ErrorToReturn
}

// ListServices provides a mock of a Public API method.
func (c *MockAPIClient) ListServices(ctx context.Context, in *pb.ListServicesRequest, opts ...grpc.CallOption) (*pb.ListServicesResponse, error) {
	return c.ListServicesResponseToReturn, c.ErrorToReturn
}

// Tap provides a mock of a Public API method.
func (c *MockAPIClient) Tap(ctx context.Context, in *pb.TapRequest, opts ...grpc.CallOption) (pb.Api_TapClient, error) {
	return c.APITapClientToReturn, c.ErrorToReturn
}

// TapByResource provides a mock of a Public API method.
func (c *MockAPIClient) TapByResource(ctx context.Context, in *pb.TapByResourceRequest, opts ...grpc.CallOption) (pb.Api_TapByResourceClient, error) {
	return c.APITapByResourceClientToReturn, c.ErrorToReturn
}

// SelfCheck provides a mock of a Public API method.
func (c *MockAPIClient) SelfCheck(ctx context.Context, in *healthcheckPb.SelfCheckRequest, _ ...grpc.CallOption) (*healthcheckPb.SelfCheckResponse, error) {
	return c.SelfCheckResponseToReturn, c.ErrorToReturn
}

// Endpoints provides a mock of a Discovery API method.
func (c *MockAPIClient) Endpoints(ctx context.Context, in *discovery.EndpointsParams, _ ...grpc.CallOption) (*discovery.EndpointsResponse, error) {
	return c.EndpointsResponseToReturn, c.ErrorToReturn
}

type mockAPITapClient struct {
	TapEventsToReturn []pb.TapEvent
	ErrorsToReturn    []error
	grpc.ClientStream
}

func (a *mockAPITapClient) Recv() (*pb.TapEvent, error) {
	var eventPopped pb.TapEvent
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

// MockAPITapByResourceClient satisfies the TapByResourceClient gRPC interface.
type MockAPITapByResourceClient struct {
	TapEventsToReturn []pb.TapEvent
	ErrorsToReturn    []error
	grpc.ClientStream
}

// Recv satisfies the TapByResourceClient.Recv() gRPC method.
func (a *MockAPITapByResourceClient) Recv() (*pb.TapEvent, error) {
	var eventPopped pb.TapEvent
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

type mockProm struct {
	Res             model.Value
	QueriesExecuted []string // expose the queries our Mock Prometheus receives, to test query generation
	rwLock          sync.Mutex
}

// PodCounts is a test helper struct that is used for representing data in a
// StatTable.PodGroup.Row.
type PodCounts struct {
	MeshedPods  uint64
	RunningPods uint64
	FailedPods  uint64
}

func (m *mockProm) Query(ctx context.Context, query string, ts time.Time) (model.Value, error) {
	m.rwLock.Lock()
	defer m.rwLock.Unlock()
	m.QueriesExecuted = append(m.QueriesExecuted, query)
	return m.Res, nil
}
func (m *mockProm) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, error) {
	m.rwLock.Lock()
	defer m.rwLock.Unlock()
	m.QueriesExecuted = append(m.QueriesExecuted, query)
	return m.Res, nil
}
func (m *mockProm) LabelValues(ctx context.Context, label string) (model.LabelValues, error) {
	return nil, nil
}
func (m *mockProm) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, error) {
	return nil, nil
}

// GenStatSummaryResponse generates a mock Public API StatSummaryResponse
// object.
func GenStatSummaryResponse(resName, resType string, resNs []string, counts *PodCounts, basicStats bool, tcpStats bool) pb.StatSummaryResponse {
	rows := []*pb.StatTable_PodGroup_Row{}
	for _, ns := range resNs {
		statTableRow := &pb.StatTable_PodGroup_Row{
			Resource: &pb.Resource{
				Namespace: ns,
				Type:      resType,
				Name:      resName,
			},
			TimeWindow: "1m",
		}

		if basicStats {
			statTableRow.Stats = &pb.BasicStats{
				SuccessCount:    123,
				FailureCount:    0,
				LatencyMsP50:    123,
				LatencyMsP95:    123,
				LatencyMsP99:    123,
				TlsRequestCount: 123,
			}
		}

		if tcpStats {
			statTableRow.TcpStats = &pb.TcpStats{
				OpenConnections: 123,
				ReadBytesTotal:  123,
				WriteBytesTotal: 123,
			}
		}

		if counts != nil {
			statTableRow.MeshedPodCount = counts.MeshedPods
			statTableRow.RunningPodCount = counts.RunningPods
			statTableRow.FailedPodCount = counts.FailedPods
		}

		rows = append(rows, statTableRow)
	}

	resp := pb.StatSummaryResponse{
		Response: &pb.StatSummaryResponse_Ok_{ // https://github.com/golang/protobuf/issues/205
			Ok: &pb.StatSummaryResponse_Ok{
				StatTables: []*pb.StatTable{
					&pb.StatTable{
						Table: &pb.StatTable_PodGroup_{
							PodGroup: &pb.StatTable_PodGroup{
								Rows: rows,
							},
						},
					},
				},
			},
		},
	}

	return resp
}

// GenTopRoutesResponse generates a mock Public API TopRoutesResponse object.
func GenTopRoutesResponse(routes []string, counts []uint64, outbound bool, authority string) pb.TopRoutesResponse {
	rows := []*pb.RouteTable_Row{}
	for i, route := range routes {
		row := &pb.RouteTable_Row{
			Route:     route,
			Authority: authority,
			Stats: &pb.BasicStats{
				SuccessCount: counts[i],
				FailureCount: 0,
				LatencyMsP50: 123,
				LatencyMsP95: 123,
				LatencyMsP99: 123,
			},
			TimeWindow: "1m",
		}
		if outbound {
			row.Stats.ActualSuccessCount = counts[i]
		}
		rows = append(rows, row)
	}
	defaultRow := &pb.RouteTable_Row{
		Route:     "[DEFAULT]",
		Authority: authority,
		Stats: &pb.BasicStats{
			SuccessCount: counts[len(counts)-1],
			FailureCount: 0,
			LatencyMsP50: 123,
			LatencyMsP95: 123,
			LatencyMsP99: 123,
		},
		TimeWindow: "1m",
	}
	if outbound {
		defaultRow.Stats.ActualSuccessCount = counts[len(counts)-1]
	}
	rows = append(rows, defaultRow)

	resp := pb.TopRoutesResponse{
		Response: &pb.TopRoutesResponse_Ok_{
			Ok: &pb.TopRoutesResponse_Ok{
				Routes: []*pb.RouteTable{
					&pb.RouteTable{
						Rows:     rows,
						Resource: "deploy/foobar",
					},
				},
			},
		},
	}

	return resp
}

// GenEndpointsResponse generates a mock Public API Endpoints
// object.
// identities is a list of "pod.namespace" strings
func GenEndpointsResponse(identities []string) discovery.EndpointsResponse {
	resp := discovery.EndpointsResponse{
		ServicePorts: make(map[string]*discovery.ServicePort),
	}
	for _, identity := range identities {
		parts := strings.SplitN(identity, ".", 2)
		pod := parts[0]
		ns := parts[1]
		ip, _ := addr.ParsePublicIPV4("1.2.3.4")
		resp.ServicePorts[identity] = &discovery.ServicePort{
			PortEndpoints: map[uint32]*discovery.PodAddresses{
				8080: &discovery.PodAddresses{
					PodAddresses: []*discovery.PodAddress{
						&discovery.PodAddress{
							Addr: &pb.TcpAddress{
								Ip:   ip,
								Port: 8080,
							},
							Pod: &pb.Pod{
								Name:            ns + "/" + pod,
								Status:          "running",
								PodIP:           "1.2.3.4",
								ResourceVersion: "1234",
							},
						},
					},
				},
			},
		}
	}

	return resp
}

type expectedStatRPC struct {
	err                       error
	k8sConfigs                []string    // k8s objects to seed the API
	mockPromResponse          model.Value // mock out a prometheus query response
	expectedPrometheusQueries []string    // queries we expect public-api to issue to prometheus
}

func newMockGrpcServer(exp expectedStatRPC) (*mockProm, *grpcServer, error) {
	k8sAPI, err := k8s.NewFakeAPI("", exp.k8sConfigs...)
	if err != nil {
		return nil, nil, err
	}

	mockProm := &mockProm{Res: exp.mockPromResponse}
	fakeGrpcServer := newGrpcServer(
		mockProm,
		tap.NewTapClient(nil),
		discovery.NewDiscoveryClient(nil),
		k8sAPI,
		"linkerd",
		[]string{},
		false,
	)

	k8sAPI.Sync()

	return mockProm, fakeGrpcServer, nil
}

func (exp expectedStatRPC) verifyPromQueries(mockProm *mockProm) error {
	// if exp.expectedPrometheusQueries is an empty slice we still wanna check no queries were executed.
	if exp.expectedPrometheusQueries != nil {
		sort.Strings(exp.expectedPrometheusQueries)
		sort.Strings(mockProm.QueriesExecuted)

		// because reflect.DeepEqual([]string{}, nil) is false
		if len(exp.expectedPrometheusQueries) == 0 && len(mockProm.QueriesExecuted) == 0 {
			return nil
		}

		if !reflect.DeepEqual(exp.expectedPrometheusQueries, mockProm.QueriesExecuted) {
			return fmt.Errorf("Prometheus queries incorrect. \nExpected:\n%+v \nGot:\n%+v",
				exp.expectedPrometheusQueries, mockProm.QueriesExecuted)
		}
	}
	return nil
}
