package util

import (
	"errors"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	corev1 "k8s.io/api/core/v1"
)

var (
	defaultMetricTimeWindow    = "1m"
	metricTimeWindowLowerBound = time.Second * 15 //the window value needs to equal or larger than that
)

// StatsBaseRequestParams contains parameters that are used to build requests
// for metrics data.  This includes requests to StatSummary and TopRoutes.
type StatsBaseRequestParams struct {
	TimeWindow    string
	Namespace     string
	ResourceType  string
	ResourceName  string
	AllNamespaces bool
}

// StatsSummaryRequestParams contains parameters that are used to build
// StatSummary requests.
type StatsSummaryRequestParams struct {
	StatsBaseRequestParams
	ToNamespace   string
	ToType        string
	ToName        string
	FromNamespace string
	FromType      string
	FromName      string
	SkipStats     bool
	TCPStats      bool
	LabelSelector string
}

// EdgesRequestParams contains parameters that are used to build
// Edges requests.
type EdgesRequestParams struct {
	Namespace     string
	ResourceType  string
	AllNamespaces bool
}

// TopRoutesRequestParams contains parameters that are used to build TopRoutes
// requests.
type TopRoutesRequestParams struct {
	StatsBaseRequestParams
	ToNamespace   string
	ToType        string
	ToName        string
	LabelSelector string
}

// GatewayRequestParams contains parameters that are used to build a
// GatewayRequest
type GatewayRequestParams struct {
	RemoteClusterName string
	GatewayNamespace  string
	TimeWindow        string
}

// BuildStatSummaryRequest builds a Public API StatSummaryRequest from a
// StatsSummaryRequestParams.
func BuildStatSummaryRequest(p StatsSummaryRequestParams) (*pb.StatSummaryRequest, error) {
	window := defaultMetricTimeWindow
	if p.TimeWindow != "" {
		w, err := time.ParseDuration(p.TimeWindow)
		if err != nil {
			return nil, err
		}

		if w < metricTimeWindowLowerBound {
			return nil, errors.New("metrics time window needs to be at least 15s")
		}

		window = p.TimeWindow
	}

	if p.AllNamespaces && p.ResourceName != "" {
		return nil, errors.New("stats for a resource cannot be retrieved by name across all namespaces")
	}

	targetNamespace := p.Namespace
	if p.AllNamespaces {
		targetNamespace = ""
	} else if p.Namespace == "" {
		targetNamespace = corev1.NamespaceDefault
	}

	resourceType, err := k8s.CanonicalResourceNameFromFriendlyName(p.ResourceType)
	if err != nil {
		return nil, err
	}

	statRequest := &pb.StatSummaryRequest{
		Selector: &pb.ResourceSelection{
			Resource: &pb.Resource{
				Namespace: targetNamespace,
				Name:      p.ResourceName,
				Type:      resourceType,
			},
			LabelSelector: p.LabelSelector,
		},
		TimeWindow: window,
		SkipStats:  p.SkipStats,
		TcpStats:   p.TCPStats,
	}

	if p.ToName != "" || p.ToType != "" || p.ToNamespace != "" {
		if p.ToNamespace == "" {
			p.ToNamespace = targetNamespace
		}
		if p.ToType == "" {
			p.ToType = resourceType
		}

		toType, err := k8s.CanonicalResourceNameFromFriendlyName(p.ToType)
		if err != nil {
			return nil, err
		}

		toResource := pb.StatSummaryRequest_ToResource{
			ToResource: &pb.Resource{
				Namespace: p.ToNamespace,
				Type:      toType,
				Name:      p.ToName,
			},
		}
		statRequest.Outbound = &toResource
	}

	if p.FromName != "" || p.FromType != "" || p.FromNamespace != "" {
		if p.FromNamespace == "" {
			p.FromNamespace = targetNamespace
		}
		if p.FromType == "" {
			p.FromType = resourceType
		}

		fromType, err := validateFromResourceType(p.FromType)
		if err != nil {
			return nil, err
		}

		fromResource := pb.StatSummaryRequest_FromResource{
			FromResource: &pb.Resource{
				Namespace: p.FromNamespace,
				Type:      fromType,
				Name:      p.FromName,
			},
		}
		statRequest.Outbound = &fromResource
	}

	return statRequest, nil
}

// BuildEdgesRequest builds a Public API EdgesRequest from a
// EdgesRequestParams.
func BuildEdgesRequest(p EdgesRequestParams) (*pb.EdgesRequest, error) {
	namespace := p.Namespace

	// If all namespaces was specified, ignore namespace value.
	if p.AllNamespaces {
		namespace = ""
	}

	resourceType, err := k8s.CanonicalResourceNameFromFriendlyName(p.ResourceType)
	if err != nil {
		return nil, err
	}

	edgesRequest := &pb.EdgesRequest{
		Selector: &pb.ResourceSelection{
			Resource: &pb.Resource{
				Namespace: namespace,
				Type:      resourceType,
			},
		},
	}

	return edgesRequest, nil
}

