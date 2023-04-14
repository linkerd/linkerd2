package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/profiles"
	"github.com/linkerd/linkerd2/pkg/protohttp"
	"github.com/linkerd/linkerd2/viz/pkg/api"
	hc "github.com/linkerd/linkerd2/viz/pkg/healthcheck"
	vizutil "github.com/linkerd/linkerd2/viz/pkg/util"
	pb "github.com/linkerd/linkerd2/viz/tap/gen/tap"
	"github.com/linkerd/linkerd2/viz/tap/pkg"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"
)

type profileOptions struct {
	name          string
	namespace     string
	tap           string
	tapDuration   time.Duration
	tapRouteLimit uint
}

func newProfileOptions() *profileOptions {
	return &profileOptions{
		tapDuration:   5 * time.Second,
		tapRouteLimit: 20,
	}
}

func (options *profileOptions) validate() error {
	if options.tap == "" {
		return errors.New("The --tap flag must be specified")
	}
	// a DNS-1035 label must consist of lower case alphanumeric characters or '-',
	// start with an alphabetic character, and end with an alphanumeric character
	if errs := validation.IsDNS1035Label(options.name); len(errs) != 0 {
		return fmt.Errorf("invalid service %q: %v", options.name, errs)
	}
	// a DNS-1123 label must consist of lower case alphanumeric characters or '-',
	// and must start and end with an alphanumeric character
	if errs := validation.IsDNS1123Label(options.namespace); len(errs) != 0 {
		return fmt.Errorf("invalid namespace %q: %v", options.namespace, errs)
	}
	return nil
}

// newCmdProfile creates a new cobra command for the Profile subcommand which
// generates Linkerd service profile based off tap data.
func newCmdProfile() *cobra.Command {
	options := newProfileOptions()

	cmd := &cobra.Command{
		Use:   "profile [flags] --tap resource (SERVICE)",
		Short: "Output service profile config for Kubernetes based off tap data",
		Long:  "Output service profile config for Kubernetes based off tap data.",
		Example: `  # Generate a profile by watching live traffic.
  linkerd viz profile -n emojivoto web-svc --tap deploy/web --tap-duration 10s --tap-route-limit 5
`,
		Args: cobra.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			// Skip providing suggestions if one or more arguments are provided
			// We either have a suggestion selected or more multiple args are provided
			// which is not allowed for this command.
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveError
			}

			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}

			if options.namespace == "" {
				options.namespace = pkgcmd.GetDefaultNamespace(kubeconfigPath, kubeContext)
			}

			cc := k8s.NewCommandCompletion(k8sAPI, options.namespace)

			// Profiles require completion for only services so prepend the service resource
			// type to the list of args
			results, err := cc.Complete([]string{k8s.Service}, toComplete)
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}

			return results, cobra.ShellCompDirectiveDefault
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			api.CheckClientOrExit(hc.VizOptions{
				Options: &healthcheck.Options{
					ControlPlaneNamespace: controlPlaneNamespace,
					KubeConfig:            kubeconfigPath,
					Impersonate:           impersonate,
					ImpersonateGroup:      impersonateGroup,
					KubeContext:           kubeContext,
					APIAddr:               apiAddr,
				},
				VizNamespaceOverride: vizNamespace,
			})
			if options.namespace == "" {
				options.namespace = pkgcmd.GetDefaultNamespace(kubeconfigPath, kubeContext)
			}
			options.name = args[0]
			clusterDomain := "cluster.local"
			var k8sAPI *k8s.KubernetesAPI
			err := options.validate()
			if err != nil {
				return err
			}
			k8sAPI, err = k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}
			_, values, err := healthcheck.FetchCurrentConfiguration(cmd.Context(), k8sAPI, controlPlaneNamespace)
			if err != nil {
				return err
			}
			if cd := values.ClusterDomain; cd != "" {
				clusterDomain = cd
			}
			return renderTapOutputProfile(cmd.Context(), k8sAPI, options.tap, options.namespace, options.name, clusterDomain, options.tapDuration, int(options.tapRouteLimit), os.Stdout)
		},
	}
	cmd.PersistentFlags().StringVar(&options.tap, "tap", options.tap, "Output a service profile based on tap data for the given target resource")
	cmd.PersistentFlags().DurationVar(&options.tapDuration, "tap-duration", options.tapDuration, "Duration over which tap data is collected (for example: \"10s\", \"1m\", \"10m\")")
	cmd.PersistentFlags().UintVar(&options.tapRouteLimit, "tap-route-limit", options.tapRouteLimit, "Max number of routes to add to the profile")
	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace of the service")

	pkgcmd.ConfigureNamespaceFlagCompletion(
		cmd, []string{"namespace"},
		kubeconfigPath, impersonate, impersonateGroup, kubeContext)
	return cmd
}

