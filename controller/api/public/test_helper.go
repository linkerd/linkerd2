package public

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"sync"
	"time"

	destinationPb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2-proxy-api/go/net"
	"github.com/linkerd/linkerd2/controller/api/discovery"
	healthcheckPb "github.com/linkerd/linkerd2/controller/gen/common/healthcheck"
	configPb "github.com/linkerd/linkerd2/controller/gen/config"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/controller/k8s"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
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
	EdgesResponseToReturn          *pb.EdgesResponse
	SelfCheckResponseToReturn      *healthcheckPb.SelfCheckResponse
	ConfigResponseToReturn         *configPb.All
	APITapClientToReturn           pb.Api_TapClient
	APITapByResourceClientToReturn pb.Api_TapByResourceClient
	DestinationGetClientToReturn   destinationPb.Destination_GetClient
	*discovery.MockDiscoveryClient
}

// StatSummary provides a mock of a Public API method.
func (c *MockAPIClient) StatSummary(ctx context.Context, in *pb.StatSummaryRequest, opts ...grpc.CallOption) (*pb.StatSummaryResponse, error) {
	return c.StatSummaryResponseToReturn, c.ErrorToReturn
}

// TopRoutes provides a mock of a Public API method.
func (c *MockAPIClient) TopRoutes(ctx context.Context, in *pb.TopRoutesRequest, opts ...grpc.CallOption) (*pb.TopRoutesResponse, error) {
	return c.TopRoutesResponseToReturn, c.ErrorToReturn
}

