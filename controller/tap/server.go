package tap

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	common "github.com/runconduit/conduit/controller/gen/common"
	pb "github.com/runconduit/conduit/controller/gen/controller/tap"
	proxy "github.com/runconduit/conduit/controller/gen/proxy/tap"
	public "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/controller/k8s"
	"github.com/runconduit/conduit/controller/util"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"k8s.io/client-go/pkg/api/v1"
)

var tapInterval = 10 * time.Second

type (
	server struct {
		tapPort uint
		// We use the Kubernetes API to find the IP addresses of pods to tap
		replicaSets *k8s.ReplicaSetStore
		pods        *k8s.PodIndex
	}
)

func (s *server) Tap(req *public.TapRequest, stream pb.Tap_TapServer) error {

	// TODO: Allow a configurable aperture A.
	//       If the target contains more than A pods, select A of them at random.
	var pods []*v1.Pod
	var targetName string
	switch target := req.Target.(type) {
	case *public.TapRequest_Pod:
		targetName = target.Pod
		pod, err := s.pods.GetPod(target.Pod)
		if err != nil {
			return err
		}
		pods = []*v1.Pod{pod}
	case *public.TapRequest_Deployment:
		targetName = target.Deployment
		var err error
		pods, err = (*s.pods).GetPodsByIndex(target.Deployment)
		if err != nil {
			return err
		}
	}

	log.Printf("Tapping %d pods for target %s", len(pods), targetName)

	events := make(chan *common.TapEvent)

	go func() { // Stop sending back events if the request is cancelled
		<-stream.Context().Done()
		close(events)
	}()

	// divide the rps evenly between all pods to tap
	rpsPerPod := req.MaxRps / float32(len(pods))

	for _, pod := range pods {
		// initiate a tap on the pod
		match, err := makeMatch(req)
		if err != nil {
			return nil
		}
		go s.tapProxy(stream.Context(), rpsPerPod, match, pod.Status.PodIP, events)
	}

	// read events from the taps and send them back
	for event := range events {
		err := stream.Send(event)
		if err != nil {
			return err
		}
	}
	return nil
}

func validatePort(port uint32) error {
	if port > 65535 {
		return fmt.Errorf("Port number of range: %d", port)
	}
	return nil
}

func makeMatch(req *public.TapRequest) (*proxy.ObserveRequest_Match, error) {
	matches := make([]*proxy.ObserveRequest_Match, 0)
	if req.FromIP != "" {
		ip, err := util.ParseIPV4(req.FromIP)
		if err != nil {
			return nil, err
		}
		matches = append(matches, &proxy.ObserveRequest_Match{
			Match: &proxy.ObserveRequest_Match_Source{
				Source: &proxy.ObserveRequest_Match_Tcp{
					Match: &proxy.ObserveRequest_Match_Tcp_Netmask_{
						Netmask: &proxy.ObserveRequest_Match_Tcp_Netmask{
							Ip:   ip,
							Mask: 32,
						},
					},
				},
			},
		})
	}

	if req.FromPort != 0 {
		if err := validatePort(req.FromPort); err != nil {
			return nil, err
		}
		matches = append(matches, &proxy.ObserveRequest_Match{
			Match: &proxy.ObserveRequest_Match_Source{
				Source: &proxy.ObserveRequest_Match_Tcp{
					Match: &proxy.ObserveRequest_Match_Tcp_Ports{
						Ports: &proxy.ObserveRequest_Match_Tcp_PortRange{
							Min: req.FromPort,
						},
					},
				},
			},
		})
	}

	if req.ToIP != "" {
		ip, err := util.ParseIPV4(req.ToIP)
		if err != nil {
			return nil, err
		}
		matches = append(matches, &proxy.ObserveRequest_Match{
			Match: &proxy.ObserveRequest_Match_Destination{
				Destination: &proxy.ObserveRequest_Match_Tcp{
					Match: &proxy.ObserveRequest_Match_Tcp_Netmask_{
						Netmask: &proxy.ObserveRequest_Match_Tcp_Netmask{
							Ip:   ip,
							Mask: 32,
						},
					},
				},
			},
		})
	}

	if req.ToPort != 0 {
		if err := validatePort(req.ToPort); err != nil {
			return nil, err
		}
		matches = append(matches, &proxy.ObserveRequest_Match{
			Match: &proxy.ObserveRequest_Match_Destination{
				Destination: &proxy.ObserveRequest_Match_Tcp{
					Match: &proxy.ObserveRequest_Match_Tcp_Ports{
						Ports: &proxy.ObserveRequest_Match_Tcp_PortRange{
							Min: req.ToPort,
						},
					},
				},
			},
		})
	}

	if req.Scheme != "" {
		matches = append(matches, &proxy.ObserveRequest_Match{
			Match: &proxy.ObserveRequest_Match_Http_{
				Http: &proxy.ObserveRequest_Match_Http{
					Match: &proxy.ObserveRequest_Match_Http_Scheme{
						Scheme: parseScheme(req.Scheme),
					},
				},
			},
		})
	}

	if req.Method != "" {
		matches = append(matches, &proxy.ObserveRequest_Match{
			Match: &proxy.ObserveRequest_Match_Http_{
				Http: &proxy.ObserveRequest_Match_Http{
					Match: &proxy.ObserveRequest_Match_Http_Method{
						Method: parseMethod(req.Method),
					},
				},
			},
		})
	}

	// exact match
	if req.Authority != "" {
		matches = append(matches, &proxy.ObserveRequest_Match{
			Match: &proxy.ObserveRequest_Match_Http_{
				Http: &proxy.ObserveRequest_Match_Http{
					Match: &proxy.ObserveRequest_Match_Http_Authority{
						Authority: &proxy.ObserveRequest_Match_Http_StringMatch{
							Match: &proxy.ObserveRequest_Match_Http_StringMatch_Exact{
								Exact: req.Authority,
							},
						},
					},
				},
			},
		})
	}

	// prefix match
	if req.Path != "" {
		matches = append(matches, &proxy.ObserveRequest_Match{
			Match: &proxy.ObserveRequest_Match_Http_{
				Http: &proxy.ObserveRequest_Match_Http{
					Match: &proxy.ObserveRequest_Match_Http_Path{
						Path: &proxy.ObserveRequest_Match_Http_StringMatch{
							Match: &proxy.ObserveRequest_Match_Http_StringMatch_Prefix{
								Prefix: req.Path,
							},
						},
					},
				},
			},
		})
	}

	return &proxy.ObserveRequest_Match{
		Match: &proxy.ObserveRequest_Match_All{
			All: &proxy.ObserveRequest_Match_Seq{
				Matches: matches,
			},
		},
	}, nil
}