// BuildTopRoutesRequest builds a Public API TopRoutesRequest from a
// TopRoutesRequestParams.
func BuildTopRoutesRequest(p TopRoutesRequestParams) (*pb.TopRoutesRequest, error) {
	window := defaultMetricTimeWindow
	if p.TimeWindow != "" {
		_, err := time.ParseDuration(p.TimeWindow)
		if err != nil {
			return nil, err
		}
		window = p.TimeWindow
	}

	if p.AllNamespaces && p.ResourceName != "" {
		return nil, errors.New("routes for a resource cannot be retrieved by name across all namespaces")
	}

	targetNamespace := p.Namespace
	if p.AllNamespaces {
		targetNamespace = ""
	} else if p.Namespace == "" {
		targetNamespace = corev1.NamespaceDefault
	}

	resourceType, err := k8s.CanonicalResourceNameFromFriendlyName(p.ResourceType)
	if err != nil {
		return nil, err
	}

	topRoutesRequest := &pb.TopRoutesRequest{
		Selector: &pb.ResourceSelection{
			Resource: &pb.Resource{
				Namespace: targetNamespace,
				Name:      p.ResourceName,
				Type:      resourceType,
			},
			LabelSelector: p.LabelSelector,
		},
		TimeWindow: window,
	}

	if p.ToName != "" || p.ToType != "" || p.ToNamespace != "" {
		if p.ToNamespace == "" {
			p.ToNamespace = targetNamespace
		}
		if p.ToType == "" {
			p.ToType = resourceType
		}

		toType, err := k8s.CanonicalResourceNameFromFriendlyName(p.ToType)
		if err != nil {
			return nil, err
		}

		toResource := pb.TopRoutesRequest_ToResource{
			ToResource: &pb.Resource{
				Namespace: p.ToNamespace,
				Type:      toType,
				Name:      p.ToName,
			},
		}
		topRoutesRequest.Outbound = &toResource
	} else {
		topRoutesRequest.Outbound = &pb.TopRoutesRequest_None{
			None: &pb.Empty{},
		}
	}

	return topRoutesRequest, nil
}

// An authority can only receive traffic, not send it, so it can't be a --from
func validateFromResourceType(resourceType string) (string, error) {
	name, err := k8s.CanonicalResourceNameFromFriendlyName(resourceType)
	if err != nil {
		return "", err
	}
	if name == k8s.Authority {
		return "", errors.New("cannot query traffic --from an authority")
	}
	return name, nil
}

// K8sPodToPublicPod converts a Kubernetes Pod to a Public API Pod
func K8sPodToPublicPod(pod corev1.Pod, ownerKind string, ownerName string) *pb.Pod {
	status := string(pod.Status.Phase)
	if pod.DeletionTimestamp != nil {
		status = "Terminating"
	}

	if pod.Status.Reason == "Evicted" {
		status = "Evicted"
	}

	controllerComponent := pod.Labels[k8s.ControllerComponentLabel]
	controllerNS := pod.Labels[k8s.ControllerNSLabel]

	item := &pb.Pod{
		Name:                pod.Namespace + "/" + pod.Name,
		Status:              status,
		PodIP:               pod.Status.PodIP,
		ControllerNamespace: controllerNS,
		ControlPlane:        controllerComponent != "",
		ProxyReady:          k8s.GetProxyReady(pod),
		ProxyVersion:        k8s.GetProxyVersion(pod),
		ResourceVersion:     pod.ResourceVersion,
	}

	namespacedOwnerName := pod.Namespace + "/" + ownerName

	switch ownerKind {
	case k8s.Deployment:
		item.Owner = &pb.Pod_Deployment{Deployment: namespacedOwnerName}
	case k8s.DaemonSet:
		item.Owner = &pb.Pod_DaemonSet{DaemonSet: namespacedOwnerName}
	case k8s.Job:
		item.Owner = &pb.Pod_Job{Job: namespacedOwnerName}
	case k8s.ReplicaSet:
		item.Owner = &pb.Pod_ReplicaSet{ReplicaSet: namespacedOwnerName}
	case k8s.ReplicationController:
		item.Owner = &pb.Pod_ReplicationController{ReplicationController: namespacedOwnerName}
	case k8s.StatefulSet:
		item.Owner = &pb.Pod_StatefulSet{StatefulSet: namespacedOwnerName}
	}

	return item
}
