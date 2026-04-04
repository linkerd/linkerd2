package endpoints

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	destinationpb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination"
	pkgaddr "github.com/linkerd/linkerd2/pkg/addr"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/testutil"
)

func TestDestinationAPIStreamTracksRolloutEndpoints(t *testing.T) {
	ctx := context.Background()

	TestHelper.WithDataPlaneNamespace(ctx, "destination-api-rollout", map[string]string{}, t, func(t *testing.T, ns string) {
		const deployName = "rollout-echo"
		const serviceName = "rollout-echo"

		// Create a simple meshed workload and service, then wait for rollout so we
		// don't race destination watches against Kubernetes scheduling.
		applyDeploymentAndService(t, ns, deployName, serviceName, 1)
		TestHelper.WaitRollout(t, map[string]testutil.DeploySpec{
			deployName: {
				Namespace: ns,
				Replicas:  1,
			},
		})

		authority := serviceAuthority(serviceName, ns)
		client, conn := newDestinationClient(t, ctx)
		defer conn.Close()

		// Start a long-lived destination stream and verify it converges on the
		// same initial endpoint set reported by Kubernetes Endpoints.
		sub := newSubscriber(t, client, authority, "")

		initialEndpoints := waitForServiceEndpoints(t, ns, serviceName, 60*time.Second)
		sub.WaitForExact(t, initialEndpoints, 60*time.Second)

		// Trigger a rollout and assert the same stream eventually delivers the
		// new endpoint set, including Add/Remove updates as pods change.
		if _, err := TestHelper.Kubectl("", "-n", ns, "rollout", "restart", "deployment", deployName); err != nil {
			testutil.AnnotatedFatalf(t, "failed to restart deployment", "failed to restart deployment %s: %v", deployName, err)
		}
		TestHelper.WaitRollout(t, map[string]testutil.DeploySpec{
			deployName: {
				Namespace: ns,
				Replicas:  1,
			},
		})

		updatedEndpoints := waitForChangedServiceEndpoints(t, ns, serviceName, initialEndpoints, 60*time.Second)
		sub.WaitForExact(t, updatedEndpoints, 60*time.Second)
	})
}

func TestDestinationAPIConcurrentSubscribers(t *testing.T) {
	ctx := context.Background()

	TestHelper.WithDataPlaneNamespace(ctx, "destination-api-subscribers", map[string]string{}, t, func(t *testing.T, ns string) {
		const (
			serviceA = "subscriber-a"
			serviceB = "subscriber-b"
			deployA  = "subscriber-a"
			deployB  = "subscriber-b"
		)

		applyDeploymentAndService(t, ns, deployA, serviceA, 2)
		applyDeploymentAndService(t, ns, deployB, serviceB, 1)
		TestHelper.WaitRollout(t, map[string]testutil.DeploySpec{
			deployA: {
				Namespace: ns,
				Replicas:  2,
			},
			deployB: {
				Namespace: ns,
				Replicas:  1,
			},
		})

		authorityA := serviceAuthority(serviceA, ns)
		authorityB := serviceAuthority(serviceB, ns)

		client, conn := newDestinationClient(t, ctx)
		defer conn.Close()

		// Create multiple concurrent subscribers to the same authority and one
		// subscriber for a different authority.
		subA1 := newSubscriber(t, client, authorityA, "")
		subA2 := newSubscriber(t, client, authorityA, "")
		subA3 := newSubscriber(t, client, authorityA, "")
		subB := newSubscriber(t, client, authorityB, "")

		expectedA := waitForServiceEndpoints(t, ns, serviceA, 60*time.Second)
		expectedB := waitForServiceEndpoints(t, ns, serviceB, 60*time.Second)

		subA1.WaitForExact(t, expectedA, 60*time.Second)
		subA2.WaitForExact(t, expectedA, 60*time.Second)
		subA3.WaitForExact(t, expectedA, 60*time.Second)
		subB.WaitForExact(t, expectedB, 60*time.Second)

		if _, err := TestHelper.Kubectl("", "-n", ns, "scale", "deployment", deployA, "--replicas=3"); err != nil {
			testutil.AnnotatedFatalf(t, "failed to scale deployment", "failed to scale deployment %s: %v", deployA, err)
		}
		TestHelper.WaitRollout(t, map[string]testutil.DeploySpec{
			deployA: {
				Namespace: ns,
				Replicas:  3,
			},
		})

		expectedAAfterScale := waitForChangedServiceEndpoints(t, ns, serviceA, expectedA, 60*time.Second)
		subA1.WaitForExact(t, expectedAAfterScale, 60*time.Second)
		subA2.WaitForExact(t, expectedAAfterScale, 60*time.Second)
		subA3.WaitForExact(t, expectedAAfterScale, 60*time.Second)

		// The unrelated authority should remain stable and not receive updates
		// caused by changes to service A.
		subB.WaitForExact(t, expectedB, 60*time.Second)
	})
}

