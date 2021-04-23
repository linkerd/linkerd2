package cmd

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/multicluster"
	"github.com/linkerd/linkerd2/pkg/servicemirror"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/linkerd/linkerd2/pkg/version"
	vizCmd "github.com/linkerd/linkerd2/viz/cmd"
	"github.com/linkerd/linkerd2/viz/metrics-api/client"
	vizPb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// MulticlusterExtensionName is the name of the multicluster extension
	MulticlusterExtensionName = "multicluster"

	// linkerdMulticlusterExtensionCheck adds checks related to the multicluster extension
	linkerdMulticlusterExtensionCheck healthcheck.CategoryID = "linkerd-multicluster"

	linkerdServiceMirrorServiceAccountName = "linkerd-service-mirror-%s"
	linkerdServiceMirrorComponentName      = "service-mirror"
	linkerdServiceMirrorClusterRoleName    = "linkerd-service-mirror-access-local-resources-%s"
	linkerdServiceMirrorRoleName           = "linkerd-service-mirror-read-remote-creds-%s"
)

type checkOptions struct {
	wait   time.Duration
	output string
}

func newCheckOptions() *checkOptions {
	return &checkOptions{
		wait:   300 * time.Second,
		output: healthcheck.TableOutput,
	}
}

func (options *checkOptions) validate() error {
	if options.output != healthcheck.TableOutput && options.output != healthcheck.JSONOutput {
		return fmt.Errorf("Invalid output type '%s'. Supported output types are: %s, %s", options.output, healthcheck.JSONOutput, healthcheck.TableOutput)
	}
	return nil
}

type healthChecker struct {
	*healthcheck.HealthChecker
	links []multicluster.Link
}

func newHealthChecker(linkerdHC *healthcheck.HealthChecker) *healthChecker {
	return &healthChecker{
		linkerdHC,
		[]multicluster.Link{},
	}
}

// NewCmdCheck generates a new cobra command for the multicluster extension.
func NewCmdCheck() *cobra.Command {
	options := newCheckOptions()
	cmd := &cobra.Command{
		Use:   "check [flags]",
		Args:  cobra.NoArgs,
		Short: "Check the multicluster extension for potential problems",
		Long: `Check the multicluster extension for potential problems.

The check command will perform a series of checks to validate that the
multicluster extension is configured correctly. If the command encounters a
failure it will print additional information about the failure and exit with a
non-zero exit code.`,
		Example: `  # Check that the multicluster extension is configured correctly
  linkerd multicluster check`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get the multicluster extension namespace
			kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			_, err = kubeAPI.GetNamespaceWithExtensionLabel(context.Background(), MulticlusterExtensionName)
			if err != nil {
				err = fmt.Errorf("%w; install by running `linkerd multicluster install | kubectl apply -f -`", err)
				fmt.Fprintln(os.Stderr, err.Error())
				os.Exit(1)
			}
			return configureAndRunChecks(stdout, stderr, options)
		},
	}
	cmd.Flags().StringVarP(&options.output, "output", "o", options.output, "Output format. One of: basic, json")
	cmd.Flags().DurationVar(&options.wait, "wait", options.wait, "Maximum allowed time for all tests to pass")
	cmd.Flags().Bool("proxy", false, "")
	cmd.Flags().MarkHidden("proxy")
	cmd.Flags().StringP("namespace", "n", "", "")
	cmd.Flags().MarkHidden("namespace")
	return cmd
}

