package healthcheck

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"sort"
	"strings"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
	sm "github.com/linkerd/linkerd2/pkg/servicemirror"
	tsclient "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	corev1 "k8s.io/api/core/v1"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	v1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// LinkerdMulticlusterChecks adds a series of checks to validate
	// that the multicluster setup is working as expected
	LinkerdMulticlusterChecks CategoryID = "linkerd-multicluster"

	linkerdServiceMirrorComponentName      = "service-mirror"
	linkerdServiceMirrorServiceAccountName = "linkerd-service-mirror"
	linkerdServiceMirrorClusterRoleName    = "linkerd-service-mirror-access-local-resources"
	linkerdServiceMirrorRoleName           = "linkerd-service-mirror-read-remote-creds"
)

var expectedServiceMirrorClusterRolePolicies = []expectedPolicy{
	{
		resources: []string{"endpoints", "services"},
		verbs:     []string{"list", "get", "watch", "create", "delete", "update"},
	},
	{
		resources: []string{"namespaces"},
		verbs:     []string{"create", "list", "get", "watch"},
	},
}

var expectedServiceMirrorRolePolicies = []expectedPolicy{
	{
		resources: []string{"secrets"},
		verbs:     []string{"list", "get", "watch"},
	},
}

var expectedServiceMirrorRemoteClusterPolicyVerbs = []string{
	"get",
	"list",
	"watch",
}

func (hc *HealthChecker) multiClusterCategory() category {
	return category{
		id: LinkerdMulticlusterChecks,
		checkers: []checker{
			{
				description: "service mirror controller is running",
				hintAnchor:  "l5d-multicluster-service-mirror-running",
				fatal:       true,
				check: func(context.Context) error {
					return hc.checkServiceMirrorController()
				},
			},
			{
				description: "service mirror controller ClusterRoles exist",
				hintAnchor:  "l5d-multicluster-cluster-role-exist",
				check: func(context.Context) error {
					if hc.Options.ShouldCheckMulticluster {
						return hc.checkClusterRoles(true, []string{linkerdServiceMirrorClusterRoleName}, hc.serviceMirrorComponentsSelector())
					}
					return &SkipError{Reason: "not checking muticluster"}
				},
			},
			{
				description: "service mirror controller ClusterRoleBindings exist",
				hintAnchor:  "l5d-multicluster-cluster-role-binding-exist",
				check: func(context.Context) error {
					if hc.Options.ShouldCheckMulticluster {
						return hc.checkClusterRoleBindings(true, []string{linkerdServiceMirrorClusterRoleName}, hc.serviceMirrorComponentsSelector())
					}
					return &SkipError{Reason: "not checking muticluster"}
				},
			},
			{
				description: "service mirror controller Roles exist",
				hintAnchor:  "l5d-multicluster-role-exist",
				check: func(context.Context) error {
					if hc.Options.ShouldCheckMulticluster {
						return hc.checkRoles(true, hc.serviceMirrorNs, []string{linkerdServiceMirrorRoleName}, hc.serviceMirrorComponentsSelector())
					}
					return &SkipError{Reason: "not checking muticluster"}
				},
			},
			{
				description: "service mirror controller RoleBindings exist",
				hintAnchor:  "l5d-multicluster-role-binding-exist",
				check: func(context.Context) error {
					if hc.Options.ShouldCheckMulticluster {
						return hc.checkRoleBindings(true, hc.serviceMirrorNs, []string{linkerdServiceMirrorRoleName}, hc.serviceMirrorComponentsSelector())
					}
					return &SkipError{Reason: "not checking muticluster"}
				},
			},
			{
				description: "service mirror controller ServiceAccounts exist",
				hintAnchor:  "l5d-multicluster-service-account-exist",
				check: func(context.Context) error {
					if hc.Options.ShouldCheckMulticluster {
						return hc.checkServiceAccounts([]string{linkerdServiceMirrorServiceAccountName}, hc.serviceMirrorNs, hc.serviceMirrorComponentsSelector())
					}
					return &SkipError{Reason: "not checking muticluster"}
				},
			},
			{
				description: "service mirror controller has required permissions",
				hintAnchor:  "l5d-multicluster-local-rbac-correct",
				check: func(context.Context) error {
					return hc.checkServiceMirrorLocalRBAC()
				},
			},
			{
				description: "service mirror controller can access remote clusters",
				hintAnchor:  "l5d-smc-remote-remote-clusters-access",
				check: func(context.Context) error {
					return hc.checkRemoteClusterConnectivity()
				},
			},
			{
				description: "all remote cluster gateways are alive",
				hintAnchor:  "l5d-multicluster-remote-gateways-alive",
				check: func(ctx context.Context) error {
					return hc.checkRemoteClusterGatewaysHealth(ctx)
				},
			},
			{
				description: "clusters share trust anchors",
				hintAnchor:  "l5d-multicluster-clusters-share-anchors",
				check: func(ctx context.Context) error {
					return hc.checkRemoteClusterAnchors()
				},
			},
			{
				description: "multicluster daisy chaining is avoided",
				hintAnchor:  "l5d-multicluster-daisy-chaining",
				warning:     true,
				check: func(ctx context.Context) error {
					return hc.checkDaisyChains()
				},
			},
		},
	}
}