func TestDestinationAPIForZonesSourceNodeFiltering(t *testing.T) {
	ctx := context.Background()

	TestHelper.WithDataPlaneNamespace(ctx, "destination-api-zones", map[string]string{}, t, func(t *testing.T, ns string) {
		const deployName = "zone-endpoints"
		const appName = "zone-endpoints"
		const serviceName = "zone-endpoints"
		const port = 8080

		applyDeploymentOnly(t, ns, deployName, appName, 2)
		TestHelper.WaitRollout(t, map[string]testutil.DeploySpec{
			deployName: {
				Namespace: ns,
				Replicas:  2,
			},
		})

		pods := waitForPods(t, ns, "app="+appName, 2, 60*time.Second)
		sort.Slice(pods, func(i, j int) bool {
			return pods[i].Name < pods[j].Name
		})

		// Use the first pod's node as the source node and ensure it has a zone
		// label; if absent, add one for this test and clean it up afterwards.
		nodeName := pods[0].NodeName
		localZone, labelCleanup := ensureNodeZoneLabel(t, nodeName)
		if labelCleanup != nil {
			t.Cleanup(labelCleanup)
		}
		remoteZone := localZone + "-remote"

		applySelectorlessServiceAndEndpointSlice(t, ns, serviceName, port, []endpointSliceEntry{
			{Address: pods[0].IP, ZoneHint: localZone},
			{Address: pods[1].IP, ZoneHint: remoteZone},
		})

		authority := serviceAuthority(serviceName, ns)
		client, conn := newDestinationClient(t, ctx)
		defer conn.Close()

		contextToken := fmt.Sprintf(`{"ns":"%s","nodeName":"%s"}`+"\n", ns, nodeName)
		filteredSub := newSubscriber(t, client, authority, contextToken)
		allSub := newSubscriber(t, client, authority, "")

		expectedFiltered := map[string]struct{}{fmt.Sprintf("%s:%d", pods[0].IP, port): {}}
		expectedAll := map[string]struct{}{
			fmt.Sprintf("%s:%d", pods[0].IP, port): {},
			fmt.Sprintf("%s:%d", pods[1].IP, port): {},
		}

		// Subscriber with a source-node context token should see only local-zone
		// endpoints; subscriber without token should receive all endpoints.
		filteredSub.WaitForExact(t, expectedFiltered, 60*time.Second)
		allSub.WaitForExact(t, expectedAll, 60*time.Second)
	})
}

type subscriber struct {
	cancel    context.CancelFunc
	events    chan struct{}
	errCh     chan error
	mu        sync.RWMutex
	endpoints map[string]struct{}
}

func newSubscriber(t *testing.T, client destinationpb.DestinationClient, authority, contextToken string) *subscriber {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.Get(ctx, &destinationpb.GetDestination{
		Scheme:       "k8s",
		Path:         authority,
		ContextToken: contextToken,
	})
	if err != nil {
		cancel()
		testutil.AnnotatedFatalf(t, "failed to create destination stream", "failed to create destination stream for %s: %v", authority, err)
	}

	s := &subscriber{
		cancel:    cancel,
		events:    make(chan struct{}, 1),
		errCh:     make(chan error, 1),
		endpoints: map[string]struct{}{},
	}
	go s.readUpdates(stream)
	t.Cleanup(s.cancel)
	return s
}

