package cmd

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/linkerd/linkerd2/controller/gen/apis/link/v1alpha3"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/servicemirror"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/prometheus/common/expfmt"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// MulticlusterExtensionName is the name of the multicluster extension
	MulticlusterExtensionName = "multicluster"

	// MulticlusterLegacyExtension is the name of the multicluster extension
	// prior to stable-2.10.0 when the linkerd prefix was removed.
	MulticlusterLegacyExtension = "linkerd-multicluster"

	// LinkerdMulticlusterExtensionCheck adds checks related to the multicluster extension
	LinkerdMulticlusterExtensionCheck healthcheck.CategoryID = "linkerd-multicluster"
)

// For these vars, the second name is for service mirror controllers
// managed by the linkerd-multicluster chart
var (
	linkerdServiceMirrorServiceAccountNames = []string{"linkerd-service-mirror-%s", "controller-%s"}
	linkerdServiceMirrorComponentNames      = []string{"service-mirror", "controller"}

	linkerdServiceMirrorClusterRoleNames = []string{
		"linkerd-service-mirror-access-local-resources-%s",
		"linkerd-multicluster-controller-access-local-resources",
	}
	linkerdServiceMirrorRoleNames = []string{
		"linkerd-service-mirror-read-remote-creds-%s",
		"controller-read-remote-creds-%s",
	}
)

type checkOptions struct {
	wait    time.Duration
	output  string
	timeout time.Duration
}

func newCheckOptions() *checkOptions {
	return &checkOptions{
		wait:    300 * time.Second,
		output:  healthcheck.TableOutput,
		timeout: 10 * time.Second,
	}
}

func (options *checkOptions) validate() error {
	if options.output != healthcheck.TableOutput && options.output != healthcheck.JSONOutput && options.output != healthcheck.ShortOutput {
		return fmt.Errorf("Invalid output type '%s'. Supported output types are: %s, %s, %s", options.output, healthcheck.JSONOutput, healthcheck.TableOutput, healthcheck.ShortOutput)
	}
	return nil
}

type healthChecker struct {
	*healthcheck.HealthChecker
	links []v1alpha3.Link
}

func newHealthChecker(linkerdHC *healthcheck.HealthChecker) *healthChecker {
	return &healthChecker{
		linkerdHC,
		[]v1alpha3.Link{},
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
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to run multicluster check: %s\n", err)
				os.Exit(1)
			}

			_, err = kubeAPI.GetNamespaceWithExtensionLabel(context.Background(), MulticlusterExtensionName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s; install by running `linkerd multicluster install | kubectl apply -f -`\n", err)
				os.Exit(1)
			}
			return configureAndRunChecks(stdout, stderr, options)
		},
	}
	cmd.Flags().StringVarP(&options.output, "output", "o", options.output, "Output format. One of: table, json, short")
	cmd.Flags().DurationVar(&options.wait, "wait", options.wait, "Maximum allowed time for all tests to pass")
	cmd.Flags().DurationVar(&options.timeout, "timeout", options.timeout, "Timeout for calls to the Kubernetes API")
	cmd.Flags().Bool("proxy", false, "")
	cmd.Flags().MarkHidden("proxy")
	cmd.Flags().StringP("namespace", "n", "", "")
	cmd.Flags().MarkHidden("namespace")

	pkgcmd.ConfigureNamespaceFlagCompletion(
		cmd, []string{"namespace"},
		kubeconfigPath, impersonate, impersonateGroup, kubeContext)
	pkgcmd.ConfigureOutputFlagCompletion(cmd)

	return cmd
}

func configureAndRunChecks(wout io.Writer, werr io.Writer, options *checkOptions) error {
	err := options.validate()
	if err != nil {
		return fmt.Errorf("Validation error when executing check command: %w", err)
	}
	checks := []healthcheck.CategoryID{
		LinkerdMulticlusterExtensionCheck,
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
		fmt.Fprintf(werr, "Error initializing k8s API client: %s\n", err)
		os.Exit(1)
	}

	err = linkerdHC.InitializeLinkerdGlobalConfig(context.Background())
	if err != nil {
		fmt.Fprintf(werr, "Failed to fetch linkerd config: %s\n", err)
		os.Exit(1)
	}

	hc := newHealthChecker(linkerdHC)
	category := multiclusterCategory(hc, options.timeout)
	hc.AppendCategories(category)
	success, warning := healthcheck.RunChecks(wout, werr, hc, options.output)
	healthcheck.PrintChecksResult(wout, options.output, success, warning)
	if !success {
		os.Exit(1)
	}
	return nil
}