func (hc *HealthChecker) serviceMirrorComponentsSelector() string {
	return fmt.Sprintf("%s=%s", k8s.ControllerComponentLabel, linkerdServiceMirrorComponentName)
}

func (hc *HealthChecker) checkServiceMirrorController() error {
	options := metav1.ListOptions{
		LabelSelector: hc.serviceMirrorComponentsSelector(),
	}
	result, err := hc.kubeAPI.AppsV1().Deployments(corev1.NamespaceAll).List(options)
	if err != nil {
		return err
	}

	// if we have explicitly requested for multicluster to be checked, error out
	if len(result.Items) == 0 && hc.Options.ShouldCheckMulticluster {
		return errors.New("Service mirror controller is not present")
	}

	if len(result.Items) > 0 {
		hc.Options.ShouldCheckMulticluster = true

		if len(result.Items) > 1 {
			var errors []string
			for _, smc := range result.Items {
				errors = append(errors, fmt.Sprintf("%s/%s", smc.Namespace, smc.Name))
			}
			return fmt.Errorf("There are more than one service mirror controllers:\n\t%s", strings.Join(errors, "\n\t"))
		}

		controller := result.Items[0]
		if controller.Status.AvailableReplicas < 1 {
			return fmt.Errorf("Service mirror controller is not available: %s/%s", controller.Namespace, controller.Name)
		}
		hc.serviceMirrorNs = controller.Namespace
		return nil
	}

	return &SkipError{Reason: "not checking muticluster"}
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

func verifyRule(expected expectedPolicy, actual []v1.PolicyRule) error {
	for _, rule := range actual {
		if err := comparePermissions(expected.resources, rule.Resources); err == nil {
			if err := comparePermissions(expected.verbs, rule.Verbs); err != nil {
				return fmt.Errorf("unexpected verbs %s", err)
			}
			return nil
		}
	}
	return fmt.Errorf("could not fine rule for %s", strings.Join(expected.resources, ","))
}

func (hc *HealthChecker) checkServiceMirrorLocalRBAC() error {
	if hc.Options.ShouldCheckMulticluster {
		var errors []string

		clusterRole, err := hc.kubeAPI.RbacV1().ClusterRoles().Get(linkerdServiceMirrorClusterRoleName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("Could not obtain service mirror ClusterRole %s: %s", linkerdServiceMirrorClusterRoleName, err)
		}

		role, err := hc.kubeAPI.RbacV1().Roles(hc.serviceMirrorNs).Get(linkerdServiceMirrorRoleName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("Could not obtain service mirror Role %s : %s", linkerdServiceMirrorRoleName, err)
		}

		if len(clusterRole.Rules) != len(expectedServiceMirrorClusterRolePolicies) {
			return fmt.Errorf("Service mirror ClusterRole %s has %d policy rules, expected %d", clusterRole.Name, len(clusterRole.Rules), len(expectedServiceMirrorClusterRolePolicies))
		}

		for _, rule := range expectedServiceMirrorClusterRolePolicies {
			if err := verifyRule(rule, clusterRole.Rules); err != nil {
				errors = append(errors, fmt.Sprintf("Service mirror ClusterRole %s: %s", clusterRole.Name, err))
			}
		}

		if len(role.Rules) != len(expectedServiceMirrorRolePolicies) {
			return fmt.Errorf("Service mirror Role %s has %d policy rules, expected %d", role.Name, len(role.Rules), len(expectedServiceMirrorRolePolicies))
		}

		for _, rule := range expectedServiceMirrorRolePolicies {
			if err := verifyRule(rule, role.Rules); err != nil {
				errors = append(errors, fmt.Sprintf("Service mirror Role %s: %s", role.Name, err))
			}
		}

		if len(errors) > 0 {
			return fmt.Errorf(strings.Join(errors, "\n"))
		}

		return nil
	}
	return &SkipError{Reason: "not checking muticluster"}
}

func (hc *HealthChecker) checkRemoteClusterAnchors() error {
	if len(hc.remoteClusterConfigs) == 0 {
		return &SkipError{Reason: "no remote cluster configs"}
	}

	localAnchors, err := tls.DecodePEMCertificates(hc.linkerdConfig.Global.IdentityContext.TrustAnchorsPem)
	if err != nil {
		return fmt.Errorf("Cannot parse local trust anchors: %s", err)
	}

	var offendingClusters []string
	for _, cfg := range hc.remoteClusterConfigs {

		clientConfig, err := clientcmd.RESTConfigFromKubeConfig(cfg.APIConfig)
		if err != nil {
			offendingClusters = append(offendingClusters, fmt.Sprintf("* %s: unable to parse api config", cfg.ClusterName))
			continue
		}

		remoteAPI, err := k8s.NewAPIForConfig(clientConfig, "", []string{}, requestTimeout)
		if err != nil {
			offendingClusters = append(offendingClusters, fmt.Sprintf("* %s: unable to instantiate api", cfg.ClusterName))
			continue
		}

		_, cfMap, err := FetchLinkerdConfigMap(remoteAPI, cfg.LinkerdNamespace)
		if err != nil {
			offendingClusters = append(offendingClusters, fmt.Sprintf("* %s: unable to fetch anchors: %s", cfg.ClusterName, err))
			continue
		}
		remoteAnchors, err := tls.DecodePEMCertificates(cfMap.Global.IdentityContext.TrustAnchorsPem)
		if err != nil {
			offendingClusters = append(offendingClusters, fmt.Sprintf("* %s: cannot parse trust anchors", cfg.ClusterName))
			continue
		}

		// we fail early if the lens are not the same. If they are the
		// same, we can only compare certs one way and be sure we have
		// identical anchors
		if len(remoteAnchors) != len(localAnchors) {
			offendingClusters = append(offendingClusters, fmt.Sprintf("* %s", cfg.ClusterName))
			continue
		}

		localAnchorsMap := make(map[string]*x509.Certificate)
		for _, c := range localAnchors {
			localAnchorsMap[string(c.Signature)] = c
		}

		for _, remote := range remoteAnchors {
			local, ok := localAnchorsMap[string(remote.Signature)]
			if !ok || !local.Equal(remote) {
				offendingClusters = append(offendingClusters, fmt.Sprintf("* %s", cfg.ClusterName))
				break
			}
		}
	}

	if len(offendingClusters) > 0 {
		return fmt.Errorf("Problematic clusters:\n\t%s", strings.Join(offendingClusters, "\n\t"))
	}

	return nil
}

func serviceExported(svc corev1.Service) bool {
	_, hasGtwName := svc.Annotations[k8s.GatewayNameAnnotation]
	_, hasGtwNs := svc.Annotations[k8s.GatewayNsAnnotation]
	return hasGtwName && hasGtwNs
}

func (hc *HealthChecker) checkDaisyChains() error {
	if hc.Options.ShouldCheckMulticluster {
		errs := []error{}

		svcs, err := hc.kubeAPI.CoreV1().Services(metav1.NamespaceAll).List(metav1.ListOptions{})
		if err != nil {
			return err
		}
		for _, svc := range svcs.Items {
			_, isMirror := svc.Labels[k8s.MirroredResourceLabel]
			if isMirror && serviceExported(svc) {
				errs = append(errs, fmt.Errorf("mirror service %s.%s is exported", svc.Name, svc.Namespace))
			}
		}

		ts, err := tsclient.NewForConfig(hc.kubeAPI.Config)
		if err != nil {
			return err
		}
		splits, err := ts.SplitV1alpha1().TrafficSplits(metav1.NamespaceAll).List(metav1.ListOptions{})
		if err != nil {
			return err
		}
		for _, split := range splits.Items {
			apex, err := hc.kubeAPI.CoreV1().Services(split.Namespace).Get(split.Spec.Service, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if serviceExported(*apex) {
				for _, backend := range split.Spec.Backends {
					if backend.Weight.IsZero() {
						continue
					}
					leaf, err := hc.kubeAPI.CoreV1().Services(split.Namespace).Get(backend.Service, metav1.GetOptions{})
					if err != nil {
						return err
					}
					_, isMirror := leaf.Labels[k8s.MirroredResourceLabel]
					if isMirror {
						errs = append(errs, fmt.Errorf("exported service %s.%s routes to mirror service %s.%s via traffic split %s.%s",
							apex.Name, apex.Namespace, leaf.Name, leaf.Namespace, split.Name, split.Namespace,
						))
					}
				}
			}
		}
		if len(errs) > 0 {
			messages := []string{}
			for _, err := range errs {
				messages = append(messages, fmt.Sprintf("* %s", err.Error()))
			}
			return errors.New(strings.Join(messages, "\n"))
		}
		return nil
	}
	return &SkipError{Reason: "not checking muticluster"}
}

func (hc *HealthChecker) checkRemoteClusterConnectivity() error {
	if hc.Options.ShouldCheckMulticluster {
		options := metav1.ListOptions{
			FieldSelector: fmt.Sprintf("%s=%s", "type", k8s.MirrorSecretType),
		}
		secrets, err := hc.kubeAPI.CoreV1().Secrets(corev1.NamespaceAll).List(options)
		if err != nil {
			return err
		}

		if len(secrets.Items) == 0 {
			return &SkipError{Reason: "no remote cluster configs"}
		}

		var errors []string
		for _, s := range secrets.Items {
			secret := s
			config, err := sm.ParseRemoteClusterSecret(&secret)
			if err != nil {
				errors = append(errors, fmt.Sprintf("*  secret: [%s/%s]: could not parse config secret: %s", secret.Namespace, secret.Name, err))
				continue
			}

			clientConfig, err := clientcmd.RESTConfigFromKubeConfig(config.APIConfig)
			if err != nil {
				errors = append(errors, fmt.Sprintf("* secret: [%s/%s] cluster: [%s]: unable to parse api config: %s", secret.Namespace, secret.Name, config.ClusterName, err))
				continue
			}

			remoteAPI, err := k8s.NewAPIForConfig(clientConfig, "", []string{}, requestTimeout)
			if err != nil {
				errors = append(errors, fmt.Sprintf("* secret: [%s/%s] cluster: [%s]: could not instantiate remote api: %s", secret.Namespace, secret.Name, config.ClusterName, err))
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
				errors = append(errors, fmt.Sprintf("* cluster: [%s]: Insufficient Service permissions: %s", config.ClusterName, err))
			}

			hc.remoteClusterConfigs = append(hc.remoteClusterConfigs, config)

		}

		if len(errors) > 0 {
			return fmt.Errorf("Problematic clusters:\n\t%s", strings.Join(errors, "\n\t"))
		}
		return nil
	}
	return &SkipError{Reason: "not checking muticluster"}
}

func (hc *HealthChecker) checkRemoteClusterGatewaysHealth(ctx context.Context) error {
	if hc.Options.ShouldCheckMulticluster {
		if hc.apiClient == nil {
			return errors.New("public api client uninitialized")
		}
		req := &pb.GatewaysRequest{
			TimeWindow: "1m",
		}
		rsp, err := hc.apiClient.Gateways(ctx, req)
		if err != nil {
			return err
		}

		var deadGateways []string
		var aliveGateways []string
		if len(rsp.GetOk().GatewaysTable.Rows) == 0 {
			return &SkipError{Reason: "no remote gateways"}
		}
		for _, gtw := range rsp.GetOk().GatewaysTable.Rows {
			if gtw.Alive {
				aliveGateways = append(aliveGateways, fmt.Sprintf("\t* cluster: [%s], gateway: [%s/%s]", gtw.ClusterName, gtw.Namespace, gtw.Name))
			} else {
				deadGateways = append(deadGateways, fmt.Sprintf("* cluster: [%s], gateway: [%s/%s]", gtw.ClusterName, gtw.Namespace, gtw.Name))
			}
		}

		if len(deadGateways) > 0 {
			return fmt.Errorf("Some gateways are not alive:\n\t%s", strings.Join(deadGateways, "\n\t"))
		}
		return &VerboseSuccess{Message: strings.Join(aliveGateways, "\n")}
	}
	return &SkipError{Reason: "not checking muticluster"}
}