func (s *subscriber) readUpdates(stream destinationpb.Destination_GetClient) {
	for {
		update, err := stream.Recv()
		if err != nil {
			// A canceled stream context is expected during test cleanup.
			if errors.Is(stream.Context().Err(), context.Canceled) || errors.Is(err, context.Canceled) {
				return
			}
			// EOF without cancellation means the server closed the stream
			// unexpectedly before the test reached a terminal assertion.
			if errors.Is(err, io.EOF) {
				err = fmt.Errorf("destination stream closed unexpectedly: %w", err)
			}
			select {
			case s.errCh <- err:
			default:
			}
			return
		}

		s.mu.Lock()
		if add := update.GetAdd(); add != nil {
			for _, addr := range add.GetAddrs() {
				addrStr := pkgaddr.ProxyAddressToString(addr.Addr)
				s.endpoints[addrStr] = struct{}{}
			}
		}
		if remove := update.GetRemove(); remove != nil {
			for _, addr := range remove.GetAddrs() {
				addrStr := pkgaddr.ProxyAddressToString(addr)
				delete(s.endpoints, addrStr)
			}
		}
		s.mu.Unlock()

		select {
		case s.events <- struct{}{}:
		default:
		}
	}
}

func (s *subscriber) WaitForExact(t *testing.T, expected map[string]struct{}, timeout time.Duration) {
	t.Helper()

	// Fast path for already-converged state.
	if exactSetEqual(expected, s.snapshot()) {
		return
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case err := <-s.errCh:
			testutil.AnnotatedFatalf(t, "destination stream failed", "destination stream failed: %v", err)
		case <-s.events:
			// The stream can emit multiple incremental updates; keep waiting until
			// the complete expected set is observed.
			if exactSetEqual(expected, s.snapshot()) {
				return
			}
		case <-timer.C:
			actual := s.snapshot()
			testutil.AnnotatedFatalf(t,
				"timed out waiting for destination state",
				"timed out waiting for destination state\nexpected: %v\nactual:   %v",
				sortedSet(expected), sortedSet(actual),
			)
		}
	}
}

func (s *subscriber) snapshot() map[string]struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	copy := make(map[string]struct{}, len(s.endpoints))
	for endpoint := range s.endpoints {
		copy[endpoint] = struct{}{}
	}
	return copy
}

func newDestinationClient(t *testing.T, ctx context.Context) (destinationpb.DestinationClient, io.Closer) {
	t.Helper()

	k8sAPI, err := k8s.NewAPI("", "", "", []string{}, 0)
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to create kubernetes api", "failed to create kubernetes api: %v", err)
	}
	client, conn, err := destination.NewExternalClient(ctx, TestHelper.GetLinkerdNamespace(), k8sAPI, "")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to create destination client", "failed to create destination client: %v", err)
	}
	return client, conn
}

func serviceAuthority(serviceName, namespace string) string {
	return fmt.Sprintf("%s.%s.svc.%s:8080", serviceName, namespace, TestHelper.GetClusterDomain())
}

