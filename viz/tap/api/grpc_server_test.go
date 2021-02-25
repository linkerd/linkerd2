package api

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"testing"

	proxy "github.com/linkerd/linkerd2-proxy-api/go/tap"
	"github.com/linkerd/linkerd2/controller/api/util"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/addr"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	metricsPb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	tapPb "github.com/linkerd/linkerd2/viz/tap/gen/tap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type tapExpected struct {
	err       error
	k8sRes    []string
	req       *tapPb.TapByResourceRequest
	requireID string
}

// mockTapByResourceServer satisfies controller.tap.Tap_TapByResourceServer
type mockTapByResourceServer struct {
	util.MockServerStream
}

func (m *mockTapByResourceServer) Send(event *tapPb.TapEvent) error {
	return nil
}

// mockProxyTapServer satisfies proxy.tap.TapServer
type mockProxyTapServer struct {
	mockControllerServer mockTapByResourceServer // for cancellation
	ctx                  context.Context
}

func (m *mockProxyTapServer) Observe(req *proxy.ObserveRequest, obsSrv proxy.Tap_ObserveServer) error {
	m.ctx = obsSrv.Context()
	m.mockControllerServer.Cancel()
	return nil
}

func TestTapByResource(t *testing.T) {
	expectations := []tapExpected{
		{
			err:    status.Error(codes.InvalidArgument, "TapByResource received nil target ResourceSelection"),
			k8sRes: []string{},
			req:    &tapPb.TapByResourceRequest{},
		},
		{
			err: status.Errorf(codes.Unimplemented, "unexpected match specified: any:{}"),
			k8sRes: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: controller-ns
  annotations:
    viz.linkerd.io/tap-enabled: "true"
    linkerd.io/proxy-version: testinjectversion
status:
  phase: Running
  podIP: 127.0.0.1
`,
			},
			req: &tapPb.TapByResourceRequest{
				Target: &metricsPb.ResourceSelection{
					Resource: &metricsPb.Resource{
						Namespace: "emojivoto",
						Type:      pkgK8s.Pod,
						Name:      "emojivoto-meshed",
					},
				},
				Match: &tapPb.TapByResourceRequest_Match{
					Match: &tapPb.TapByResourceRequest_Match_Any{
						Any: &tapPb.TapByResourceRequest_Match_Seq{},
					},
				},
			},
		},
		{
			err: status.Errorf(codes.NotFound, "no pods to tap for pod/emojivoto-not-meshed\n"),
			k8sRes: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-not-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
status:
  phase: Running
  podIP: 127.0.0.1
`,
			},
			req: &tapPb.TapByResourceRequest{
				Target: &metricsPb.ResourceSelection{
					Resource: &metricsPb.Resource{
						Namespace: "emojivoto",
						Type:      pkgK8s.Pod,
						Name:      "emojivoto-not-meshed",
					},
				},
			},
		},
		{
			err:    status.Errorf(codes.Unimplemented, "unimplemented resource type: bad-type"),
			k8sRes: []string{},
			req: &tapPb.TapByResourceRequest{
				Target: &metricsPb.ResourceSelection{
					Resource: &metricsPb.Resource{
						Namespace: "emojivoto",
						Type:      "bad-type",
						Name:      "emojivoto-meshed-not-found",
					},
				},
			},
		},
		{
			err: status.Errorf(codes.NotFound, "pod \"emojivoto-meshed-not-found\" not found"),
			k8sRes: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
  annotations:
    viz.linkerd.io/tap-enabled: "true"
    linkerd.io/proxy-version: testinjectversion
status:
  phase: Running
  podIP: 127.0.0.1
`,
			},
			req: &tapPb.TapByResourceRequest{
				Target: &metricsPb.ResourceSelection{
					Resource: &metricsPb.Resource{
						Namespace: "emojivoto",
						Type:      pkgK8s.Pod,
						Name:      "emojivoto-meshed-not-found",
					},
				},
			},
		},
		{
			err: status.Errorf(codes.NotFound, "no pods to tap for pod/emojivoto-meshed\n"),
			k8sRes: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
  annotations:
    viz.linkerd.io/tap-enabled: "true"
    linkerd.io/proxy-version: testinjectversion
status:
  phase: Finished
  podIP: 127.0.0.1
`,
			},
			req: &tapPb.TapByResourceRequest{
				Target: &metricsPb.ResourceSelection{
					Resource: &metricsPb.Resource{
						Namespace: "emojivoto",
						Type:      pkgK8s.Pod,
						Name:      "emojivoto-meshed",
					},
				},
			},
		},
		{
			err: status.Errorf(codes.NotFound, `no pods to tap for pod/emojivoto-meshed-tap-disabled
1 pods found with tap disabled via the viz.linkerd.io/disable-tap annotation:
	* emojivoto-meshed-tap-disabled
remove this annotation to make these pods valid tap targets
`),
			k8sRes: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed-tap-disabled
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: controller-ns
  annotations:
    viz.linkerd.io/disable-tap: "true"
    linkerd.io/proxy-version: testinjectversion
status:
  phase: Running
  podIP: 127.0.0.1
    `,
			},
			req: &tapPb.TapByResourceRequest{
				Target: &metricsPb.ResourceSelection{
					Resource: &metricsPb.Resource{
						Namespace: "emojivoto",
						Type:      pkgK8s.Pod,
						Name:      "emojivoto-meshed-tap-disabled",
					},
				},
				Match: &tapPb.TapByResourceRequest_Match{
					Match: &tapPb.TapByResourceRequest_Match_All{
						All: &tapPb.TapByResourceRequest_Match_Seq{},
					},
				},
			},
		},
		{
			err: status.Errorf(codes.NotFound, `no pods to tap for pod/emojivoto-meshed-tap-not-enabled
1 pods found with tap not enabled:
	* emojivoto-meshed-tap-not-enabled
restart these pods to enable tap and make them valid tap targets
`),
			k8sRes: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed-tap-not-enabled
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: controller-ns
  annotations:
    linkerd.io/proxy-version: testinjectversion
status:
  phase: Running
  podIP: 127.0.0.1
    `,
			},
			req: &tapPb.TapByResourceRequest{
				Target: &metricsPb.ResourceSelection{
					Resource: &metricsPb.Resource{
						Namespace: "emojivoto",
						Type:      pkgK8s.Pod,
						Name:      "emojivoto-meshed-tap-not-enabled",
					},
				},
				Match: &tapPb.TapByResourceRequest_Match{
					Match: &tapPb.TapByResourceRequest_Match_All{
						All: &tapPb.TapByResourceRequest_Match_Seq{},
					},
				},
			},
		},
		{
			// success, underlying tap events tested in http_server_test.go
			err: nil,
			k8sRes: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: controller-ns
  annotations:
    viz.linkerd.io/tap-enabled: "true"
    linkerd.io/proxy-version: testinjectversion
status:
  phase: Running
  podIP: 127.0.0.1
`,
			},
			req: &tapPb.TapByResourceRequest{
				Target: &metricsPb.ResourceSelection{
					Resource: &metricsPb.Resource{
						Namespace: "emojivoto",
						Type:      pkgK8s.Pod,
						Name:      "emojivoto-meshed",
					},
				},
				Match: &tapPb.TapByResourceRequest_Match{
					Match: &tapPb.TapByResourceRequest_Match_All{
						All: &tapPb.TapByResourceRequest_Match_Seq{},
					},
				},
			},
			requireID: ".emojivoto.serviceaccount.identity.controller-ns.cluster.local",
		},
		{
			err: nil,
			k8sRes: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: controller-ns
  annotations:
    viz.linkerd.io/tap-enabled: "true"
    linkerd.io/proxy-version: testinjectversion
spec:
  serviceAccountName: emojivoto-meshed-sa
status:
  phase: Running
  podIP: 127.0.0.1
`,
			},
			req: &tapPb.TapByResourceRequest{
				Target: &metricsPb.ResourceSelection{
					Resource: &metricsPb.Resource{
						Namespace: "emojivoto",
						Type:      pkgK8s.Pod,
						Name:      "emojivoto-meshed",
					},
				},
				Match: &tapPb.TapByResourceRequest_Match{
					Match: &tapPb.TapByResourceRequest_Match_All{
						All: &tapPb.TapByResourceRequest_Match_Seq{},
					},
				},
			},
			requireID: "emojivoto-meshed-sa.emojivoto.serviceaccount.identity.controller-ns.cluster.local",
		},
		{
			err: nil,
			k8sRes: []string{`
apiVersion: v1
kind: Namespace
metadata:
  name: emojivoto
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: controller-ns
  annotations:
    viz.linkerd.io/tap-enabled: "true"
    linkerd.io/proxy-version: testinjectversion
spec:
  serviceAccountName: emojivoto-meshed-sa
status:
  phase: Running
  podIP: 127.0.0.1
`,
			},
			req: &tapPb.TapByResourceRequest{
				Target: &metricsPb.ResourceSelection{
					Resource: &metricsPb.Resource{
						Namespace: "",
						Type:      pkgK8s.Namespace,
						Name:      "emojivoto",
					},
				},
				Match: &tapPb.TapByResourceRequest_Match{
					Match: &tapPb.TapByResourceRequest_Match_All{
						All: &tapPb.TapByResourceRequest_Match_Seq{},
					},
				},
			},
			requireID: "emojivoto-meshed-sa.emojivoto.serviceaccount.identity.controller-ns.cluster.local",
		},
	}

	for i, exp := range expectations {
		exp := exp // pin
		t.Run(fmt.Sprintf("%d: Returns expected response", i), func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(exp.k8sRes...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			stream := mockTapByResourceServer{
				MockServerStream: util.NewMockServerStream(),
			}

			s := grpc.NewServer()

			mockProxyTapServer := mockProxyTapServer{
				mockControllerServer: stream,
			}
			proxy.RegisterTapServer(s, &mockProxyTapServer)

			lis, err := net.Listen("tcp", ":0")
			if err != nil {
				t.Fatalf("Failed to listen")
			}

			// TODO: mock out the underlying grpc tap events
			errChan := make(chan error, 1)
			go func() {
				errChan <- s.Serve(lis)
			}()

			defer func() {
				if err := <-errChan; err != nil {
					t.Fatalf("Failed to serve on %+v: %s", lis, err)
				}
			}()

			defer s.GracefulStop()

			_, port, err := net.SplitHostPort(lis.Addr().String())
			if err != nil {
				t.Fatal(err.Error())
			}

			tapPort, err := strconv.ParseUint(port, 10, 32)
			if err != nil {
				t.Fatalf("Invalid port: %s", port)
			}

			fakeGrpcServer := newGRPCTapServer(uint(tapPort), "controller-ns", "cluster.local", k8sAPI)

			k8sAPI.Sync(nil)

			err = fakeGrpcServer.TapByResource(exp.req, &stream)
			if err != nil || exp.err != nil {
				code := status.Code(err)
				expCode := status.Code(exp.err)
				if code != expCode {
					t.Fatalf("TapByResource returned unexpected error code: [%s], expected: [%s]", code, expCode)
				}
				if err.Error() != exp.err.Error() {
					t.Fatalf("TapByResource returned unexpected error message: [%s], expected: [%s]", err.Error(), exp.err.Error())
				}
			}

			if exp.requireID != "" {
				md, ok := metadata.FromIncomingContext(mockProxyTapServer.ctx)
				if !ok {
					t.Fatalf("FromIncomingContext failed given: %+v", mockProxyTapServer.ctx)
				}

				if !reflect.DeepEqual(md.Get(pkgK8s.RequireIDHeader), []string{exp.requireID}) {
					t.Fatalf("Unexpected l5d-require-id header [%+v] expected [%+v]", md.Get(pkgK8s.RequireIDHeader), []string{exp.requireID})
				}
			}

		})
	}
}

