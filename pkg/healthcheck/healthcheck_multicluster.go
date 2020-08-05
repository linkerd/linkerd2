package healthcheck

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/multicluster"
	"github.com/linkerd/linkerd2/pkg/servicemirror"
	corev1 "k8s.io/api/core/v1"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// LinkerdMulticlusterChecks adds a series of checks to validate a
	// multicluster setup.
	LinkerdMulticlusterChecks CategoryID = "linkerd-multicluster"

	linkerdServiceMirrorComponentName      = "linkerd-service-mirror"
	linkerdServiceMirrorSerivceAccountName = "linkerd-service-mirror-%s"
	linkerdServiceMirrorClusterRoleName    = "linkerd-service-mirror-access-local-resources-%s"
	linkerdServiceMirrorRoleName           = "linkerd-service-mirror-read-remote-creds-%s"
)

var expectedServiceMirrorRemoteClusterPolicyVerbs = []string{
	"get",
	"list",
	"watch",
}

func (hc *HealthChecker) multiClusterCategory() []category {
	return []category{
		{
			id: LinkerdMulticlusterChecks,
			checkers: []checker{
				/* Link checks */
				{
					description: "Link CRD exists",
					hintAnchor:  "l5d-multicluster-link-crd-exists",
					fatal:       true,
					check: func(context.Context) error {
						return hc.checkLinkCRD()
					},
				},
				{
					description: "Link resources are valid",
					hintAnchor:  "l5d-multicluster-links-are-valid",
					fatal:       true,
					check: func(context.Context) error {
						if hc.Options.MultiCluster {
							return hc.checkLinks()
						}
						return &SkipError{Reason: "not checking muticluster"}
					},
				},
				/* Serivce mirror controller checks */
				{
					description: "service mirror controller has required permissions",
					hintAnchor:  "l5d-multicluster-source-rbac-correct",
					check: func(context.Context) error {
						if hc.Options.MultiCluster {
							return hc.checkServiceMirrorLocalRBAC()
						}
						return &SkipError{Reason: "not checking muticluster"}
					},
				},
				{
					description:         "service mirror controllers are running",
					hintAnchor:          "l5d-multicluster-service-mirror-running",
					retryDeadline:       hc.RetryDeadline,
					surfaceErrorOnRetry: true,
					check: func(context.Context) error {
						if hc.Options.MultiCluster {
							return hc.checkServiceMirrorController()
						}
						return &SkipError{Reason: "not checking muticluster"}
					},
				},
				/* Target cluster access checks */
				{
					description: "remote cluster access credentials are valid",
					hintAnchor:  "l5d-smc-target-clusters-access",
					check: func(context.Context) error {
						if hc.Options.MultiCluster {
							return hc.checkRemoteClusterConnectivity()
						}
						return &SkipError{Reason: "not checking muticluster"}
					},
				},
				{
					description: "clusters share trust anchors",
					hintAnchor:  "l5d-multicluster-clusters-share-anchors",
					check: func(ctx context.Context) error {
						if hc.Options.MultiCluster {
							return hc.checkRemoteClusterAnchors()
						}
						return &SkipError{Reason: "not checking muticluster"}
					},
				},
				/* Gateway mirror checks */
				{
					description: "all gateway mirrors are healthy",
					hintAnchor:  "l5d-multicluster-gateways-endpoints",
					check: func(ctx context.Context) error {
						if hc.Options.MultiCluster {
							return hc.checkIfGatewayMirrorsHaveEndpoints(ctx)
						}
						return &SkipError{Reason: "not checking muticluster"}
					},
				},
				/* Mirror service checks */
				{
					description: "all mirror services have endpoints",
					hintAnchor:  "l5d-multicluster-services-endpoints",
					check: func(ctx context.Context) error {
						if hc.Options.MultiCluster {
							return hc.checkIfMirrorServicesHaveEndpoints()
						}
						return &SkipError{Reason: "not checking muticluster"}
					},
				},
				{
					description: "all mirror services are part of a Link",
					hintAnchor:  "l5d-multicluster-orphaned-services",
					warning:     true,
					check: func(ctx context.Context) error {
						if hc.Options.MultiCluster {
							return hc.checkForOrphanedServices()
						}
						return &SkipError{Reason: "not checking muticluster"}
					},
				},
			},
		},
	}
}

/* Link checks */

func (hc *HealthChecker) checkLinkCRD() error {
	err := k8s.LinkAccess(hc.kubeAPI.Interface)
	if err == nil {
		hc.Options.MultiCluster = true
		return nil
	}
	if !hc.Options.MultiCluster {
		return &SkipError{Reason: "not checking muticluster"}
	}
	return fmt.Errorf("multicluster.linkerd.io/Link CRD is missing: %s", err)
}

func (hc *HealthChecker) checkLinks() error {
	links, err := multicluster.GetLinks(hc.kubeAPI.DynamicClient)
	if err != nil {
		return err
	}
	if len(links) == 0 {
		return &SkipError{Reason: "no links detected"}
	}
	linkNames := []string{}
	for _, l := range links {
		linkNames = append(linkNames, fmt.Sprintf("\t* %s", l.TargetClusterName))
	}
	hc.links = links
	return &VerboseSuccess{Message: strings.Join(linkNames, "\n")}
}