func applyDeploymentAndService(t *testing.T, namespace, deploymentName, serviceName string, replicas int) {
	t.Helper()

	manifest := fmt.Sprintf(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  labels:
    app: %s
spec:
  replicas: %d
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      annotations:
        linkerd.io/inject: enabled
      labels:
        app: %s
    spec:
      containers:
      - name: app
        image: nginx:alpine
---
apiVersion: v1
kind: Service
metadata:
  name: %s
spec:
  ports:
  - name: service
    port: 8080
  selector:
    app: %s
`, deploymentName, deploymentName, replicas, deploymentName, deploymentName, serviceName, deploymentName)

	if _, err := TestHelper.KubectlApply(manifest, namespace); err != nil {
		testutil.AnnotatedFatalf(t, "failed to apply deployment/service", "failed to apply deployment/service for %s: %v", deploymentName, err)
	}
}

func applyDeploymentOnly(t *testing.T, namespace, deploymentName, app string, replicas int) {
	t.Helper()

	manifest := fmt.Sprintf(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  labels:
    app: %s
spec:
  replicas: %d
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      annotations:
        linkerd.io/inject: enabled
      labels:
        app: %s
    spec:
      containers:
      - name: app
        image: nginx:alpine
`, deploymentName, app, replicas, app, app)

	if _, err := TestHelper.KubectlApply(manifest, namespace); err != nil {
		testutil.AnnotatedFatalf(t, "failed to apply deployment", "failed to apply deployment for %s: %v", deploymentName, err)
	}
}

type endpointSliceEntry struct {
	Address  string
	ZoneHint string
}

