package pkg

import "github.com/linkerd/linkerd2/pkg/k8s"

// ValidTargets specifies resource types allowed as a target:
// - target resource on an inbound query
// - target resource on an outbound 'to' query
// - destination resource on an outbound 'from' query
var ValidTargets = []string{
	k8s.Authority,
	k8s.CronJob,
	k8s.DaemonSet,
	k8s.Deployment,
	k8s.Job,
	k8s.Namespace,
	k8s.Pod,
	k8s.ReplicaSet,
	k8s.ReplicationController,
	k8s.StatefulSet,
}