/* Serivce mirror controller checks */

func (hc *HealthChecker) checkServiceMirrorLocalRBAC() error {
	links := []string{}
	errors := []string{}

	for _, link := range hc.links {

		err := hc.checkServiceAccounts(
			[]string{fmt.Sprintf(linkerdServiceMirrorSerivceAccountName, link.TargetClusterName)},
			link.Namespace,
			serviceMirrorComponentsSelector(link.TargetClusterName),
		)
		if err != nil {
			errors = append(errors, err.Error())
		}

		err = hc.checkClusterRoles(
			true,
			[]string{fmt.Sprintf(linkerdServiceMirrorClusterRoleName, link.TargetClusterName)},
			serviceMirrorComponentsSelector(link.TargetClusterName),
		)
		if err != nil {
			errors = append(errors, err.Error())
		}

		err = hc.checkClusterRoleBindings(
			true,
			[]string{fmt.Sprintf(linkerdServiceMirrorClusterRoleName, link.TargetClusterName)},
			serviceMirrorComponentsSelector(link.TargetClusterName),
		)
		if err != nil {
			errors = append(errors, err.Error())
		}

		err = hc.checkRoles(
			true,
			link.Namespace,
			[]string{fmt.Sprintf(linkerdServiceMirrorRoleName, link.TargetClusterName)},
			serviceMirrorComponentsSelector(link.TargetClusterName),
		)
		if err != nil {
			errors = append(errors, err.Error())
		}

		err = hc.checkRoleBindings(
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
		return &SkipError{Reason: "no links"}
	}

	return &VerboseSuccess{Message: strings.Join(links, "\n")}
}

func (hc *HealthChecker) checkServiceMirrorController() error {

	errors := []error{}
	clusterNames := []string{}

	for _, link := range hc.links {
		options := metav1.ListOptions{
			LabelSelector: serviceMirrorComponentsSelector(link.TargetClusterName),
		}
		result, err := hc.kubeAPI.AppsV1().Deployments(corev1.NamespaceAll).List(options)
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
		return &SkipError{Reason: "no links"}
	}

	return &VerboseSuccess{Message: strings.Join(clusterNames, "\n")}
}

/* Target cluster access checks */

func (hc *HealthChecker) checkRemoteClusterConnectivity() error {
	errors := []error{}
	links := []string{}
	for _, link := range hc.links {
		// Load the credentials secret
		secret, err := hc.kubeAPI.Interface.CoreV1().Secrets(link.Namespace).Get(link.ClusterCredentialsSecret, metav1.GetOptions{})
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

		remoteAPI, err := k8s.NewAPIForConfig(clientConfig, "", []string{}, requestTimeout)
		if err != nil {
			errors = append(errors, fmt.Errorf("* secret: [%s/%s] cluster: [%s]: could not instantiate api for target cluster: %s", secret.Namespace, secret.Name, link.TargetClusterName, err))
			continue
		}

		var verbs []string
		if err := hc.checkCanPerformAction(remoteAPI, "get", corev1.NamespaceAll, "", "v1", "services"); err == nil {
			verbs = append(verbs, "get")
		}

		if err := hc.checkCanPerformAction(remoteAPI, "list", corev1.NamespaceAll, "", "v1", "services"); err == nil {
			verbs = append(verbs, "list")
		}

		if err := hc.checkCanPerformAction(remoteAPI, "watch", corev1.NamespaceAll, "", "v1", "services"); err == nil {
			verbs = append(verbs, "watch")
		}

		if err := comparePermissions(expectedServiceMirrorRemoteClusterPolicyVerbs, verbs); err != nil {
			errors = append(errors, fmt.Errorf("* cluster: [%s]: Insufficient Service permissions: %s", link.TargetClusterName, err))
		}

		links = append(links, fmt.Sprintf("\t* %s", link.TargetClusterName))
	}

	if len(errors) > 0 {
		return joinErrors(errors, 2)
	}

	if len(links) == 0 {
		return &SkipError{Reason: "no links"}
	}

	return &VerboseSuccess{Message: strings.Join(links, "\n")}
}

func (hc *HealthChecker) checkRemoteClusterAnchors() error {
	localAnchors, err := tls.DecodePEMCertificates(hc.linkerdConfig.Global.IdentityContext.TrustAnchorsPem)
	if err != nil {
		return fmt.Errorf("Cannot parse source trust anchors: %s", err)
	}
	errors := []string{}
	links := []string{}
	for _, link := range hc.links {
		// Load the credentials secret
		secret, err := hc.kubeAPI.Interface.CoreV1().Secrets(link.Namespace).Get(link.ClusterCredentialsSecret, metav1.GetOptions{})
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

		remoteAPI, err := k8s.NewAPIForConfig(clientConfig, "", []string{}, requestTimeout)
		if err != nil {
			errors = append(errors, fmt.Sprintf("* secret: [%s/%s] cluster: [%s]: could not instantiate api for target cluster: %s", secret.Namespace, secret.Name, link.TargetClusterName, err))
			continue
		}

		_, cfMap, err := FetchLinkerdConfigMap(remoteAPI, link.TargetClusterLinkerdNamespace)
		if err != nil {
			errors = append(errors, fmt.Sprintf("* %s: unable to fetch anchors: %s", link.TargetClusterName, err))
			continue
		}

		remoteAnchors, err := tls.DecodePEMCertificates(cfMap.Global.IdentityContext.TrustAnchorsPem)
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
		return &SkipError{Reason: "no links"}
	}

	return &VerboseSuccess{Message: strings.Join(links, "\n")}
}

/* Gateway mirror checks */

func (hc *HealthChecker) checkIfGatewayMirrorsHaveEndpoints(ctx context.Context) error {
	links := []string{}
	errors := []error{}

	for _, link := range hc.links {
		selector := metav1.ListOptions{LabelSelector: fmt.Sprintf("%s,%s=%s", k8s.MirroredGatewayLabel, k8s.RemoteClusterNameLabel, link.TargetClusterName)}
		gatewayMirrors, err := hc.kubeAPI.CoreV1().Services(metav1.NamespaceAll).List(selector)
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
		endpoints, err := hc.kubeAPI.CoreV1().Endpoints(svc.Namespace).Get(svc.Name, metav1.GetOptions{})
		if err != nil || len(endpoints.Subsets) == 0 {
			errors = append(errors, fmt.Errorf("%s.%s mirrored from cluster [%s] has no endpoints", svc.Name, svc.Namespace, svc.Labels[k8s.RemoteClusterNameLabel]))
			continue
		}

		// Check gateway liveness according to probes
		req := public.GatewaysRequest{
			TimeWindow:        "1m",
			RemoteClusterName: link.TargetClusterName,
		}
		rsp, err := hc.apiClient.Gateways(ctx, &req)
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
		return &SkipError{Reason: "no links"}
	}

	return &VerboseSuccess{Message: strings.Join(links, "\n")}
}

/* Mirror service checks */

func (hc *HealthChecker) checkIfMirrorServicesHaveEndpoints() error {

	var servicesWithNoEndpoints []string
	selector := fmt.Sprintf("%s, !%s", k8s.MirroredResourceLabel, k8s.MirroredGatewayLabel)
	mirrorServices, err := hc.kubeAPI.CoreV1().Services(metav1.NamespaceAll).List(metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}

	for _, svc := range mirrorServices.Items {
		// Check if there is a relevant end-point
		endpoint, err := hc.kubeAPI.CoreV1().Endpoints(svc.Namespace).Get(svc.Name, metav1.GetOptions{})
		if err != nil || len(endpoint.Subsets) == 0 {
			servicesWithNoEndpoints = append(servicesWithNoEndpoints, fmt.Sprintf("%s.%s mirrored from cluster [%s]", svc.Name, svc.Namespace, svc.Labels[k8s.RemoteClusterNameLabel]))
		}
	}

	if len(servicesWithNoEndpoints) > 0 {
		return fmt.Errorf("Some mirror services do not have endpoints:\n    %s", strings.Join(servicesWithNoEndpoints, "\n    "))
	}

	if len(mirrorServices.Items) == 0 {
		return &SkipError{Reason: "no mirror services"}
	}

	return nil
}

func (hc *HealthChecker) checkForOrphanedServices() error {
	errors := []error{}

	selector := fmt.Sprintf("%s, !%s", k8s.MirroredResourceLabel, k8s.MirroredGatewayLabel)
	mirrorServices, err := hc.kubeAPI.CoreV1().Services(metav1.NamespaceAll).List(metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}

	links, err := multicluster.GetLinks(hc.kubeAPI.DynamicClient)
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
		return &SkipError{Reason: "no mirror services"}
	}

	if len(errors) > 0 {
		return joinErrors(errors, 1)
	}
	return nil
}

/* util */

func serviceMirrorComponentsSelector(targetCluster string) string {
	return fmt.Sprintf("%s=%s,%s=%s",
		k8s.ControllerComponentLabel, linkerdServiceMirrorComponentName,
		k8s.RemoteClusterNameLabel, targetCluster)
}

func joinErrors(errs []error, tabDepth int) error {
	indent := strings.Repeat("    ", tabDepth)
	errStrings := []string{}
	for _, err := range errs {
		errStrings = append(errStrings, indent+err.Error())
	}
	return errors.New(strings.Join(errStrings, "\n"))
}

func comparePermissions(expected, actual []string) error {
	sort.Strings(expected)
	sort.Strings(actual)

	expectedStr := strings.Join(expected, ",")
	actualStr := strings.Join(actual, ",")

	if expectedStr != actualStr {
		return fmt.Errorf("expected %s, got %s", expectedStr, actualStr)
	}

	return nil
}