// renderTapOutputProfile performs a tap on the desired resource and generates
// a service profile with routes pre-populated from the tap data
// Only inbound tap traffic is considered.
func renderTapOutputProfile(ctx context.Context, k8sAPI *k8s.KubernetesAPI, tapResource, namespace, name, clusterDomain string, tapDuration time.Duration, routeLimit int, w io.Writer) error {
	requestParams := pkg.TapRequestParams{
		Resource:  tapResource,
		Namespace: namespace,
	}
	log.Debugf("Running `linkerd tap %s --namespace %s`", tapResource, namespace)
	req, err := pkg.BuildTapByResourceRequest(requestParams)
	if err != nil {
		return err
	}
	profile, err := tapToServiceProfile(ctx, k8sAPI, req, namespace, name, clusterDomain, tapDuration, routeLimit)
	if err != nil {
		return err
	}
	output, err := yaml.Marshal(profile)
	if err != nil {
		return fmt.Errorf("Error writing Service Profile: %w", err)
	}
	w.Write(output)
	return nil
}

func tapToServiceProfile(ctx context.Context, k8sAPI *k8s.KubernetesAPI, tapReq *pb.TapByResourceRequest, namespace, name, clusterDomain string, tapDuration time.Duration, routeLimit int) (sp.ServiceProfile, error) {
	profile := sp.ServiceProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s.%s.svc.%s", name, namespace, clusterDomain),
			Namespace: namespace,
		},
		TypeMeta: profiles.ServiceProfileMeta,
	}
	ctxWithTime, cancel := context.WithTimeout(ctx, tapDuration)
	defer cancel()
	reader, body, err := pkg.Reader(ctxWithTime, k8sAPI, tapReq)
	if err != nil {
		return profile, err
	}
	defer body.Close()
	routes := routeSpecFromTap(reader, routeLimit)
	profile.Spec.Routes = routes
	return profile, nil
}

func routeSpecFromTap(tapByteStream *bufio.Reader, routeLimit int) []*sp.RouteSpec {
	routes := make([]*sp.RouteSpec, 0)
	routesMap := make(map[string]*sp.RouteSpec)
	for {
		log.Debug("Waiting for data...")
		event := pb.TapEvent{}
		err := protohttp.FromByteStreamToProtocolBuffers(tapByteStream, &event)
		if err != nil {
			// expected errors when hitting the tapDuration deadline
			var e net.Error
			if !errors.Is(err, io.EOF) &&
				!(errors.As(err, &e) && e.Timeout()) &&
				!errors.Is(err, context.DeadlineExceeded) &&
				!strings.HasSuffix(err.Error(), pkg.ErrClosedResponseBody) {
				fmt.Fprintln(os.Stderr, err)
			}
			break
		}
		routeSpec := getPathDataFromTap(&event)
		log.Debugf("Created route spec: %v", routeSpec)
		if routeSpec != nil {
			routesMap[routeSpec.Name] = routeSpec
			if len(routesMap) >= routeLimit {
				break
			}
		}
	}
	for _, path := range sortMapKeys(routesMap) {
		routes = append(routes, routesMap[path])
	}
	return routes
}

func sortMapKeys(m map[string]*sp.RouteSpec) (keys []string) {
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return
}

func getPathDataFromTap(event *pb.TapEvent) *sp.RouteSpec {
	if event.GetProxyDirection() != pb.TapEvent_INBOUND {
		return nil
	}
	switch ev := event.GetHttp().GetEvent().(type) {
	case *pb.TapEvent_Http_RequestInit_:
		path := ev.RequestInit.GetPath()
		if path == "/" {
			return nil
		}

		return profiles.MkRouteSpec(
			path,
			profiles.PathToRegex(path), // for now, no path consolidation
			vizutil.HTTPMethodToString(ev.RequestInit.GetMethod()),
			nil)
	default:
		return nil
	}
}