func multiclusterCategory(hc *healthChecker, wait time.Duration) *healthcheck.Category {
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
		*healthcheck.NewChecker("Link and CLI versions match").
			WithHintAnchor("l5d-multicluster-links-version").
			Warning().
			WithCheck(func(ctx context.Context) error { return hc.checkLinkVersions() }))
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
					return fmt.Errorf("Cannot parse source trust anchors: %w", err)
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
		*healthcheck.NewChecker("probe services able to communicate with all gateway mirrors").
			WithHintAnchor("l5d-multicluster-gateways-endpoints").
			WithCheck(func(ctx context.Context) error {
				return hc.checkIfGatewayMirrorsHaveEndpoints(ctx, wait)
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
			Warning().
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

	return healthcheck.NewCategory(LinkerdMulticlusterExtensionCheck, checkers, true)
}

func (hc *healthChecker) checkLinkCRD(ctx context.Context) error {
	err := hc.linkAccess(ctx)
	if err != nil {
		return fmt.Errorf("multicluster.linkerd.io/Link CRD is missing: %w", err)
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
	links, err := hc.KubeAPIClient().L5dCrdClient.LinkV1alpha3().Links("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if len(links.Items) == 0 {
		return healthcheck.SkipError{Reason: "no links detected"}
	}
	linkNames := []string{}
	for _, l := range links.Items {
		linkNames = append(linkNames, fmt.Sprintf("\t* %s", l.Spec.TargetClusterName))
	}
	hc.links = links.Items
	return healthcheck.VerboseSuccess{Message: strings.Join(linkNames, "\n")}
}

func (hc *healthChecker) checkLinkVersions() error {
	errors := []error{}
	links := []string{}
	for _, link := range hc.links {
		parts := strings.Split(link.Annotations[k8s.CreatedByAnnotation], " ")
		if len(parts) == 2 && parts[0] == "linkerd/cli" {
			if parts[1] == version.Version {
				links = append(links, fmt.Sprintf("\t* %s", link.Spec.TargetClusterName))
			} else {
				errors = append(errors, fmt.Errorf("* %s: CLI version is %s but Link version is %s", link.Spec.TargetClusterName, version.Version, parts[1]))
			}
		} else {
			errors = append(errors, fmt.Errorf("* %s: unable to determine version", link.Spec.TargetClusterName))
		}
	}
	if len(errors) > 0 {
		return joinErrors(errors, 2)
	}
	if len(links) == 0 {
		return healthcheck.SkipError{Reason: "no links"}
	}
	return healthcheck.VerboseSuccess{Message: strings.Join(links, "\n")}
}

func (hc *healthChecker) checkRemoteClusterConnectivity(ctx context.Context) error {
	errors := []error{}
	links := []string{}
	for _, link := range hc.links {
		// Load the credentials secret
		secret, err := hc.KubeAPIClient().Interface.CoreV1().Secrets(link.Namespace).Get(ctx, link.Spec.ClusterCredentialsSecret, metav1.GetOptions{})
		if err != nil {
			errors = append(errors, fmt.Errorf("* secret: [%s/%s]: %w", link.Namespace, link.Spec.ClusterCredentialsSecret, err))
			continue
		}
		config, err := servicemirror.ParseRemoteClusterSecret(secret)
		if err != nil {
			errors = append(errors, fmt.Errorf("* secret: [%s/%s]: could not parse config secret: %w", secret.Namespace, secret.Name, err))
			continue
		}
		clientConfig, err := clientcmd.RESTConfigFromKubeConfig(config)
		if err != nil {
			errors = append(errors, fmt.Errorf("* secret: [%s/%s] cluster: [%s]: unable to parse api config: %w", secret.Namespace, secret.Name, link.Spec.TargetClusterName, err))
			continue
		}
		remoteAPI, err := k8s.NewAPIForConfig(clientConfig, "", []string{}, healthcheck.RequestTimeout, 0, 0)
		if err != nil {
			errors = append(errors, fmt.Errorf("* secret: [%s/%s] cluster: [%s]: could not instantiate api for target cluster: %w", secret.Namespace, secret.Name, link.Spec.TargetClusterName, err))
			continue
		}
		// We use this call just to check connectivity.
		_, err = remoteAPI.Discovery().ServerVersion()
		if err != nil {
			errors = append(errors, fmt.Errorf("* failed to connect to API for cluster: [%s]: %w", link.Spec.TargetClusterName, err))
			continue
		}
		verbs := []string{"get", "list", "watch"}
		for _, verb := range verbs {
			if err := healthcheck.CheckCanPerformAction(ctx, remoteAPI, verb, corev1.NamespaceAll, "", "v1", "services"); err != nil {
				errors = append(errors, fmt.Errorf("* missing service permission [%s] for cluster [%s]: %w", verb, link.Spec.TargetClusterName, err))
			}
		}
		links = append(links, fmt.Sprintf("\t* %s", link.Spec.TargetClusterName))
	}
	if len(errors) > 0 {
		return joinErrors(errors, 2)
	}
	if len(links) == 0 {
		return healthcheck.SkipError{Reason: "no links"}
	}
	return healthcheck.VerboseSuccess{Message: strings.Join(links, "\n")}
}

func (hc *healthChecker) checkRemoteClusterAnchors(ctx context.Context, localAnchors []*x509.Certificate) error {
	errors := []string{}
	links := []string{}
	for _, link := range hc.links {
		// Load the credentials secret
		secret, err := hc.KubeAPIClient().Interface.CoreV1().Secrets(link.Namespace).Get(ctx, link.Spec.ClusterCredentialsSecret, metav1.GetOptions{})
		if err != nil {
			errors = append(errors, fmt.Sprintf("* secret: [%s/%s]: %s", link.Namespace, link.Spec.ClusterCredentialsSecret, err))
			continue
		}
		config, err := servicemirror.ParseRemoteClusterSecret(secret)
		if err != nil {
			errors = append(errors, fmt.Sprintf("* secret: [%s/%s]: could not parse config secret: %s", secret.Namespace, secret.Name, err))
			continue
		}
		clientConfig, err := clientcmd.RESTConfigFromKubeConfig(config)
		if err != nil {
			errors = append(errors, fmt.Sprintf("* secret: [%s/%s] cluster: [%s]: unable to parse api config: %s", secret.Namespace, secret.Name, link.Spec.TargetClusterName, err))
			continue
		}
		remoteAPI, err := k8s.NewAPIForConfig(clientConfig, "", []string{}, healthcheck.RequestTimeout, 0, 0)
		if err != nil {
			errors = append(errors, fmt.Sprintf("* secret: [%s/%s] cluster: [%s]: could not instantiate api for target cluster: %s", secret.Namespace, secret.Name, link.Spec.TargetClusterName, err))
			continue
		}
		_, values, err := healthcheck.FetchCurrentConfiguration(ctx, remoteAPI, link.Spec.TargetClusterLinkerdNamespace)
		if err != nil {
			errors = append(errors, fmt.Sprintf("* %s: unable to fetch anchors: %s", link.Spec.TargetClusterName, err))
			continue
		}
		remoteAnchors, err := tls.DecodePEMCertificates(values.IdentityTrustAnchorsPEM)
		if err != nil {
			errors = append(errors, fmt.Sprintf("* %s: cannot parse trust anchors", link.Spec.TargetClusterName))
			continue
		}
		// we fail early if the lens are not the same. If they are the
		// same, we can only compare certs one way and be sure we have
		// identical anchors
		if len(remoteAnchors) != len(localAnchors) {
			errors = append(errors, fmt.Sprintf("* %s", link.Spec.TargetClusterName))
			continue
		}
		localAnchorsMap := make(map[string]*x509.Certificate)
		for _, c := range localAnchors {
			localAnchorsMap[string(c.Signature)] = c
		}
		for _, remote := range remoteAnchors {
			local, ok := localAnchorsMap[string(remote.Signature)]
			if !ok || !local.Equal(remote) {
				errors = append(errors, fmt.Sprintf("* %s", link.Spec.TargetClusterName))
				break
			}
		}
		links = append(links, fmt.Sprintf("\t* %s", link.Spec.TargetClusterName))
	}
	if len(errors) > 0 {
		return fmt.Errorf("Problematic clusters:\n    %s", strings.Join(errors, "\n    "))
	}
	if len(links) == 0 {
		return healthcheck.SkipError{Reason: "no links"}
	}
	return healthcheck.VerboseSuccess{Message: strings.Join(links, "\n")}
}

func (hc *healthChecker) checkServiceMirrorLocalRBAC(ctx context.Context) error {
	links := []string{}
	messages := []string{}
	for _, link := range hc.links {
		err := healthcheck.CheckServiceAccounts(
			ctx,
			hc.KubeAPIClient(),
			[]string{fmt.Sprintf(linkerdServiceMirrorServiceAccountNames[0], link.Spec.TargetClusterName)},
			link.Namespace,
			serviceMirrorComponentsSelector(link.Spec.TargetClusterName),
		)
		if err != nil {
			err2 := healthcheck.CheckServiceAccounts(
				ctx,
				hc.KubeAPIClient(),
				[]string{fmt.Sprintf(linkerdServiceMirrorServiceAccountNames[1], link.Spec.TargetClusterName)},
				link.Namespace,
				serviceMirrorComponentsSelector(link.Spec.TargetClusterName),
			)
			if err2 != nil {
				messages = append(messages, err.Error(), err2.Error())
			}
		}
		err = healthcheck.CheckClusterRoles(
			ctx,
			hc.KubeAPIClient(),
			true,
			[]string{fmt.Sprintf(linkerdServiceMirrorClusterRoleNames[0], link.Spec.TargetClusterName)},
			serviceMirrorComponentsSelector(link.Spec.TargetClusterName),
		)
		if err != nil {
			err2 := healthcheck.CheckClusterRoles(
				ctx,
				hc.KubeAPIClient(),
				true,
				[]string{linkerdServiceMirrorClusterRoleNames[1]},
				"component=controller",
			)
			if err2 != nil {
				messages = append(messages, err.Error(), err2.Error())
			}
		}
		err = healthcheck.CheckClusterRoleBindings(
			ctx,
			hc.KubeAPIClient(),
			true,
			[]string{fmt.Sprintf(linkerdServiceMirrorClusterRoleNames[0], link.Spec.TargetClusterName)},
			serviceMirrorComponentsSelector(link.Spec.TargetClusterName),
		)
		if err != nil {
			err2 := healthcheck.CheckClusterRoleBindings(
				ctx,
				hc.KubeAPIClient(),
				true,
				[]string{fmt.Sprintf("%s-%s", linkerdServiceMirrorClusterRoleNames[1], link.Spec.TargetClusterName)},
				serviceMirrorComponentsSelector(link.Spec.TargetClusterName),
			)
			if err2 != nil {
				messages = append(messages, err.Error(), err2.Error())
			}
		}
		err = healthcheck.CheckRoles(
			ctx,
			hc.KubeAPIClient(),
			true,
			link.Namespace,
			[]string{fmt.Sprintf(linkerdServiceMirrorRoleNames[0], link.Spec.TargetClusterName)},
			serviceMirrorComponentsSelector(link.Spec.TargetClusterName),
		)
		if err != nil {
			err2 := healthcheck.CheckRoles(
				ctx,
				hc.KubeAPIClient(),
				true,
				link.Namespace,
				[]string{fmt.Sprintf(linkerdServiceMirrorRoleNames[1], link.Spec.TargetClusterName)},
				serviceMirrorComponentsSelector(link.Spec.TargetClusterName),
			)
			if err2 != nil {
				messages = append(messages, err.Error(), err2.Error())
			}
		}
		err = healthcheck.CheckRoleBindings(
			ctx,
			hc.KubeAPIClient(),
			true,
			link.Namespace,
			[]string{fmt.Sprintf(linkerdServiceMirrorRoleNames[0], link.Spec.TargetClusterName)},
			serviceMirrorComponentsSelector(link.Spec.TargetClusterName),
		)
		if err != nil {
			err2 := healthcheck.CheckRoleBindings(
				ctx,
				hc.KubeAPIClient(),
				true,
				link.Namespace,
				[]string{fmt.Sprintf(linkerdServiceMirrorRoleNames[1], link.Spec.TargetClusterName)},
				serviceMirrorComponentsSelector(link.Spec.TargetClusterName),
			)
			if err2 != nil {
				messages = append(messages, err.Error(), err2.Error())
			}
		}
		links = append(links, fmt.Sprintf("\t* %s", link.Spec.TargetClusterName))
	}
	if len(messages) > 0 {
		return errors.New(strings.Join(messages, "\n"))
	}
	if len(links) == 0 {
		return healthcheck.SkipError{Reason: "no links"}
	}
	return healthcheck.VerboseSuccess{Message: strings.Join(links, "\n")}
}

func (hc *healthChecker) checkServiceMirrorController(ctx context.Context) error {
	errors := []error{}
	clusterNames := []string{}
	for _, link := range hc.links {
		options := metav1.ListOptions{
			LabelSelector: serviceMirrorComponentsSelector(link.Spec.TargetClusterName),
		}
		result, err := hc.KubeAPIClient().AppsV1().Deployments(corev1.NamespaceAll).List(ctx, options)
		if err != nil {
			return err
		}
		if len(result.Items) > 1 {
			errors = append(errors, fmt.Errorf("* too many service mirror controller deployments for Link %s", link.Spec.TargetClusterName))
			continue
		}
		if len(result.Items) == 0 {
			errors = append(errors, fmt.Errorf("* no service mirror controller deployment for Link %s", link.Spec.TargetClusterName))
			continue
		}
		controller := result.Items[0]
		if controller.Status.AvailableReplicas < 1 {
			errors = append(errors, fmt.Errorf("* service mirror controller is not available: %s/%s", controller.Namespace, controller.Name))
			continue
		}
		clusterNames = append(clusterNames, fmt.Sprintf("\t* %s", link.Spec.TargetClusterName))
	}
	if len(errors) > 0 {
		return joinErrors(errors, 2)
	}
	if len(clusterNames) == 0 {
		return healthcheck.SkipError{Reason: "no links"}
	}
	return healthcheck.VerboseSuccess{Message: strings.Join(clusterNames, "\n")}
}

func (hc *healthChecker) checkIfGatewayMirrorsHaveEndpoints(ctx context.Context, wait time.Duration) error {
	multiclusterNs, err := hc.KubeAPIClient().GetNamespaceWithExtensionLabel(ctx, MulticlusterExtensionName)
	if err != nil {
		return healthcheck.SkipError{Reason: fmt.Sprintf("failed to find the linkerd-multicluster namespace: %s", err)}
	}

	links := []string{}
	errors := []error{}
	for _, link := range hc.links {
		// When linked against a cluster without a gateway, there will be no
		// gateway address and no probe spec initialised. In such cases, skip
		// the check
		if link.Spec.GatewayAddress == "" || link.Spec.ProbeSpec.Path == "" {
			continue
		}

		// Check that each gateway probe service has endpoints.
		selector := metav1.ListOptions{LabelSelector: fmt.Sprintf("%s,%s=%s", k8s.MirroredGatewayLabel, k8s.RemoteClusterNameLabel, link.Spec.TargetClusterName)}
		gatewayMirrors, err := hc.KubeAPIClient().CoreV1().Services(metav1.NamespaceAll).List(ctx, selector)
		if err != nil {
			errors = append(errors, err)
			continue
		}
		if len(gatewayMirrors.Items) != 1 {
			errors = append(errors, fmt.Errorf("wrong number (%d) of probe gateways for target cluster %s", len(gatewayMirrors.Items), link.Spec.TargetClusterName))
			continue
		}
		svc := gatewayMirrors.Items[0]
		endpoints, err := hc.KubeAPIClient().CoreV1().Endpoints(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
		if err != nil || len(endpoints.Subsets) == 0 {
			errors = append(errors, fmt.Errorf("%s.%s mirrored from cluster [%s] has no endpoints", svc.Name, svc.Namespace, svc.Labels[k8s.RemoteClusterNameLabel]))
			continue
		}

		// Get the service mirror component in the linkerd-multicluster
		// namespace which corresponds to the current link.
		selector = metav1.ListOptions{LabelSelector: fmt.Sprintf("component in(linkerd-service-mirror, controller),mirror.linkerd.io/cluster-name=%s", link.Spec.TargetClusterName)}
		pods, err := hc.KubeAPIClient().CoreV1().Pods(multiclusterNs.Name).List(ctx, selector)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to get the service-mirror component for target cluster %s: %w", link.Spec.TargetClusterName, err))
			continue
		}

		lease, err := hc.KubeAPIClient().CoordinationV1().Leases(multiclusterNs.Name).Get(ctx, fmt.Sprintf("service-mirror-write-%s", link.Spec.TargetClusterName), metav1.GetOptions{})
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to get the service-mirror component Lease for target cluster %s: %w", link.Spec.TargetClusterName, err))
			continue
		}

		// Build a simple lookup table to retrieve Lease object claimant.
		// Metrics should only be pulled from claimants as they are the ones
		// running probes.
		leaders := make(map[string]struct{})
		leaders[*lease.Spec.HolderIdentity] = struct{}{}

		// Get and parse the gateway metrics so that we can extract liveness
		// information.
		gatewayMetrics := getGatewayMetrics(hc.KubeAPIClient(), pods.Items, leaders, wait)
		if len(gatewayMetrics) != 1 {
			errors = append(errors, fmt.Errorf("expected exactly one gateway metric for target cluster %s; got %d", link.Spec.TargetClusterName, len(gatewayMetrics)))
			continue
		}
		var metricsParser expfmt.TextParser
		parsedMetrics, err := metricsParser.TextToMetricFamilies(bytes.NewReader(gatewayMetrics[0].metrics))
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to parse gateway metrics for target cluster %s: %w", link.Spec.TargetClusterName, err))
			continue
		}

		// Ensure the gateway for the current link is alive.
		for _, metrics := range parsedMetrics["gateway_alive"].GetMetric() {
			if !isTargetClusterMetric(metrics, link.Spec.TargetClusterName) {
				continue
			}
			if metrics.GetGauge().GetValue() != 1 {
				err = fmt.Errorf("liveness checks failed for %s", link.Spec.TargetClusterName)
			}
			break
		}
		if err != nil {
			errors = append(errors, err)
			continue
		}
		links = append(links, fmt.Sprintf("\t* %s", link.Spec.TargetClusterName))
	}
	if len(errors) > 0 {
		return joinErrors(errors, 1)
	}
	if len(links) == 0 {
		return healthcheck.SkipError{Reason: "no links"}
	}
	return healthcheck.VerboseSuccess{Message: strings.Join(links, "\n")}
}