// Edges provides a mock of a Public API method.
func (c *MockAPIClient) Edges(ctx context.Context, in *pb.EdgesRequest, opts ...grpc.CallOption) (*pb.EdgesResponse, error) {
	return c.EdgesResponseToReturn, c.ErrorToReturn
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

// Get provides a mock of a Public API method.
func (c *MockAPIClient) Get(ctx context.Context, in *destinationPb.GetDestination, opts ...grpc.CallOption) (destinationPb.Destination_GetClient, error) {
	return c.DestinationGetClientToReturn, c.ErrorToReturn
}

// GetProfile provides a mock of a Public API method
func (c *MockAPIClient) GetProfile(ctx context.Context, _ *destinationPb.GetDestination, _ ...grpc.CallOption) (destinationPb.Destination_GetProfileClient, error) {
	// Not implemented through this client. The proxies use the gRPC server directly instead.
	return nil, errors.New("Not implemented")
}

// SelfCheck provides a mock of a Public API method.
func (c *MockAPIClient) SelfCheck(ctx context.Context, in *healthcheckPb.SelfCheckRequest, _ ...grpc.CallOption) (*healthcheckPb.SelfCheckResponse, error) {
	return c.SelfCheckResponseToReturn, c.ErrorToReturn
}

// Config provides a mock of a Public API method.
func (c *MockAPIClient) Config(ctx context.Context, in *pb.Empty, _ ...grpc.CallOption) (*configPb.All, error) {
	return c.ConfigResponseToReturn, c.ErrorToReturn
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

// MockDestinationGetClient satisfies the Destination_GetClient gRPC interface.
type MockDestinationGetClient struct {
	UpdatesToReturn []destinationPb.Update
	ErrorsToReturn  []error
	grpc.ClientStream
	sync.Mutex
}

// Recv satisfies the Destination_GetClient.Recv() gRPC method.
func (a *MockDestinationGetClient) Recv() (*destinationPb.Update, error) {
	a.Lock()
	defer a.Unlock()
	var updatePopped destinationPb.Update
	var errorPopped error
	if len(a.UpdatesToReturn) == 0 && len(a.ErrorsToReturn) == 0 {
		return nil, io.EOF
	}
	if len(a.UpdatesToReturn) != 0 {
		updatePopped, a.UpdatesToReturn = a.UpdatesToReturn[0], a.UpdatesToReturn[1:]
	}
	if len(a.ErrorsToReturn) != 0 {
		errorPopped, a.ErrorsToReturn = a.ErrorsToReturn[0], a.ErrorsToReturn[1:]
	}

	return &updatePopped, errorPopped
}

// AuthorityEndpoints holds the details for the Endpoints associated to an authority
type AuthorityEndpoints struct {
	Namespace string
	ServiceID string
	Pods      []PodDetails
}

// PodDetails holds the details for pod associated to an Endpoint
type PodDetails struct {
	Name string
	IP   uint32
	Port uint32
}

// BuildAddrSet converts AuthorityEndpoints into its protobuf representation
func BuildAddrSet(endpoint AuthorityEndpoints) *destinationPb.WeightedAddrSet {
	addrs := make([]*destinationPb.WeightedAddr, 0)
	for _, pod := range endpoint.Pods {
		addr := &net.TcpAddress{
			Ip:   &net.IPAddress{Ip: &net.IPAddress_Ipv4{Ipv4: pod.IP}},
			Port: pod.Port,
		}
		labels := map[string]string{"pod": pod.Name}
		weightedAddr := &destinationPb.WeightedAddr{Addr: addr, MetricLabels: labels}
		addrs = append(addrs, weightedAddr)
	}
	labels := map[string]string{"namespace": endpoint.Namespace, "service": endpoint.ServiceID}
	return &destinationPb.WeightedAddrSet{Addrs: addrs, MetricLabels: labels}
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
	Status      string
	MeshedPods  uint64
	RunningPods uint64
	FailedPods  uint64
	Errors      map[string]*pb.PodErrors
}

func (m *mockProm) Query(ctx context.Context, query string, ts time.Time) (model.Value, error) {
	m.rwLock.Lock()
	defer m.rwLock.Unlock()
	m.QueriesExecuted = append(m.QueriesExecuted, query)
	return m.Res, nil
}
func (m *mockProm) QueryRange(ctx context.Context, query string, r promv1.Range) (model.Value, error) {
	m.rwLock.Lock()
	defer m.rwLock.Unlock()
	m.QueriesExecuted = append(m.QueriesExecuted, query)
	return m.Res, nil
}

func (m *mockProm) AlertManagers(ctx context.Context) (promv1.AlertManagersResult, error) {
	return promv1.AlertManagersResult{}, nil
}
func (m *mockProm) CleanTombstones(ctx context.Context) error {
	return nil
}
func (m *mockProm) Config(ctx context.Context) (promv1.ConfigResult, error) {
	return promv1.ConfigResult{}, nil
}
func (m *mockProm) DeleteSeries(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) error {
	return nil
}
func (m *mockProm) Flags(ctx context.Context) (promv1.FlagsResult, error) {
	return promv1.FlagsResult{}, nil
}
func (m *mockProm) LabelValues(ctx context.Context, label string) (model.LabelValues, error) {
	return nil, nil
}
func (m *mockProm) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, error) {
	return nil, nil
}
func (m *mockProm) Snapshot(ctx context.Context, skipHead bool) (promv1.SnapshotResult, error) {
	return promv1.SnapshotResult{}, nil
}
func (m *mockProm) Targets(ctx context.Context) (promv1.TargetsResult, error) {
	return promv1.TargetsResult{}, nil
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
				SuccessCount: 123,
				FailureCount: 0,
				LatencyMsP50: 123,
				LatencyMsP95: 123,
				LatencyMsP99: 123,
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
			statTableRow.Status = counts.Status
			statTableRow.ErrorsByPod = counts.Errors
		}

		rows = append(rows, statTableRow)
	}

	resp := pb.StatSummaryResponse{
		Response: &pb.StatSummaryResponse_Ok_{ // https://github.com/golang/protobuf/issues/205
			Ok: &pb.StatSummaryResponse_Ok{
				StatTables: []*pb.StatTable{
					{
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

// GenEdgesResponse generates a mock Public API StatSummaryResponse
// object.
func GenEdgesResponse(resourceType string, resSrc, resDst, resSrcNamespace, resDstNamespace, resClient, resServer, msg []string) pb.EdgesResponse {
	edges := []*pb.Edge{}
	for i := range resSrc {
		edge := &pb.Edge{
			Src: &pb.Resource{
				Name:      resSrc[i],
				Namespace: resSrcNamespace[i],
				Type:      resourceType,
			},
			Dst: &pb.Resource{
				Name:      resDst[i],
				Namespace: resDstNamespace[i],
				Type:      resourceType,
			},
			ClientId:      resClient[i],
			ServerId:      resServer[i],
			NoIdentityMsg: msg[i],
		}
		edges = append(edges, edge)
	}

	resp := pb.EdgesResponse{
		Response: &pb.EdgesResponse_Ok_{
			Ok: &pb.EdgesResponse_Ok{
				Edges: edges,
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
					{
						Rows:     rows,
						Resource: "deploy/foobar",
					},
				},
			},
		},
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
	k8sAPI, err := k8s.NewFakeAPI(exp.k8sConfigs...)
	if err != nil {
		return nil, nil, err
	}

	mockProm := &mockProm{Res: exp.mockPromResponse}
	fakeGrpcServer := newGrpcServer(
		mockProm,
		nil,
		nil,
		nil,
		k8sAPI,
		"linkerd",
		[]string{},
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
