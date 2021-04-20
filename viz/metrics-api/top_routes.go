package api

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	api "github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/k8s"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	routeReqQuery             = "sum(increase(route_response_total%s[%s])) by (%s, dst, classification)"
	actualRouteReqQuery       = "sum(increase(route_actual_response_total%s[%s])) by (%s, dst, classification)"
	routeLatencyQuantileQuery = "histogram_quantile(%s, sum(irate(route_response_latency_ms_bucket%s[%s])) by (le, dst, %s))"
	dstLabel                  = `dst=~"(%s)(:\\d+)?"`
	// DefaultRouteName is the name to display for requests that don't match any routes.
	DefaultRouteName = "[DEFAULT]"
)

type dstAndRoute struct {
	dst   string
	route string
}

type indexedTable = map[dstAndRoute]*pb.RouteTable_Row

type resourceTable struct {
	resource string
	table    indexedTable
}

func (s *grpcServer) TopRoutes(ctx context.Context, req *pb.TopRoutesRequest) (*pb.TopRoutesResponse, error) {
	log.Debugf("TopRoutes request: %+v", req)

	if !s.k8sAPI.SPAvailable() {
		return topRoutesError(req, "Routes are not available"), nil
	}

	errRsp := validateRequest(req)
	if errRsp != nil {
		return errRsp, nil
	}

	// TopRoutes will return one table for each resource object requested.
	tables := make([]resourceTable, 0)
	targetResource := req.GetSelector().GetResource()
	labelSelector, err := getTopLabelSelector(req)
	if err != nil {
		return nil, err
	}
	if targetResource.GetType() == k8s.Authority {
		// Authority cannot be the target because authorities don't have namespaces,
		// therefore there is no namespace in which to look for a service profile.
		return topRoutesError(req, "Authority cannot be the target of a routes query; try using an authority in the --to flag instead"), nil
	}

	// Non-authority resource
	objects, err := s.k8sAPI.GetObjects(targetResource.Namespace, targetResource.Type, targetResource.Name, labelSelector)
	if err != nil {
		return nil, err
	}

	// Create a table for each object in the resource.
	for _, obj := range objects {
		table, err := s.topRoutesFor(ctx, req, obj)
		if err != nil {
			// No samples for this object, skip it.
			continue
		}
		tables = append(tables, *table)
	}

	if len(tables) == 0 {
		return topRoutesError(req, "No Service Profiles found for selected resources"), nil
	}

	// Construct response.
	routeTables := make([]*pb.RouteTable, 0)

	for _, t := range tables {
		rows := make([]*pb.RouteTable_Row, 0)
		for _, row := range t.table {
			rows = append(rows, row)
		}
		routeTables = append(routeTables, &pb.RouteTable{
			Resource: t.resource,
			Rows:     rows,
		})
	}

	return &pb.TopRoutesResponse{
		Response: &pb.TopRoutesResponse_Ok_{
			Ok: &pb.TopRoutesResponse_Ok{
				Routes: routeTables,
			},
		},
	}, nil
}