func configureAndRunChecks(wout io.Writer, werr io.Writer, options *checkOptions) error {
	err := options.validate()
	if err != nil {
		return fmt.Errorf("Validation error when executing check command: %v", err)
	}
	checks := []healthcheck.CategoryID{
		linkerdMulticlusterExtensionCheck,
	}
	linkerdHC := healthcheck.NewHealthChecker(checks, &healthcheck.Options{
		ControlPlaneNamespace: controlPlaneNamespace,
		KubeConfig:            kubeconfigPath,
		KubeContext:           kubeContext,
		Impersonate:           impersonate,
		ImpersonateGroup:      impersonateGroup,
		APIAddr:               apiAddr,
		RetryDeadline:         time.Now().Add(options.wait),
	})

	err = linkerdHC.InitializeKubeAPIClient()
	if err != nil {
		err = fmt.Errorf("Error initializing k8s API client: %s", err)
		fmt.Fprintln(werr, err)
		os.Exit(1)
	}

	err = linkerdHC.InitializeLinkerdGlobalConfig(context.Background())
	if err != nil {
		err = fmt.Errorf("Failed to fetch linkerd config: %s", err)
		fmt.Fprintln(werr, err)
		os.Exit(1)
	}

	hc := newHealthChecker(linkerdHC)
	category := multiclusterCategory(hc)
	hc.AppendCategories(category)
	success := healthcheck.RunChecks(wout, werr, hc, options.output)
	if !success {
		os.Exit(1)
	}
	return nil
}