// TODO: validate scheme
func parseScheme(scheme string) *common.Scheme {
	value, ok := common.Scheme_Registered_value[strings.ToUpper(scheme)]
	if ok {
		return &common.Scheme{
			Type: &common.Scheme_Registered_{
				Registered: common.Scheme_Registered(value),
			},
		}
	}
	return &common.Scheme{
		Type: &common.Scheme_Unregistered{
			Unregistered: strings.ToUpper(scheme),
		},
	}
}

// TODO: validate method
func parseMethod(method string) *common.HttpMethod {
	value, ok := common.HttpMethod_Registered_value[strings.ToUpper(method)]
	if ok {
		return &common.HttpMethod{
			Type: &common.HttpMethod_Registered_{
				Registered: common.HttpMethod_Registered(value),
			},
		}
	}
	return &common.HttpMethod{
		Type: &common.HttpMethod_Unregistered{
			Unregistered: strings.ToUpper(method),
		},
	}
}

// Tap a pod.
// This method will run continuously until an error is encountered or the
// request is cancelled via the context.  Thus it should be called as a
// go-routine.
// To limit the rps to maxRps, this method calls Observe on the pod with a limit
// of maxRps * 10s at most once per 10s window.  If this limit is reached in
// less than 10s, we sleep until the end of the window before calling Observe
// again.
func (s *server) tapProxy(ctx context.Context, maxRps float32, match *proxy.ObserveRequest_Match, addr string, events chan *common.TapEvent) {
	tapAddr := fmt.Sprintf("%s:%d", addr, s.tapPort)
	log.Printf("Establishing tap on %s", tapAddr)
	conn, err := grpc.DialContext(ctx, tapAddr, grpc.WithInsecure())
	if err != nil {
		log.Println(err)
		return
	}
	client := proxy.NewTapClient(conn)

	req := &proxy.ObserveRequest{
		Limit: uint32(maxRps * float32(tapInterval.Seconds())),
		Match: match,
	}

	for { // Request loop
		windowStart := time.Now()
		windowEnd := windowStart.Add(tapInterval)
		rsp, err := client.Observe(ctx, req)
		if err != nil {
			log.Println(err)
			return
		}
		for { // Stream loop
			event, err := rsp.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Println(err)
				return
			}
			events <- event
		}
		if time.Now().Before(windowEnd) {
			time.Sleep(time.Until(windowEnd))
		}
	}
}

func NewServer(addr string, tapPort uint, kubeconfig string) (*grpc.Server, net.Listener, error) {

	clientSet, err := k8s.NewClientSet(kubeconfig)
	if err != nil {
		return nil, nil, err
	}

	replicaSets, err := k8s.NewReplicaSetStore(clientSet)
	if err != nil {
		return nil, nil, err
	}
	replicaSets.Run()

	// index pods by deployment
	deploymentIndex := func(obj interface{}) ([]string, error) {
		pod, ok := obj.(*v1.Pod)
		if !ok {
			return nil, fmt.Errorf("Object is not a Pod")
		}
		deployment, err := replicaSets.GetDeploymentForPod(pod)
		return []string{deployment}, err
	}

	pods, err := k8s.NewPodIndex(clientSet, deploymentIndex)
	if err != nil {
		return nil, nil, err
	}
	pods.Run()

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	s := util.NewGrpcServer()
	srv := server{
		tapPort:     tapPort,
		replicaSets: replicaSets,
		pods:        pods,
	}
	pb.RegisterTapServer(s, &srv)

	// TODO: register shutdown hook to call pods.Stop() and replicatSets.Stop()

	return s, lis, nil
}