// topRoutesFor constructs a resource table for the given resource object.
func (s *grpcServer) topRoutesFor(ctx context.Context, req *pb.TopRoutesRequest, object runtime.Object) (*resourceTable, error) {
	// requestedResource is the destination resource.  For inbound queries, it is the target resource.
	// For outbound (i.e. --to) queries, it is the ToResource.  We will look at the service profiles
	// of this destination resource.
	name, err := api.GetNameOf(object)
	if err != nil {
		return nil, err
	}
	clientNs := req.GetSelector().GetResource().GetNamespace()
	typ := req.GetSelector().GetResource().GetType()
	labelSelector, err := getTopLabelSelector(req)
	if err != nil {
		return nil, err
	}
	targetResource := &pb.Resource{
		Name:      name,
		Namespace: req.GetSelector().GetResource().GetNamespace(),
		Type:      typ,
	}
	requestedResource := targetResource
	if req.GetToResource() != nil {
		requestedResource = req.GetToResource()
	}

	profiles := make(map[string]*sp.ServiceProfile)

	if requestedResource.GetType() == k8s.Authority {
		// Authorities may not be a source, so we know this is a ToResource.
		profiles, err = s.getProfilesForAuthority(requestedResource.GetName(), clientNs, labelSelector)
		if err != nil {
			return nil, err
		}
	} else {
		// Non-authority resource.
		// Lookup individual resource objects.
		objects, err := s.k8sAPI.GetObjects(requestedResource.Namespace, requestedResource.Type, requestedResource.Name, labelSelector)
		if err != nil {
			return nil, err
		}
		// Find service profiles for all services in all objects in the resource.
		for _, obj := range objects {
			// Lookup services for each object.
			services, err := s.k8sAPI.GetServicesFor(obj, false)
			if err != nil {
				return nil, err
			}

			for _, svc := range services {
				p := s.k8sAPI.GetServiceProfileFor(svc, clientNs, s.clusterDomain)
				profiles[svc.GetName()] = p
			}
		}
	}

	metrics, err := s.getRouteMetrics(ctx, req, profiles, targetResource)
	if err != nil {
		return nil, err
	}

	return &resourceTable{
		resource: fmt.Sprintf("%s/%s", typ, name),
		table:    metrics,
	}, nil
}

func topRoutesError(req *pb.TopRoutesRequest, message string) *pb.TopRoutesResponse {
	return &pb.TopRoutesResponse{
		Response: &pb.TopRoutesResponse_Error{
			Error: &pb.ResourceError{
				Resource: req.GetSelector().GetResource(),
				Error:    message,
			},
		},
	}
}

func validateRequest(req *pb.TopRoutesRequest) *pb.TopRoutesResponse {
	if req.GetSelector().GetResource() == nil {
		return topRoutesError(req, "TopRoutes request missing Selector Resource")
	}

	if req.GetNone() == nil {
		// This is an outbound (--to) request.
		targetType := req.GetSelector().GetResource().GetType()
		if targetType == k8s.Service || targetType == k8s.Authority {
			return topRoutesError(req, fmt.Sprintf("The %s resource type is not supported with 'to' queries", targetType))
		}
	}
	return nil
}

func (s *grpcServer) getProfilesForAuthority(authority string, clientNs string, labelSelector labels.Selector) (map[string]*sp.ServiceProfile, error) {
	if authority == "" {
		// All authorities
		ps, err := s.k8sAPI.SP().Lister().ServiceProfiles(clientNs).List(labelSelector)
		if err != nil {
			return nil, err
		}

		if len(ps) == 0 {
			return nil, errors.New("No ServiceProfiles found")
		}

		profiles := make(map[string]*sp.ServiceProfile)

		for _, p := range ps {
			profiles[p.Name] = p
		}

		return profiles, nil
	}
	// Specific authority
	p, err := s.k8sAPI.SP().Lister().ServiceProfiles(clientNs).Get(authority)
	if err != nil {
		return nil, err
	}
	return map[string]*sp.ServiceProfile{
		p.Name: p,
	}, nil
}