func (hc *healthChecker) checkIfMirrorServicesHaveEndpoints(ctx context.Context) error {
	var servicesWithNoEndpoints []string
	selector := fmt.Sprintf("%s, !%s, !%s", k8s.MirroredResourceLabel, k8s.MirroredGatewayLabel, k8s.RemoteDiscoveryLabel)
	mirrorServices, err := hc.KubeAPIClient().CoreV1().Services(metav1.NamespaceAll).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	for _, svc := range mirrorServices.Items {
		if svc.Annotations[k8s.RemoteDiscoveryAnnotation] != "" || svc.Annotations[k8s.LocalDiscoveryAnnotation] != "" {
			// This is a federated service and does not need to have endpoints.
			continue
		}
		// have to use a new ctx for each call, otherwise we risk reaching the original context deadline
		ctx, cancel := context.WithTimeout(context.Background(), healthcheck.RequestTimeout)
		defer cancel()
		endpoint, err := hc.KubeAPIClient().CoreV1().Endpoints(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
		if err != nil || len(endpoint.Subsets) == 0 {
			log.Debugf("error retrieving Endpoints: %s", err)
			servicesWithNoEndpoints = append(servicesWithNoEndpoints, fmt.Sprintf("%s.%s mirrored from cluster [%s]", svc.Name, svc.Namespace, svc.Labels[k8s.RemoteClusterNameLabel]))
		}
	}
	if len(servicesWithNoEndpoints) > 0 {
		return fmt.Errorf("Some mirror services do not have endpoints:\n    %s", strings.Join(servicesWithNoEndpoints, "\n    "))
	}
	if len(mirrorServices.Items) == 0 {
		return healthcheck.SkipError{Reason: "no mirror services"}
	}
	return nil
}

func (hc *healthChecker) checkForOrphanedServices(ctx context.Context) error {
	errors := []error{}
	selector := fmt.Sprintf("%s, !%s, %s", k8s.MirroredResourceLabel, k8s.MirroredGatewayLabel, k8s.RemoteClusterNameLabel)
	mirrorServices, err := hc.KubeAPIClient().CoreV1().Services(metav1.NamespaceAll).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	links, err := hc.KubeAPIClient().L5dCrdClient.LinkV1alpha3().Links("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, svc := range mirrorServices.Items {
		targetCluster := svc.Labels[k8s.RemoteClusterNameLabel]
		hasLink := false
		for _, link := range links.Items {
			if link.Spec.TargetClusterName == targetCluster {
				hasLink = true
				break
			}
		}
		if !hasLink {
			errors = append(errors, fmt.Errorf("mirror service %s.%s is not part of any Link", svc.Name, svc.Namespace))
		}
	}
	if len(mirrorServices.Items) == 0 {
		return healthcheck.SkipError{Reason: "no mirror services"}
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
	return fmt.Sprintf("component in (%s),%s=%s",
		strings.Join(linkerdServiceMirrorComponentNames, ", "),
		k8s.RemoteClusterNameLabel, targetCluster)
}
