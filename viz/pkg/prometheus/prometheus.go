package prometheus

import (
	"github.com/linkerd/linkerd2/pkg/k8s"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/prometheus/common/model"
)

const (
	NamespaceLabel         = model.LabelName("namespace")
	DstNamespaceLabel      = model.LabelName("dst_namespace")
	GatewayNameLabel       = model.LabelName("gateway_name")
	GatewayNamespaceLabel  = model.LabelName("gateway_namespace")
	RemoteClusterNameLabel = model.LabelName("target_cluster_name")
	AuthorityLabel         = model.LabelName("authority")
	ServerKindLabel        = model.LabelName("srv_kind")
	ServerNameLabel        = model.LabelName("srv_name")
	AuthorizationKindLabel = model.LabelName("authz_kind")
	AuthorizationNameLabel = model.LabelName("authz_name")
	RouteKindLabel         = model.LabelName("route_kind")
	RouteNameLabel         = model.LabelName("route_name")
)

// add filtering by resource type
// note that metricToKey assumes the label ordering (namespace, name)
func GroupByLabelNames(resource *pb.Resource) model.LabelNames {
	names := model.LabelNames{NamespaceLabel}

	if resource.Type != k8s.Namespace {
		names = append(names, ResourceType(resource))
	}
	return names
}

// query a named resource
func QueryLabels(resource *pb.Resource) model.LabelSet {
	set := model.LabelSet{}
	if resource != nil {
		if resource.Name != "" {
			if resource.GetType() == k8s.Server {
				set[ServerKindLabel] = model.LabelValue("server")
				set[ServerNameLabel] = model.LabelValue(resource.GetName())
			} else if resource.GetType() == k8s.ServerAuthorization {
				set[AuthorizationKindLabel] = model.LabelValue("serverauthorization")
				set[AuthorizationNameLabel] = model.LabelValue(resource.GetName())
			} else if resource.GetType() == k8s.AuthorizationPolicy {
				set[AuthorizationKindLabel] = model.LabelValue("authorizationpolicy")
				set[AuthorizationNameLabel] = model.LabelValue(resource.GetName())
			} else if resource.GetType() == k8s.HTTPRoute {
				set[RouteNameLabel] = model.LabelValue(resource.GetName())
			} else if resource.GetType() != k8s.Service {
				set[ResourceType(resource)] = model.LabelValue(resource.Name)
			}
		}
		if shouldAddNamespaceLabel(resource) {
			set[NamespaceLabel] = model.LabelValue(resource.Namespace)
		}
	}
	return set
}

// add filtering by resource type
// note that metricToKey assumes the label ordering (namespace, name)
func DstGroupByLabelNames(resource *pb.Resource) model.LabelNames {
	names := model.LabelNames{DstNamespaceLabel}
	if resource.Type != k8s.Namespace {
		names = append(names, "dst_"+ResourceType(resource))
	}
	return names
}

// query a named resource
func DstQueryLabels(resource *pb.Resource) model.LabelSet {
	set := model.LabelSet{}
	if resource.Name != "" {
		set["dst_"+ResourceType(resource)] = model.LabelValue(resource.Name)
		if shouldAddNamespaceLabel(resource) {
			set[DstNamespaceLabel] = model.LabelValue(resource.Namespace)
		}

	}

	return set
}

func ResourceType(resource *pb.Resource) model.LabelName {
	l5dLabel := k8s.KindToL5DLabel(resource.Type)
	return model.LabelName(l5dLabel)
}

// determine if we should add "namespace=<namespace>" to a named query
func shouldAddNamespaceLabel(resource *pb.Resource) bool {
	return resource.Type != k8s.Namespace && resource.Namespace != ""
}
