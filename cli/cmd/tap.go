package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/runconduit/conduit/cli/k8s"
	"github.com/runconduit/conduit/cli/shell"

	common "github.com/runconduit/conduit/controller/gen/common"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/controller/util"
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
			return errors.New("please specify a target")
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
			return fmt.Errorf("unsupported resourceType [%s]", friendlyNameForResourceType)
		}

		kubeApi, err := k8s.MakeK8sAPi(shell.MakeUnixShell(), kubeconfigPath, apiAddr)
		if err != nil {
			return err
		}

		client, err := newApiClient(kubeApi)
		if err != nil {
			return err
		}

		output, err := requestTapFromApi(client, args[1], validatedResourceType, partialReq)
		if err != nil {
			return err
		}
		_, err = fmt.Print(output)

		return err
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

func requestTapFromApi(client pb.ApiClient, targetName string, resourceType string, req *pb.TapRequest) (string, error) {
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
		return "", fmt.Errorf("unsupported resourceType [%s]", resourceType)
	}

	rsp, err := client.Tap(context.Background(), req)
	if err != nil {
		return "", err
	}

	return renderTap(rsp)
}

func renderTap(rsp pb.Api_TapClient) (string, error) {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 0, 0, ' ', tabwriter.AlignRight)
	err := writeTapEvenToBuffer(rsp, w)
	if err != nil {
		return "", err
	}
	w.Flush()

	// strip left padding on the first column
	out := string(buffer.Bytes())

	return out, nil

}

func writeTapEvenToBuffer(rsp pb.Api_TapClient, w *tabwriter.Writer) error {
	for {
		event, err := rsp.Recv()
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
	http_event := http.Event
	switch ev := http_event.(type) {
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
		return fmt.Sprintf("end id=%d:%d %s grpc-status=%s duration=%dµs response-length=%dB",
			ev.ResponseEnd.Id.Base,
			ev.ResponseEnd.Id.Stream,
			flow,
			codes.Code(ev.ResponseEnd.GetGrpcStatus()),
			ev.ResponseEnd.GetSinceResponseInit().Nanos/1000,
			ev.ResponseEnd.GetResponseBytes(),
		)
	default:
		return fmt.Sprintf("unknown %s", flow)
	}
}