func applySelectorlessServiceAndEndpointSlice(t *testing.T, namespace, serviceName string, port int, entries []endpointSliceEntry) {
	t.Helper()

	endpointsYAML := make([]string, 0, len(entries))
	for _, entry := range entries {
		endpointsYAML = append(endpointsYAML, fmt.Sprintf(`
  - addresses:
    - %s
    conditions:
      ready: true
    hints:
      forZones:
      - name: %s`, entry.Address, entry.ZoneHint))
	}

	manifest := fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  name: %s
spec:
  ports:
  - name: service
    port: %d
---
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: %s-manual
  labels:
    kubernetes.io/service-name: %s
    endpointslice.kubernetes.io/managed-by: destination-api-test
addressType: IPv4
ports:
- name: service
  protocol: TCP
  port: %d
endpoints:%s
`, serviceName, port, serviceName, serviceName, port, strings.Join(endpointsYAML, ""))

	if _, err := TestHelper.KubectlApply(manifest, namespace); err != nil {
		testutil.AnnotatedFatalf(t, "failed to apply service/endpointslice", "failed to apply service/endpointslice for %s: %v", serviceName, err)
	}
}

type podEntry struct {
	Name     string `json:"name"`
	IP       string `json:"ip"`
	NodeName string `json:"nodeName"`
}

type podsResponse struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Spec struct {
			NodeName string `json:"nodeName"`
		} `json:"spec"`
		Status struct {
			PodIP string `json:"podIP"`
		} `json:"status"`
	} `json:"items"`
}

func waitForPods(t *testing.T, namespace, labelSelector string, minCount int, timeout time.Duration) []podEntry {
	t.Helper()

	var pods []podEntry
	err := testutil.RetryFor(timeout, func() error {
		out, err := TestHelper.Kubectl("", "-n", namespace, "get", "pods", "-l", labelSelector, "-o", "json")
		if err != nil {
			return err
		}

		var response podsResponse
		if err := json.Unmarshal([]byte(out), &response); err != nil {
			return err
		}

		pods = pods[:0]
		for _, item := range response.Items {
			if item.Status.PodIP == "" || item.Spec.NodeName == "" {
				continue
			}
			pods = append(pods, podEntry{
				Name:     item.Metadata.Name,
				IP:       item.Status.PodIP,
				NodeName: item.Spec.NodeName,
			})
		}

		if len(pods) < minCount {
			return fmt.Errorf("expected at least %d ready pods, got %d", minCount, len(pods))
		}
		return nil
	})
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed waiting for pods", "failed waiting for pods with selector %s: %v", labelSelector, err)
	}

	return append([]podEntry(nil), pods...)
}

type nodeListResponse struct {
	Items []struct {
		Metadata struct {
			Name   string            `json:"name"`
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
	} `json:"items"`
}

func ensureNodeZoneLabel(t *testing.T, nodeName string) (string, func()) {
	t.Helper()

	out, err := TestHelper.Kubectl("", "get", "nodes", "-o", "json")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to get nodes", "failed to get nodes: %v", err)
	}

	var nodes nodeListResponse
	if err := json.Unmarshal([]byte(out), &nodes); err != nil {
		testutil.AnnotatedFatalf(t, "failed to decode nodes", "failed to decode nodes: %v", err)
	}

	for _, item := range nodes.Items {
		if item.Metadata.Name != nodeName {
			continue
		}

		if zone, ok := item.Metadata.Labels["topology.kubernetes.io/zone"]; ok && zone != "" {
			return zone, nil
		}

		zone := "destination-api-test-zone"
		if _, err := TestHelper.Kubectl("", "label", "node", nodeName, fmt.Sprintf("topology.kubernetes.io/zone=%s", zone), "--overwrite"); err != nil {
			testutil.AnnotatedFatalf(t, "failed to label node zone", "failed to label node %s: %v", nodeName, err)
		}
		cleanup := func() {
			_, _ = TestHelper.Kubectl("", "label", "node", nodeName, "topology.kubernetes.io/zone-")
		}
		return zone, cleanup
	}

	testutil.AnnotatedFatalf(t, "node not found", "node %s not found in cluster", nodeName)
	return "", nil
}

type endpointsResponse struct {
	Subsets []struct {
		Addresses []struct {
			IP string `json:"ip"`
		} `json:"addresses"`
		Ports []struct {
			Port int `json:"port"`
		} `json:"ports"`
	} `json:"subsets"`
}

func waitForServiceEndpoints(t *testing.T, namespace, service string, timeout time.Duration) map[string]struct{} {
	t.Helper()

	var endpoints map[string]struct{}
	// Poll Endpoints until at least one address is present. This avoids races
	// where the service exists but endpoint publication is still in flight.
	err := testutil.RetryFor(timeout, func() error {
		out, err := TestHelper.Kubectl("", "-n", namespace, "get", "endpoints", service, "-o", "json")
		if err != nil {
			return err
		}

		set, err := parseEndpointSet(out)
		if err != nil {
			return err
		}
		if len(set) == 0 {
			return fmt.Errorf("no endpoints available for service %s", service)
		}
		endpoints = set
		return nil
	})
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed waiting for service endpoints", "failed waiting for endpoints for service %s: %v", service, err)
	}

	return endpoints
}

func waitForChangedServiceEndpoints(t *testing.T, namespace, service string, old map[string]struct{}, timeout time.Duration) map[string]struct{} {
	t.Helper()

	var updated map[string]struct{}
	// Poll until the endpoint set differs from the old set, accounting for
	// eventual consistency during rollouts.
	err := testutil.RetryFor(timeout, func() error {
		set := waitForServiceEndpoints(t, namespace, service, timeout)
		if exactSetEqual(old, set) {
			return fmt.Errorf("service endpoints have not changed yet")
		}
		updated = set
		return nil
	})
	if err != nil {
		testutil.AnnotatedFatalf(t,
			"service endpoints did not change",
			"service %s endpoints did not change within timeout; old=%v new=%v err=%v",
			service,
			sortedSet(old),
			sortedSet(updated),
			err,
		)
	}

	return updated
}

func parseEndpointSet(raw string) (map[string]struct{}, error) {
	var response endpointsResponse
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		return nil, err
	}

	set := map[string]struct{}{}
	for _, subset := range response.Subsets {
		for _, addr := range subset.Addresses {
			for _, port := range subset.Ports {
				set[fmt.Sprintf("%s:%d", addr.IP, port.Port)] = struct{}{}
			}
		}
	}
	return set, nil
}

func exactSetEqual(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for endpoint := range a {
		if _, ok := b[endpoint]; !ok {
			return false
		}
	}
	return true
}

func sortedSet(set map[string]struct{}) []string {
	values := make([]string, 0, len(set))
	for endpoint := range set {
		values = append(values, endpoint)
	}
	sort.Strings(values)
	return values
}
