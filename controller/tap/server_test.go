package tap

import (
	"context"
	"testing"
	"time"

	public "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/controller/k8s"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
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
			tapExpected{
				msg:    "rpc error: code = InvalidArgument desc = TapByResource received nil target ResourceSelection: {Target:<nil> Match:<nil> MaxRps:0}",
				k8sRes: []string{},
				req:    public.TapByResourceRequest{},
			},
			tapExpected{
				msg: "rpc error: code = Unimplemented desc = unexpected match specified: any:<> ",
				k8sRes: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
  annotations:
    conduit.io/proxy-version: testinjectversion
status:
  phase: Running
`,
				},
				req: public.TapByResourceRequest{
					Target: &public.ResourceSelection{
						Resource: &public.Resource{
							Namespace: "emojivoto",
							Type:      "pods",
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
			tapExpected{
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
			tapExpected{
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
    conduit.io/proxy-version: testinjectversion
status:
  phase: Running
`,
				},
				req: public.TapByResourceRequest{
					Target: &public.ResourceSelection{
						Resource: &public.Resource{
							Namespace: "emojivoto",
							Type:      "pods",
							Name:      "emojivoto-meshed-not-found",
						},
					},
				},
			},
			tapExpected{
				msg: "rpc error: code = NotFound desc = no pods found for ResourceSelection: {Resource:namespace:\"emojivoto\" type:\"pods\" name:\"emojivoto-meshed\"  LabelSelector:}",
				k8sRes: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
  annotations:
    conduit.io/proxy-version: testinjectversion
status:
  phase: Finished
`,
				},
				req: public.TapByResourceRequest{
					Target: &public.ResourceSelection{
						Resource: &public.Resource{
							Namespace: "emojivoto",
							Type:      "pods",
							Name:      "emojivoto-meshed",
						},
					},
				},
			},
			tapExpected{
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
  annotations:
    conduit.io/proxy-version: testinjectversion
status:
  phase: Running
`,
				},
				req: public.TapByResourceRequest{
					Target: &public.ResourceSelection{
						Resource: &public.Resource{
							Namespace: "emojivoto",
							Type:      "pods",
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
			k8sObjs := []runtime.Object{}
			for _, res := range exp.k8sRes {
				decode := scheme.Codecs.UniversalDeserializer().Decode
				obj, _, err := decode([]byte(res), nil, nil)
				if err != nil {
					t.Fatalf("could not decode yml: %s", err)
				}
				k8sObjs = append(k8sObjs, obj)
			}

			clientSet := fake.NewSimpleClientset(k8sObjs...)
			lister := k8s.NewLister(clientSet)

			server, listener, err := NewServer("localhost:0", 0, lister)
			if err != nil {
				t.Fatalf("NewServer error: %s", err)
			}

			go func() { server.Serve(listener) }()
			defer server.GracefulStop()

			err = lister.Sync()
			if err != nil {
				t.Fatalf("timed out wait for caches to sync: %s", err)
			}

			client, conn, err := NewClient(listener.Addr().String())
			if err != nil {
				t.Fatalf("NewClient error: %v", err)
			}
			defer conn.Close()

			// TODO: mock out the underlying grpc tap events, rather than waiting an
			// arbitrary time for request to timeout.
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			tapClient, err := client.TapByResource(ctx, &exp.req)
			if err != nil {
				t.Fatalf("TapByResource failed: %v", err)
			}

			_, err = tapClient.Recv()
			if err.Error() != exp.msg && (!exp.eofOk || err.Error() != "EOF") {
				t.Fatalf("Expected error to be [%s], but was [%s]. eofOk: %v", exp.msg, err, exp.eofOk)
			}
		}
	})
}