func multiclusterCategory(hc *healthChecker) *healthcheck.Category {
	checkers := []healthcheck.Checker{}
	checkers = append(checkers,
		*healthcheck.NewChecker("Link CRD exists").
			WithHintAnchor("l5d-multicluster-link-crd-exists").
			Fatal().
			WithCheck(func(ctx context.Context) error { return hc.checkLinkCRD(ctx) }))
	checkers = append(checkers,
		*healthcheck.NewChecker("Link resources are valid").
			WithHintAnchor("l5d-multicluster-links-are-valid").
			Fatal().
			WithCheck(func(ctx context.Context) error { return hc.checkLinks(ctx) }))
	checkers = append(checkers,
		*healthcheck.NewChecker("remote cluster access credentials are valid").
			WithHintAnchor("l5d-smc-target-clusters-access").
			WithCheck(func(ctx context.Context) error { return hc.checkRemoteClusterConnectivity(ctx) }))
	checkers = append(checkers,
		*healthcheck.NewChecker("clusters share trust anchors").
			WithHintAnchor("l5d-multicluster-clusters-share-anchors").
			WithCheck(func(ctx context.Context) error {
				localAnchors, err := tls.DecodePEMCertificates(hc.LinkerdConfig().IdentityTrustAnchorsPEM)
				if err != nil {
					return fmt.Errorf("Cannot parse source trust anchors: %s", err)
				}
				return hc.checkRemoteClusterAnchors(ctx, localAnchors)
			}))
	checkers = append(checkers,
		*healthcheck.NewChecker("service mirror controller has required permissions").
			WithHintAnchor("l5d-multicluster-source-rbac-correct").
			WithCheck(func(ctx context.Context) error {
				return hc.checkServiceMirrorLocalRBAC(ctx)
			}))
	checkers = append(checkers,
		*healthcheck.NewChecker("service mirror controllers are running").
			WithHintAnchor("l5d-multicluster-service-mirror-running").
			WithRetryDeadline(hc.RetryDeadline).
			SurfaceErrorOnRetry().
			WithCheck(func(ctx context.Context) error {
				return hc.checkServiceMirrorController(ctx)
			}))
	checkers = append(checkers,
		*healthcheck.NewChecker("all gateway mirrors are healthy").
			WithHintAnchor("l5d-multicluster-gateways-endpoints").
			WithCheck(func(ctx context.Context) error {
				return hc.checkIfGatewayMirrorsHaveEndpoints(ctx)
			}))
	checkers = append(checkers,
		*healthcheck.NewChecker("all mirror services have endpoints").
			WithHintAnchor("l5d-multicluster-services-endpoints").
			WithCheck(func(ctx context.Context) error {
				return hc.checkIfMirrorServicesHaveEndpoints(ctx)
			}))
	checkers = append(checkers,
		*healthcheck.NewChecker("all mirror services are part of a Link").
			WithHintAnchor("l5d-multicluster-orphaned-services").
			Warning().
			WithCheck(func(ctx context.Context) error {
				return hc.checkForOrphanedServices(ctx)
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("multicluster extension proxies are healthy").
			WithHintAnchor("l5d-multicluster-proxy-healthy").
			Fatal().
			WithRetryDeadline(hc.RetryDeadline).
			SurfaceErrorOnRetry().
			WithCheck(func(ctx context.Context) error {
				for _, link := range hc.links {
					err := hc.CheckProxyHealth(ctx, hc.ControlPlaneNamespace, link.Namespace)
					if err != nil {
						return err
					}
				}
				return nil
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("multicluster extension proxies are up-to-date").
			WithHintAnchor("l5d-multicluster-proxy-cp-version").
			Warning().
			WithCheck(func(ctx context.Context) error {
				var err error
				if hc.VersionOverride != "" {
					hc.LatestVersions, err = version.NewChannels(hc.VersionOverride)
				} else {
					uuid := "unknown"
					if hc.UUID() != "" {
						uuid = hc.UUID()
					}
					hc.LatestVersions, err = version.GetLatestVersions(ctx, uuid, "cli")
				}
				if err != nil {
					return err
				}

				var pods []corev1.Pod
				for _, link := range hc.links {
					nsPods, err := hc.KubeAPIClient().GetPodsByNamespace(ctx, link.Namespace)
					if err != nil {
						return err
					}

					pods = append(pods, nsPods...)
				}

				return hc.CheckProxyVersionsUpToDate(pods)
			}))

	checkers = append(checkers,
		*healthcheck.NewChecker("multicluster extension proxies and cli versions match").
			WithHintAnchor("l5d-multicluster-proxy-cli-version").
			Warning().
			WithCheck(func(ctx context.Context) error {
				var pods []corev1.Pod
				for _, link := range hc.links {
					nsPods, err := hc.KubeAPIClient().GetPodsByNamespace(ctx, link.Namespace)
					if err != nil {
						return err
					}

					pods = append(pods, nsPods...)
				}

				return healthcheck.CheckIfProxyVersionsMatchWithCLI(pods)
			}))

	return healthcheck.NewCategory(linkerdMulticlusterExtensionCheck, checkers, true)
}

func (hc *healthChecker) checkLinkCRD(ctx context.Context) error {
	err := hc.linkAccess(ctx)
	if err != nil {
		return fmt.Errorf("multicluster.linkerd.io/Link CRD is missing: %s", err)
	}
	return nil
}

func (hc *healthChecker) linkAccess(ctx context.Context) error {
	res, err := hc.KubeAPIClient().Discovery().ServerResourcesForGroupVersion(k8s.LinkAPIGroupVersion)
	if err != nil {
		return err
	}
	if res.GroupVersion == k8s.LinkAPIGroupVersion {
		for _, apiRes := range res.APIResources {
			if apiRes.Kind == k8s.LinkKind {
				return k8s.ResourceAuthz(ctx, hc.KubeAPIClient(), "", "list", k8s.LinkAPIGroup, k8s.LinkAPIVersion, "links", "")
			}
		}
	}
	return errors.New("Link CRD not found")
}

func (hc *healthChecker) checkLinks(ctx context.Context) error {
	links, err := multicluster.GetLinks(ctx, hc.KubeAPIClient().DynamicClient)
	if err != nil {
		return err
	}
	if len(links) == 0 {
		return &healthcheck.SkipError{Reason: "no links detected"}
	}
	linkNames := []string{}
	for _, l := range links {
		linkNames = append(linkNames, fmt.Sprintf("\t* %s", l.TargetClusterName))
	}
	hc.links = links
	return &healthcheck.VerboseSuccess{Message: strings.Join(linkNames, "\n")}
}

func (hc *healthChecker) checkRemoteClusterConnectivity(ctx context.Context) error {
	errors := []error{}
	links := []string{}
	for _, link := range hc.links {
		// Load the credentials secret
		secret, err := hc.KubeAPIClient().Interface.CoreV1().Secrets(link.Namespace).Get(ctx, link.ClusterCredentialsSecret, metav1.GetOptions{})
		if err != nil {
			errors = append(errors, fmt.Errorf("* secret: [%s/%s]: %s", link.Namespace, link.ClusterCredentialsSecret, err))
			continue
		}
		config, err := servicemirror.ParseRemoteClusterSecret(secret)
		if err != nil {
			errors = append(errors, fmt.Errorf("* secret: [%s/%s]: could not parse config secret: %s", secret.Namespace, secret.Name, err))
			continue
		}
		clientConfig, err := clientcmd.RESTConfigFromKubeConfig(config)
		if err != nil {
			errors = append(errors, fmt.Errorf("* secret: [%s/%s] cluster: [%s]: unable to parse api config: %s", secret.Namespace, secret.Name, link.TargetClusterName, err))
			continue
		}
		remoteAPI, err := k8s.NewAPIForConfig(clientConfig, "", []string{}, healthcheck.RequestTimeout)
		if err != nil {
			errors = append(errors, fmt.Errorf("* secret: [%s/%s] cluster: [%s]: could not instantiate api for target cluster: %s", secret.Namespace, secret.Name, link.TargetClusterName, err))
			continue
		}
		// We use this call just to check connectivity.
		_, err = remoteAPI.Discovery().ServerVersion()
		if err != nil {
			errors = append(errors, fmt.Errorf("* failed to connect to API for cluster: [%s]: %s", link.TargetClusterName, err))
			continue
		}
		verbs := []string{"get", "list", "watch"}
		for _, verb := range verbs {
			if err := healthcheck.CheckCanPerformAction(ctx, remoteAPI, verb, corev1.NamespaceAll, "", "v1", "services"); err != nil {
				errors = append(errors, fmt.Errorf("* missing service permission [%s] for cluster [%s]: %s", verb, link.TargetClusterName, err))
			}
		}
		links = append(links, fmt.Sprintf("\t* %s", link.TargetClusterName))
	}
	if len(errors) > 0 {
		return joinErrors(errors, 2)
	}
	if len(links) == 0 {
		return &healthcheck.SkipError{Reason: "no links"}
	}
	return &healthcheck.VerboseSuccess{Message: strings.Join(links, "\n")}
}

func (hc *healthChecker) checkRemoteClusterAnchors(ctx context.Context, localAnchors []*x509.Certificate) error {
	errors := []string{}
	links := []string{}
	for _, link := range hc.links {
		// Load the credentials secret
		secret, err := hc.KubeAPIClient().Interface.CoreV1().Secrets(link.Namespace).Get(ctx, link.ClusterCredentialsSecret, metav1.GetOptions{})
		if err != nil {
			errors = append(errors, fmt.Sprintf("* secret: [%s/%s]: %s", link.Namespace, link.ClusterCredentialsSecret, err))
			continue
		}
		config, err := servicemirror.ParseRemoteClusterSecret(secret)
		if err != nil {
			errors = append(errors, fmt.Sprintf("* secret: [%s/%s]: could not parse config secret: %s", secret.Namespace, secret.Name, err))
			continue
		}
		clientConfig, err := clientcmd.RESTConfigFromKubeConfig(config)
		if err != nil {
			errors = append(errors, fmt.Sprintf("* secret: [%s/%s] cluster: [%s]: unable to parse api config: %s", secret.Namespace, secret.Name, link.TargetClusterName, err))
			continue
		}
		remoteAPI, err := k8s.NewAPIForConfig(clientConfig, "", []string{}, healthcheck.RequestTimeout)
		if err != nil {
			errors = append(errors, fmt.Sprintf("* secret: [%s/%s] cluster: [%s]: could not instantiate api for target cluster: %s", secret.Namespace, secret.Name, link.TargetClusterName, err))
			continue
		}
		_, values, err := healthcheck.FetchCurrentConfiguration(ctx, remoteAPI, link.TargetClusterLinkerdNamespace)
		if err != nil {
			errors = append(errors, fmt.Sprintf("* %s: unable to fetch anchors: %s", link.TargetClusterName, err))
			continue
		}
		remoteAnchors, err := tls.DecodePEMCertificates(values.IdentityTrustAnchorsPEM)
		if err != nil {
			errors = append(errors, fmt.Sprintf("* %s: cannot parse trust anchors", link.TargetClusterName))
			continue
		}
		// we fail early if the lens are not the same. If they are the
		// same, we can only compare certs one way and be sure we have
		// identical anchors
		if len(remoteAnchors) != len(localAnchors) {
			errors = append(errors, fmt.Sprintf("* %s", link.TargetClusterName))
			continue
		}
		localAnchorsMap := make(map[string]*x509.Certificate)
		for _, c := range localAnchors {
			localAnchorsMap[string(c.Signature)] = c
		}
		for _, remote := range remoteAnchors {
			local, ok := localAnchorsMap[string(remote.Signature)]
			if !ok || !local.Equal(remote) {
				errors = append(errors, fmt.Sprintf("* %s", link.TargetClusterName))
				break
			}
		}
		links = append(links, fmt.Sprintf("\t* %s", link.TargetClusterName))
	}
	if len(errors) > 0 {
		return fmt.Errorf("Problematic clusters:\n    %s", strings.Join(errors, "\n    "))
	}
	if len(links) == 0 {
		return &healthcheck.SkipError{Reason: "no links"}
	}
	return &healthcheck.VerboseSuccess{Message: strings.Join(links, "\n")}
}

func (hc *healthChecker) checkServiceMirrorLocalRBAC(ctx context.Context) error {
	links := []string{}
	errors := []string{}
	for _, link := range hc.links {
		err := healthcheck.CheckServiceAccounts(
			ctx,
			hc.KubeAPIClient(),
			[]string{fmt.Sprintf(linkerdServiceMirrorServiceAccountName, link.TargetClusterName)},
			link.Namespace,
			serviceMirrorComponentsSelector(link.TargetClusterName),
		)
		if err != nil {
			errors = append(errors, err.Error())
		}
		err = healthcheck.CheckClusterRoles(
			ctx,
			hc.KubeAPIClient(),
			true,
			[]string{fmt.Sprintf(linkerdServiceMirrorClusterRoleName, link.TargetClusterName)},
			serviceMirrorComponentsSelector(link.TargetClusterName),
		)
		if err != nil {
			errors = append(errors, err.Error())
		}
		err = healthcheck.CheckClusterRoleBindings(
			ctx,
			hc.KubeAPIClient(),
			true,
			[]string{fmt.Sprintf(linkerdServiceMirrorClusterRoleName, link.TargetClusterName)},
			serviceMirrorComponentsSelector(link.TargetClusterName),
		)
		if err != nil {
			errors = append(errors, err.Error())
		}
		err = healthcheck.CheckRoles(
			ctx,
			hc.KubeAPIClient(),
			true,
			link.Namespace,
			[]string{fmt.Sprintf(linkerdServiceMirrorRoleName, link.TargetClusterName)},
			serviceMirrorComponentsSelector(link.TargetClusterName),
		)
		if err != nil {
			errors = append(errors, err.Error())
		}
		err = healthcheck.CheckRoleBindings(
			ctx,
			hc.KubeAPIClient(),
			true,
			link.Namespace,
			[]string{fmt.Sprintf(linkerdServiceMirrorRoleName, link.TargetClusterName)},
			serviceMirrorComponentsSelector(link.TargetClusterName),
		)
		if err != nil {
			errors = append(errors, err.Error())
		}
		links = append(links, fmt.Sprintf("\t* %s", link.TargetClusterName))
	}
	if len(errors) > 0 {
		return fmt.Errorf(strings.Join(errors, "\n"))
	}
	if len(links) == 0 {
		return &healthcheck.SkipError{Reason: "no links"}
	}
	return &healthcheck.VerboseSuccess{Message: strings.Join(links, "\n")}
}

func (hc *healthChecker) checkServiceMirrorController(ctx context.Context) error {
	errors := []error{}
	clusterNames := []string{}
	for _, link := range hc.links {
		options := metav1.ListOptions{
			LabelSelector: serviceMirrorComponentsSelector(link.TargetClusterName),
		}
		result, err := hc.KubeAPIClient().AppsV1().Deployments(corev1.NamespaceAll).List(ctx, options)
		if err != nil {
			return err
		}
		if len(result.Items) > 1 {
			errors = append(errors, fmt.Errorf("* too many service mirror controller deployments for Link %s", link.TargetClusterName))
			continue
		}
		if len(result.Items) == 0 {
			errors = append(errors, fmt.Errorf("* no service mirror controller deployment for Link %s", link.TargetClusterName))
			continue
		}
		controller := result.Items[0]
		if controller.Status.AvailableReplicas < 1 {
			errors = append(errors, fmt.Errorf("* service mirror controller is not available: %s/%s", controller.Namespace, controller.Name))
			continue
		}
		clusterNames = append(clusterNames, fmt.Sprintf("\t* %s", link.TargetClusterName))
	}
	if len(errors) > 0 {
		return joinErrors(errors, 2)
	}
	if len(clusterNames) == 0 {
		return &healthcheck.SkipError{Reason: "no links"}
	}
	return &healthcheck.VerboseSuccess{Message: strings.Join(clusterNames, "\n")}
}

func (hc *healthChecker) checkIfGatewayMirrorsHaveEndpoints(ctx context.Context) error {
	links := []string{}
	errors := []error{}
	for _, link := range hc.links {
		selector := metav1.ListOptions{LabelSelector: fmt.Sprintf("%s,%s=%s", k8s.MirroredGatewayLabel, k8s.RemoteClusterNameLabel, link.TargetClusterName)}
		gatewayMirrors, err := hc.KubeAPIClient().CoreV1().Services(metav1.NamespaceAll).List(ctx, selector)
		if err != nil {
			errors = append(errors, err)
			continue
		}
		if len(gatewayMirrors.Items) != 1 {
			errors = append(errors, fmt.Errorf("wrong number (%d) of probe gateways for target cluster %s", len(gatewayMirrors.Items), link.TargetClusterName))
			continue
		}
		svc := gatewayMirrors.Items[0]
		// Check if there is a relevant end-point
		endpoints, err := hc.KubeAPIClient().CoreV1().Endpoints(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
		if err != nil || len(endpoints.Subsets) == 0 {
			errors = append(errors, fmt.Errorf("%s.%s mirrored from cluster [%s] has no endpoints", svc.Name, svc.Namespace, svc.Labels[k8s.RemoteClusterNameLabel]))
			continue
		}

		vizNs, err := hc.KubeAPIClient().GetNamespaceWithExtensionLabel(ctx, vizCmd.ExtensionName)
		if err != nil {
			return &healthcheck.SkipError{Reason: "failed to fetch gateway metrics"}
		}

		// Check gateway liveness according to probes
		vizClient, err := client.NewExternalClient(ctx, vizNs.Name, hc.KubeAPIClient())
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to initialize viz client: %s", err))
			break
		}
		req := vizPb.GatewaysRequest{
			TimeWindow:        "1m",
			RemoteClusterName: link.TargetClusterName,
		}
		rsp, err := vizClient.Gateways(ctx, &req)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to fetch gateway metrics for %s.%s: %s", svc.Name, svc.Namespace, err))
			continue
		}
		table := rsp.GetOk().GetGatewaysTable()
		if table == nil {
			errors = append(errors, fmt.Errorf("failed to fetch gateway metrics for %s.%s: %s", svc.Name, svc.Namespace, rsp.GetError().GetError()))
			continue
		}
		if len(table.Rows) != 1 {
			errors = append(errors, fmt.Errorf("wrong number of (%d) gateway metrics entries for %s.%s", len(table.Rows), svc.Name, svc.Namespace))
			continue
		}
		row := table.Rows[0]
		if !row.Alive {
			errors = append(errors, fmt.Errorf("liveness checks failed for %s", link.TargetClusterName))
			continue
		}
		links = append(links, fmt.Sprintf("\t* %s", link.TargetClusterName))
	}
	if len(errors) > 0 {
		return joinErrors(errors, 1)
	}
	if len(links) == 0 {
		return &healthcheck.SkipError{Reason: "no links"}
	}
	return &healthcheck.VerboseSuccess{Message: strings.Join(links, "\n")}
}

func (hc *healthChecker) checkIfMirrorServicesHaveEndpoints(ctx context.Context) error {
	var servicesWithNoEndpoints []string
	selector := fmt.Sprintf("%s, !%s", k8s.MirroredResourceLabel, k8s.MirroredGatewayLabel)
	mirrorServices, err := hc.KubeAPIClient().CoreV1().Services(metav1.NamespaceAll).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	for _, svc := range mirrorServices.Items {
		// Check if there is a relevant end-point
		endpoint, err := hc.KubeAPIClient().CoreV1().Endpoints(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
		if err != nil || len(endpoint.Subsets) == 0 {
			servicesWithNoEndpoints = append(servicesWithNoEndpoints, fmt.Sprintf("%s.%s mirrored from cluster [%s]", svc.Name, svc.Namespace, svc.Labels[k8s.RemoteClusterNameLabel]))
		}
	}
	if len(servicesWithNoEndpoints) > 0 {
		return fmt.Errorf("Some mirror services do not have endpoints:\n    %s", strings.Join(servicesWithNoEndpoints, "\n    "))
	}
	if len(mirrorServices.Items) == 0 {
		return &healthcheck.SkipError{Reason: "no mirror services"}
	}
	return nil
}

func (hc *healthChecker) checkForOrphanedServices(ctx context.Context) error {
	errors := []error{}
	selector := fmt.Sprintf("%s, !%s", k8s.MirroredResourceLabel, k8s.MirroredGatewayLabel)
	mirrorServices, err := hc.KubeAPIClient().CoreV1().Services(metav1.NamespaceAll).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	links, err := multicluster.GetLinks(ctx, hc.KubeAPIClient().DynamicClient)
	if err != nil {
		return err
	}
	for _, svc := range mirrorServices.Items {
		targetCluster := svc.Labels[k8s.RemoteClusterNameLabel]
		hasLink := false
		for _, link := range links {
			if link.TargetClusterName == targetCluster {
				hasLink = true
				break
			}
		}
		if !hasLink {
			errors = append(errors, fmt.Errorf("mirror service %s.%s is not part of any Link", svc.Name, svc.Namespace))
		}
	}
	if len(mirrorServices.Items) == 0 {
		return &healthcheck.SkipError{Reason: "no mirror services"}
	}
	if len(errors) > 0 {
		return joinErrors(errors, 1)
	}
	return nil
}

func joinErrors(errs []error, tabDepth int) error {
	indent := strings.Repeat("    ", tabDepth)
	errStrings := []string{}
	for _, err := range errs {
		errStrings = append(errStrings, indent+err.Error())
	}
	return errors.New(strings.Join(errStrings, "\n"))
}

func serviceMirrorComponentsSelector(targetCluster string) string {
	return fmt.Sprintf("%s=%s,%s=%s",
		k8s.ControllerComponentLabel, linkerdServiceMirrorComponentName,
		k8s.RemoteClusterNameLabel, targetCluster)
}
