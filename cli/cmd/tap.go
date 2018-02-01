package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	common "github.com/runconduit/conduit/controller/gen/common"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/controller/util"
	"github.com/runconduit/conduit/pkg/k8s"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
)

var (
	maxRps    float32
	toPort    uint32
	toIP      string
	fromPort  uint32
	fromIP    string
	scheme    string
	method    string
	authority string
	path      string
)

var tapCmd = &cobra.Command{
	Use:   "tap [flags] (deployment|pod) TARGET",
	Short: "Listen to a traffic stream",
	Long: `Listen to a traffic stream.

Valid targets include:
 * Pods (default/hello-world-h4fb2)
 * Deployments (default/hello-world)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 2 {
			return errors.New("please specify a resource type and target")
		}

		// We don't validate inputs because they are validated on the server.
		partialReq := &pb.TapRequest{
			MaxRps:    maxRps,
			ToPort:    toPort,
			ToIP:      toIP,
			FromPort:  fromPort,
			FromIP:    fromIP,
			Scheme:    scheme,
			Method:    method,
			Authority: authority,
			Path:      path,
		}

		friendlyNameForResourceType := strings.ToLower(args[0])
		validatedResourceType, err := k8s.CanonicalKubernetesNameFromFriendlyName(friendlyNameForResourceType)
		if err != nil {
			return fmt.Errorf("unsupported resource type [%s]", friendlyNameForResourceType)
		}

		client, err := newPublicAPIClient()
		if err != nil {
			return err
		}

		return requestTapFromApi(os.Stdout, client, args[1], validatedResourceType, partialReq)
	},
}

func init() {
	RootCmd.AddCommand(tapCmd)
	addControlPlaneNetworkingArgs(tapCmd)
	tapCmd.PersistentFlags().Float32Var(&maxRps, "max-rps", 1.0, "Maximum requests per second to tap.")
	tapCmd.PersistentFlags().Uint32Var(&toPort, "to-port", 0, "Display requests to this port")
	tapCmd.PersistentFlags().StringVar(&toIP, "to-ip", "", "Display requests to this IP")
	tapCmd.PersistentFlags().Uint32Var(&fromPort, "from-port", 0, "Display requests from this port")
	tapCmd.PersistentFlags().StringVar(&fromIP, "from-ip", "", "Display requests from this IP")
	tapCmd.PersistentFlags().StringVar(&scheme, "scheme", "", "Display requests with this scheme")
	tapCmd.PersistentFlags().StringVar(&method, "method", "", "Display requests with this HTTP method")
	tapCmd.PersistentFlags().StringVar(&authority, "authority", "", "Display requests with this :authority")
	tapCmd.PersistentFlags().StringVar(&path, "path", "", "Display requests with paths that start with this prefix")
}

func requestTapFromApi(w io.Writer, client pb.ApiClient, targetName string, resourceType string, req *pb.TapRequest) error {
	switch resourceType {
	case k8s.KubernetesDeployments:
		req.Target = &pb.TapRequest_Deployment{
			Deployment: targetName,
		}

	case k8s.KubernetesPods:
		req.Target = &pb.TapRequest_Pod{
			Pod: targetName,
		}
	default:
		return fmt.Errorf("unsupported resource type [%s]", resourceType)
	}

	rsp, err := client.Tap(context.Background(), req)
	if err != nil {
		return err
	}

	return renderTap(w, rsp)
}

func renderTap(w io.Writer, tapClient pb.Api_TapClient) error {
	tableWriter := tabwriter.NewWriter(w, 0, 0, 0, ' ', tabwriter.AlignRight)
	err := writeTapEventsToBuffer(tapClient, tableWriter)
	if err != nil {
		return err
	}
	tableWriter.Flush()

	return nil
}

func writeTapEventsToBuffer(tapClient pb.Api_TapClient, w *tabwriter.Writer) error {
	for {
		log.Debug("Waiting for data...")
		event, err := tapClient.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			break
		}
		_, err = fmt.Fprintln(w, renderTapEvent(event))
		if err != nil {
			return err
		}
	}

	return nil
}

func renderTapEvent(event *common.TapEvent) string {
	flow := fmt.Sprintf("src=%s dst=%s",
		util.AddressToString(event.GetSource()),
		util.AddressToString(event.GetTarget()),
	)

	http := event.GetHttp()
	httpEvent := http.Event
	switch ev := httpEvent.(type) {
	case *common.TapEvent_Http_RequestInit_:
		return fmt.Sprintf("req id=%d:%d %s :method=%s :authority=%s :path=%s",
			ev.RequestInit.Id.Base,
			ev.RequestInit.Id.Stream,
			flow,
			ev.RequestInit.Method.GetRegistered().String(),
			ev.RequestInit.Authority,
			ev.RequestInit.Path,
		)
	case *common.TapEvent_Http_ResponseInit_:
		return fmt.Sprintf("rsp id=%d:%d %s :status=%d latency=%dµs",
			ev.ResponseInit.Id.Base,
			ev.ResponseInit.Id.Stream,
			flow,
			ev.ResponseInit.GetHttpStatus(),
			ev.ResponseInit.GetSinceRequestInit().Nanos/1000,
		)
	case *common.TapEvent_Http_ResponseEnd_:

		if ev.ResponseEnd.Eos != nil {
			switch eos := ev.ResponseEnd.Eos.End.(type) {
			case *common.Eos_GrpcStatusCode:
				return fmt.Sprintf("end id=%d:%d %s grpc-status=%s duration=%dµs response-length=%dB",
					ev.ResponseEnd.Id.Base,
					ev.ResponseEnd.Id.Stream,
					flow,
					codes.Code(eos.GrpcStatusCode),
					ev.ResponseEnd.GetSinceResponseInit().Nanos/1000,
					ev.ResponseEnd.GetResponseBytes(),
				)
			case *common.Eos_ResetErrorCode:
				return fmt.Sprintf("end id=%d:%d %s reset-error=%+v duration=%dµs response-length=%dB",
					ev.ResponseEnd.Id.Base,
					ev.ResponseEnd.Id.Stream,
					flow,
					eos.ResetErrorCode,
					ev.ResponseEnd.GetSinceResponseInit().Nanos/1000,
					ev.ResponseEnd.GetResponseBytes(),
				)
			}
		}

		// this catchall handles 2 cases:
		// 1) ev.ResponseEnd.Eos == nil
		// 2) ev.ResponseEnd.Eos.End == nil
		return fmt.Sprintf("end id=%d:%d %s duration=%dµs response-length=%dB",
			ev.ResponseEnd.Id.Base,
			ev.ResponseEnd.Id.Stream,
			flow,
			ev.ResponseEnd.GetSinceResponseInit().Nanos/1000,
			ev.ResponseEnd.GetResponseBytes(),
		)

	default:
		return fmt.Sprintf("unknown %s", flow)
	}
}