func (s *grpcServer) getRouteMetrics(ctx context.Context, req *pb.TopRoutesRequest, profiles map[string]*sp.ServiceProfile, resource *pb.Resource) (indexedTable, error) {
	timeWindow := req.TimeWindow

	dsts := make([]string, 0)
	for _, p := range profiles {
		dsts = append(dsts, p.GetName())
	}

	reqLabels := s.buildRouteLabels(req, dsts, resource)
	groupBy := "rt_route"

	queries := map[promType]string{
		promRequests: fmt.Sprintf(routeReqQuery, reqLabels, timeWindow, groupBy),
	}

	if req.GetOutbound() != nil && req.GetNone() == nil {
		// If this req has an Outbound, then query the actual request counts as well.
		queries[promActualRequests] = fmt.Sprintf(actualRouteReqQuery, reqLabels, timeWindow, groupBy)
	}

	quantileQueries := generateQuantileQueries(routeLatencyQuantileQuery, reqLabels, timeWindow, groupBy)
	results, err := s.getPrometheusMetrics(ctx, queries, quantileQueries)
	if err != nil {
		return nil, err
	}

	table := make(indexedTable)
	for service, profile := range profiles {
		for _, route := range profile.Spec.Routes {
			key := dstAndRoute{
				dst:   profile.GetName(),
				route: route.Name,
			}
			table[key] = &pb.RouteTable_Row{
				Authority: service,
				Route:     route.Name,
				Stats:     &pb.BasicStats{},
			}
		}
		defaultKey := dstAndRoute{
			dst:   profile.GetName(),
			route: "",
		}
		table[defaultKey] = &pb.RouteTable_Row{
			Authority: service,
			Route:     DefaultRouteName,
			Stats:     &pb.BasicStats{},
		}
	}

	processRouteMetrics(results, timeWindow, table)

	return table, nil
}

func (s *grpcServer) buildRouteLabels(req *pb.TopRoutesRequest, dsts []string, resource *pb.Resource) string {
	// labels: the labels for the resource we want to query for
	var labels model.LabelSet

	switch req.Outbound.(type) {

	case *pb.TopRoutesRequest_ToResource:
		labels = labels.Merge(promQueryLabels(resource))
		labels = labels.Merge(promDirectionLabels("outbound"))
		return renderLabels(labels, dsts)

	default:
		labels = labels.Merge(promDirectionLabels("inbound"))
		labels = labels.Merge(promQueryLabels(resource))
		return renderLabels(labels, dsts)
	}
}

func renderLabels(labels model.LabelSet, services []string) string {
	pairs := make([]string, 0)
	for k, v := range labels {
		pairs = append(pairs, fmt.Sprintf("%s=%q", k, v))
	}
	if len(services) > 0 {
		pairs = append(pairs, fmt.Sprintf(dstLabel, strings.Join(services, "|")))
	}
	sort.Strings(pairs)
	return fmt.Sprintf("{%s}", strings.Join(pairs, ", "))
}

func processRouteMetrics(results []promResult, timeWindow string, table indexedTable) {
	for _, result := range results {
		for _, sample := range result.vec {
			route := string(sample.Metric[model.LabelName("rt_route")])
			dst := string(sample.Metric[model.LabelName("dst")])
			dst = strings.Split(dst, ":")[0] // Truncate port, if there is one.

			key := dstAndRoute{dst, route}

			if table[key] == nil {
				log.Warnf("Found stats for unknown route: %s:%s", dst, route)
				continue
			}

			table[key].TimeWindow = timeWindow
			value := extractSampleValue(sample)

			switch result.prom {
			case promRequests:
				switch string(sample.Metric[model.LabelName("classification")]) {
				case success:
					table[key].Stats.SuccessCount += value
				case failure:
					table[key].Stats.FailureCount += value
				}
			case promActualRequests:
				switch string(sample.Metric[model.LabelName("classification")]) {
				case success:
					table[key].Stats.ActualSuccessCount += value
				case failure:
					table[key].Stats.ActualFailureCount += value
				}
			case promLatencyP50:
				table[key].Stats.LatencyMsP50 = value
			case promLatencyP95:
				table[key].Stats.LatencyMsP95 = value
			case promLatencyP99:
				table[key].Stats.LatencyMsP99 = value
			}
		}
	}
}

//generate correct label.Selector object according to the request
func getTopLabelSelector(req *pb.TopRoutesRequest) (labels.Selector, error) {
	labelSelector := labels.Everything()
	if s := req.GetSelector().GetLabelSelector(); s != "" {
		var err error
		labelSelector, err = labels.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("invalid label selector \"%s\": %s", s, err)
		}
	}
	return labelSelector, nil
}
