package tap

import (
	"context"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/controller/k8s"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
)

type tapExpected struct {
	msg    string
	k8sRes []string
	req    public.TapByResourceRequest
	eofOk  bool
}

func TestTapByResource(t *testing.T) {
	t.Run("Returns expected response", func(t *testing.T) {
		expectations := []tapExpected{
			{
				msg:    "rpc error: code = InvalidArgument desc = TapByResource received nil target ResourceSelection",
				k8sRes: []string{},
				req:    public.TapByResourceRequest{},
			},
			{
				msg: "rpc error: code = Unimplemented desc = unexpected match specified: any:<> ",
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
    linkerd.io/proxy-version: testinjectversion
status:
  phase: Running
`,
				},
				req: public.TapByResourceRequest{
					Target: &public.ResourceSelection{
						Resource: &public.Resource{
							Namespace: "emojivoto",
							Type:      pkgK8s.Pod,
							Name:      "emojivoto-meshed",
						},
					},
					Match: &public.TapByResourceRequest_Match{
						Match: &public.TapByResourceRequest_Match_Any{
							Any: &public.TapByResourceRequest_Match_Seq{},
						},
					},
				},
			},
			{
				msg: "rpc error: code = NotFound desc = no pods found for pod/emojivoto-not-meshed",
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
`,
				},
				req: public.TapByResourceRequest{
					Target: &public.ResourceSelection{
						Resource: &public.Resource{
							Namespace: "emojivoto",
							Type:      pkgK8s.Pod,
							Name:      "emojivoto-not-meshed",
						},
					},
				},
			},
			{
				msg:    "rpc error: code = Unimplemented desc = unimplemented resource type: bad-type",
				k8sRes: []string{},
				req: public.TapByResourceRequest{
					Target: &public.ResourceSelection{
						Resource: &public.Resource{
							Namespace: "emojivoto",
							Type:      "bad-type",
							Name:      "emojivoto-meshed-not-found",
						},
					},
				},
			},
			{
				msg: "rpc error: code = NotFound desc = pod \"emojivoto-meshed-not-found\" not found",
				k8sRes: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
  annotations:
    linkerd.io/proxy-version: testinjectversion
status:
  phase: Running
`,
				},
				req: public.TapByResourceRequest{
					Target: &public.ResourceSelection{
						Resource: &public.Resource{
							Namespace: "emojivoto",
							Type:      pkgK8s.Pod,
							Name:      "emojivoto-meshed-not-found",
						},
					},
				},
			},
			{
				msg: "rpc error: code = NotFound desc = no pods found for pod/emojivoto-meshed",
				k8sRes: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
  annotations:
    linkerd.io/proxy-version: testinjectversion
status:
  phase: Finished
`,
				},
				req: public.TapByResourceRequest{
					Target: &public.ResourceSelection{
						Resource: &public.Resource{
							Namespace: "emojivoto",
							Type:      pkgK8s.Pod,
							Name:      "emojivoto-meshed",
						},
					},
				},
			},
			{
				msg: "rpc error: code = NotFound desc = no pods found for pod/emojivoto-meshed-tap-disabled",
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
    config.linkerd.io/disable-tap: "true"
    linkerd.io/proxy-version: testinjectversion
status:
  phase: Running
`,
				},
				req: public.TapByResourceRequest{
					Target: &public.ResourceSelection{
						Resource: &public.Resource{
							Namespace: "emojivoto",
							Type:      pkgK8s.Pod,
							Name:      "emojivoto-meshed-tap-disabled",
						},
					},
					Match: &public.TapByResourceRequest_Match{
						Match: &public.TapByResourceRequest_Match_All{
							All: &public.TapByResourceRequest_Match_Seq{},
						},
					},
				},
			},
			{
				// indicates we will accept EOF, in addition to the deadline exceeded message
				eofOk: true,
				// success, underlying tap events tested in http_server_test.go
				msg: "rpc error: code = DeadlineExceeded desc = context deadline exceeded",
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
    linkerd.io/proxy-version: testinjectversion
status:
  phase: Running
`,
				},
				req: public.TapByResourceRequest{
					Target: &public.ResourceSelection{
						Resource: &public.Resource{
							Namespace: "emojivoto",
							Type:      pkgK8s.Pod,
							Name:      "emojivoto-meshed",
						},
					},
					Match: &public.TapByResourceRequest_Match{
						Match: &public.TapByResourceRequest_Match_All{
							All: &public.TapByResourceRequest_Match_Seq{},
						},
					},
				},
			},
		}

		for _, exp := range expectations {
			k8sAPI, err := k8s.NewFakeAPI(exp.k8sRes...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			server, listener, err := NewServer("localhost:0", 0, "controller-ns", k8sAPI)
			if err != nil {
				t.Fatalf("NewServer error: %s", err)
			}

			go func() { server.Serve(listener) }()
			defer server.GracefulStop()

			k8sAPI.Sync()

			client, conn, err := NewClient(listener.Addr().String())
			if err != nil {
				t.Fatalf("NewClient error: %v", err)
			}
			defer conn.Close()

			// TODO: mock out the underlying grpc tap events, rather than waiting an
			// arbitrary time for request to timeout.
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			tapByResourceClient, err := client.TapByResource(ctx, &exp.req)
			if err != nil {
				t.Fatalf("TapByResource failed: %v", err)
			}

			_, err = tapByResourceClient.Recv()
			if err.Error() != exp.msg && (!exp.eofOk || err.Error() != "EOF") {
				t.Fatalf("Expected error to be [%s], but was [%s]. eofOk: %v", exp.msg, err, exp.eofOk)
			}
		}
	})
}