func TestHydrateIPLabels(t *testing.T) {
	expectations := []struct {
		k8sRes      []string
		requestedIP string
		labels      map[string]string
	}{
		{
			// Requested IP that doesn't match node or any pod
			k8sRes: []string{`
apiVersion: v1
kind: Node
metadata:
  name: node1
status:
  addresses:
  - address: 1.2.3.4
    type: InternalIP
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
status:
  phase: Running
  podIP: 5.6.7.8
`,
			},
			requestedIP: "10.20.30.40",
			labels:      map[string]string{},
		},
		{
			// Requested IP that matches node only
			k8sRes: []string{`
apiVersion: v1
kind: Node
metadata:
  name: node1
status:
  addresses:
  - address: 1.2.3.4
    type: InternalIP
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
status:
  phase: Running
  podIP: 5.6.7.8
`,
			},
			requestedIP: "1.2.3.4",
			labels:      map[string]string{"node": "node1"},
		},
		{
			// Requested IP that matches node and pod
			k8sRes: []string{`
apiVersion: v1
kind: Node
metadata:
  name: node1
status:
  addresses:
  - address: 1.2.3.4
    type: InternalIP
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
status:
  phase: Running
  podIP: 1.2.3.4
`,
			},
			requestedIP: "1.2.3.4",
			labels:      map[string]string{"node": "node1"},
		},
		{
			// Requested IP that doesn't match node and matches exactly one pod
			k8sRes: []string{`
apiVersion: v1
kind: Node
metadata:
  name: node1
status:
  addresses:
  - address: 1.2.3.4
    type: InternalIP
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
status:
  phase: Running
  podIP: 5.6.7.8
`,
			},
			requestedIP: "5.6.7.8",
			labels: map[string]string{
				"namespace":      "emojivoto",
				"pod":            "emojivoto-meshed",
				"serviceaccount": "default",
			},
		},
		{
			// Requested IP that doesn't match node and matches exactly one running pod and one finished pod
			k8sRes: []string{`
apiVersion: v1
kind: Node
metadata:
  name: node1
status:
  addresses:
  - address: 1.2.3.4
    type: InternalIP
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
status:
  phase: Running
  podIP: 5.6.7.8
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed-2
  namespace: emojivoto
  labels:
    app: emoji-svc
status:
  phase: Finished
  podIP: 5.6.7.8
`,
			},
			requestedIP: "5.6.7.8",
			labels: map[string]string{
				"namespace":      "emojivoto",
				"pod":            "emojivoto-meshed",
				"serviceaccount": "default",
			},
		},
		{
			// Requested IP that doesn't match node and matches two running pods
			k8sRes: []string{`
apiVersion: v1
kind: Node
metadata:
  name: node1
status:
  addresses:
  - address: 1.2.3.4
    type: InternalIP
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
status:
  phase: Running
  podIP: 5.6.7.8
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed-2
  namespace: emojivoto
  labels:
    app: emoji-svc
status:
  phase: Running
  podIP: 5.6.7.8
`,
			},
			requestedIP: "5.6.7.8",
			labels:      map[string]string{},
		},
	}

	ctx := context.Background()
	for i, exp := range expectations {
		exp := exp // pin
		t.Run(fmt.Sprintf("%d: Returns expected response", i), func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(exp.k8sRes...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}
			s := NewGrpcTapServer(4190, "controller-ns", "cluster.local", k8sAPI)
			k8sAPI.Sync(nil)

			labels := make(map[string]string)
			ip, err := addr.ParsePublicIPV4(exp.requestedIP)
			if err != nil {
				t.Fatalf("Error parsing IP %s: %s", exp.requestedIP, err)
			}
			s.hydrateIPLabels(ctx, ip, labels)
			if !reflect.DeepEqual(labels, exp.labels) {
				t.Fatalf("Unexpected labels: [%#v], expected: [%#v]", labels, exp.labels)
			}
		})
	}
}
