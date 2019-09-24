package server

import (
	"bytes"
	"errors"
	"fmt"
	pb "github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/sirupsen/logrus"
	"io/ioutil"
	"k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/kubelet/apis/cri"
	criapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/runtime/v1alpha2"
	"k8s.io/kubernetes/pkg/kubelet/remote"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"github.com/linkerd/linkerd2/pkg/k8s"
)

type CRIRuntime struct {
	linkerdNamespace string
	runtimeService cri.RuntimeService
	imageService   cri.ImageManagerService
	httpClient     http.Client
	kubernetes *KubernetesClient
}

func NewCRIRuntime(kubernetes *KubernetesClient, linkerdNamespace string) (*CRIRuntime, error) {
	runtimeService, err := remote.NewRemoteRuntimeService(getRemoteRuntimeEndpoint(), 2*time.Minute)
	if err != nil {
		return nil, err
	}
	imageService, err := remote.NewRemoteImageService(getRemoteRuntimeEndpoint(), 2*time.Minute)
	if err != nil {
		return nil, err
	}

	return &CRIRuntime{
		linkerdNamespace:linkerdNamespace,
		runtimeService: runtimeService,
		imageService:   imageService,
		httpClient:     http.Client{},
		kubernetes: kubernetes,
	}, nil

}

func getRemoteRuntimeEndpoint() string {
	return "unix:///var/run/dockershim.sock"
}


func findSvcAccountSecret(pod *v1.Pod) (*string, error) {
	for _, v := range pod.Spec.Volumes {
		if  strings.Contains(v.Name, "token"){
			return &v.Name, nil
		}
	}
	return nil, errors.New("Could not find svc account secret")
}

func writeSecret(dir string, secretData map[string][]byte) error {
	for k, v := range secretData {
		err := ioutil.WriteFile(filepath.Join(dir, k), v, os.ModePerm)
		if err != nil {
			return err
		}
	}
	return nil
}

func makeServiceAccountMount(k *KubernetesClient, pod *v1.Pod, podDir string) (*criapi.Mount, error) {

	//1. Find the secret name
	secretName, err := findSvcAccountSecret(pod)
	if (nil != err) {
		return nil, err
	}

	//2. Create a directory on the host in the pods dir structure for this mount
	secretDirectory:=fmt.Sprintf("%s/volumes/kubernetes.io~secret/%s", podDir, *secretName)
	if err := createEmptyDirVolume(secretDirectory); err != nil {
		return nil, err
	}

	//3. Obtain Secret contents via the k8s API
	secretData, err := k.getSecret(*secretName, pod.Namespace)

	if err != nil {
		return nil, fmt.Errorf("Error geting Secret data from via k8s API %v", err)
	}

	//4. Write the secret contents to the dir
	err = writeSecret(secretDirectory, secretData)
	if err != nil {
		return nil, fmt.Errorf("Error geting Secret data %v", err)
	}


	//5. Return the mountpoint for the container
	return &criapi.Mount{
		ContainerPath:  "/var/run/secrets/kubernetes.io/serviceaccount",
		HostPath:       secretDirectory,
		Readonly:       true,
		Propagation:    0,
	}, nil
}

func makeEndIdentityMount(podDir string) (*criapi.Mount, error) {
	endIdentityDir := fmt.Sprintf("%s/volumes/kubernetes.io~empty-dir/linkerd-identity-end-entity",podDir)
	if err := createEmptyDirVolume(endIdentityDir); err != nil {
		return nil, err
	}

	return &criapi.Mount{
		HostPath:      endIdentityDir,
		ContainerPath: "/var/run/linkerd/identity/end-entity",
		Readonly:      false,
		Propagation:   0,
	}, nil
}



func makeHostsMount(podDir string, hostAliases []v1.HostAlias, useHostNetwork bool) (*criapi.Mount, error) {

	if err := createEmptyDirVolume(podDir); err != nil {
		return nil, err
	}

	hostsFilePath := path.Join(podDir, "etc-hosts")
	if err := ensureHostsFile(hostsFilePath, hostAliases, useHostNetwork); err != nil {
		return nil, err
	}

	return &criapi.Mount{
		ContainerPath:  etcHostsPath,
		HostPath:       hostsFilePath,
		Readonly:      false,
		Propagation:   0,
	}, nil
}


const (
	containerNameLabelKey = "io.kubernetes.container.name"
	podNameLabelKey       = "io.kubernetes.pod.name"
	podNamespaceLabelKey  = "io.kubernetes.pod.namespace"
	podUIDLabelKey        = "io.kubernetes.pod.uid"
	managedHostsHeader = "# Kubernetes-managed hosts file (host network + linkerd CNI).\n"
	etcHostsPath = "/etc/hosts"
	kubeletPodsPath = "/var/lib/kubelet/pods/"
	endIdentityPath = "/var/run/linkerd/identity/end-entity"
)


func ensureHostsFile(fileName string, hostAliases []v1.HostAlias, useHostNetwork bool) error {

	var buffer bytes.Buffer
	buffer.WriteString(managedHostsHeader)
	buffer.WriteString("127.0.0.1\tlocalhost\n")                      // ipv4 localhost
	buffer.WriteString("::1\tlocalhost ip6-localhost ip6-loopback\n") // ipv6 localhost
	buffer.WriteString("fe00::0\tip6-localnet\n")
	buffer.WriteString("fe00::0\tip6-mcastprefix\n")
	buffer.WriteString("fe00::1\tip6-allnodes\n")
	buffer.WriteString("fe00::2\tip6-allrouters\n")
	buffer.Write(hostsEntriesFromHostAliases(hostAliases))

	logrus.Debugf("Creating file %s", fileName)
	return ioutil.WriteFile(fileName, buffer.Bytes(), 0777)
}


func hostsEntriesFromHostAliases(hostAliases []v1.HostAlias) []byte {
	if len(hostAliases) == 0 {
		return []byte{}
	}

	var buffer bytes.Buffer
	buffer.WriteString("\n")
	buffer.WriteString("# Entries added by HostAliases.\n")
	// for each IP, write all aliases onto single line in hosts file
	for _, hostAlias := range hostAliases {
		buffer.WriteString(fmt.Sprintf("%s\t%s\n", hostAlias.IP, strings.Join(hostAlias.Hostnames, "\t")))
	}
	return buffer.Bytes()
}



func createEmptyDirVolume(directory string) error {
	err := os.MkdirAll(directory, os.ModePerm)
	if err != nil {
		return err
	}

	// ensure the dir is world writable, so it's accessible from within container (might not be fully writable if umask is set)
	err = os.Chmod(directory, 0777)
	if err != nil {
		return err
	}

	return  nil
}


func makeProfileSuffixes(linkerdCfg *pb.All) string {
	if linkerdCfg.Proxy.DisableExternalProfiles {
		return fmt.Sprintf("svc.%s.", linkerdCfg.Global.ClusterDomain)
	} else {
		return "."
	}
}

func identityDisabled(linkerdCfg *pb.All) bool {
	for _, v := range linkerdCfg.Install.Flags {
		if  v.Name == "disable-identity" && v.Value == "true"{
			return true
		}
	}
	return false
}

func tapDisabled(linkerdCfg *pb.All) bool {
	for _, v := range linkerdCfg.Install.Flags {
		if  v.Name == "disable-tap" && v.Value == "true"{
			return true
		}
	}
	return false
}

func makeEnvVars(l5dCfg *pb.All, pod *v1.Pod) []*criapi.KeyValue {
	const (
		ProxyLog                   = "LINKERD2_PROXY_LOG"
		DstSvcAddress              = "LINKERD2_PROXY_DESTINATION_SVC_ADDR"
		ControlListenAddress       = "LINKERD2_PROXY_CONTROL_LISTEN_ADDR"
		AdminListenAddress         = "LINKERD2_PROXY_ADMIN_LISTEN_ADDR"
		OutboundListenAddress      = "LINKERD2_PROXY_OUTBOUND_LISTEN_ADDR"
		InboundListenAddress       = "LINKERD2_PROXY_INBOUND_LISTEN_ADDR"
		DestinationGetSuffixes     = "LINKERD2_PROXY_DESTINATION_GET_SUFFIXES"
		DestinationProfileSuffixes = "LINKERD2_PROXY_DESTINATION_PROFILE_SUFFIXES"
		InboundAcceptKeepAlive     = "LINKERD2_PROXY_INBOUND_ACCEPT_KEEPALIVE"
		OutboundConnectKeepAlive   = "LINKERD2_PROXY_OUTBOUND_CONNECT_KEEPALIVE"
		PodNs                      = "_pod_ns"
		DestinationContext         = "LINKERD2_PROXY_DESTINATION_CONTEXT"
		IdentityDisabled           = "LINKERD2_PROXY_IDENTITY_DISABLED"
		IdentityDir                = "LINKERD2_PROXY_IDENTITY_DIR"
		IdentityTrustAnchors       = "LINKERD2_PROXY_IDENTITY_TRUST_ANCHORS"
		IdentityTokenFile          = "LINKERD2_PROXY_IDENTITY_TOKEN_FILE"
		IdentitySvcAddress         = "LINKERD2_PROXY_IDENTITY_SVC_ADDR"
		PodServiceAccount          = "_pod_sa"
		LinkerdNs                  = "_l5d_ns"
		LinkerdTrustDomain         = "_l5d_trustdomain"
		IdentityLocalName          = "LINKERD2_PROXY_IDENTITY_LOCAL_NAME"
		IdentitySvcName            = "LINKERD2_PROXY_IDENTITY_SVC_NAME"
		DestinationSvcName         = "LINKERD2_PROXY_DESTINATION_SVC_NAME"
		TapDisabled                = "LINKERD2_PROXY_TAP_DISABLED"
		TapSvcName                 = "LINKERD2_PROXY_TAP_SVC_NAME"
	)

	tapIsDisabled := tapDisabled(l5dCfg)
	identityIsDisabled := identityDisabled(l5dCfg)
	clusterDomain := l5dCfg.Global.ClusterDomain
	podNs := pod.Namespace
	l5dNs := l5dCfg.Global.LinkerdNamespace
	svcAccName := pod.Spec.ServiceAccountName
	trustDomain := l5dCfg.Global.IdentityContext.TrustDomain

	var envs []*criapi.KeyValue

	envs = append(envs,
		&criapi.KeyValue{
			Key:   ProxyLog,
			Value: l5dCfg.Proxy.LogLevel.Level,
		},
		&criapi.KeyValue{
			Key:   DstSvcAddress,
			Value: fmt.Sprintf("linkerd-destination.%s.svc.%s:8086", l5dNs, clusterDomain),
		},
		&criapi.KeyValue{
			Key:   ControlListenAddress,
			Value: fmt.Sprintf("0.0.0.0:%d", l5dCfg.Proxy.ControlPort.Port),
		},
		&criapi.KeyValue{
			Key:   AdminListenAddress,
			Value: fmt.Sprintf("0.0.0.0:%d", l5dCfg.Proxy.AdminPort.Port),
		},
		&criapi.KeyValue{
			Key:   OutboundListenAddress,
			Value: fmt.Sprintf("127.0.0.1:%d", l5dCfg.Proxy.OutboundPort.Port),
		},
		&criapi.KeyValue{
			Key:   InboundListenAddress,
			Value: fmt.Sprintf("0.0.0.0:%d", l5dCfg.Proxy.InboundPort.Port),
		},
		&criapi.KeyValue{
			Key:   DestinationGetSuffixes,
			Value: makeProfileSuffixes(l5dCfg),
		},
		&criapi.KeyValue{
			Key:   DestinationProfileSuffixes,
			Value: makeProfileSuffixes(l5dCfg),
		},
		&criapi.KeyValue{
			Key:   InboundAcceptKeepAlive,
			Value: "10000ms",
		},
		&criapi.KeyValue{
			Key:   OutboundConnectKeepAlive,
			Value: "10000ms",
		},
		&criapi.KeyValue{
			Key:   PodNs,
			Value: pod.Namespace,
		},
		&criapi.KeyValue{
			Key:   DestinationContext,
			Value: fmt.Sprintf("ns:%s", podNs),
		})

	if identityIsDisabled {
		envs = append(envs, &criapi.KeyValue{
			Key:   IdentityDisabled,
			Value: "disabled",
		})
	} else {
		envs = append(envs,
			&criapi.KeyValue{
				Key:   IdentityDir,
				Value: endIdentityPath,
			},
			&criapi.KeyValue{
				Key:   IdentityTrustAnchors,
				Value: l5dCfg.Global.IdentityContext.TrustAnchorsPem,
			},
			&criapi.KeyValue{
				Key:   IdentityTokenFile,
				Value: k8s.IdentityServiceAccountTokenPath,
			},
			&criapi.KeyValue{
				Key:   IdentitySvcAddress,
				Value: fmt.Sprintf("linkerd-identity.%s.svc.%s:8080", l5dNs, clusterDomain),
			},
			&criapi.KeyValue{
				Key:   PodServiceAccount,
				Value: pod.Spec.ServiceAccountName,
			},
			&criapi.KeyValue{
				Key:   LinkerdNs,
				Value: l5dCfg.Global.LinkerdNamespace,
			},
			&criapi.KeyValue{
				Key:   LinkerdTrustDomain,
				Value: l5dCfg.Global.IdentityContext.TrustDomain,
			},
			&criapi.KeyValue{
				Key:   IdentityLocalName,
				Value: fmt.Sprintf("%s.%s.serviceaccount.identity.%s.%s", svcAccName, podNs, l5dNs, trustDomain),
			},
			&criapi.KeyValue{
				Key:   IdentitySvcName,
				Value: fmt.Sprintf("linkerd-identity.%s.serviceaccount.identity.%s.%s", l5dNs, l5dNs, trustDomain),
			},
			&criapi.KeyValue{
				Key:   DestinationSvcName,
				Value: fmt.Sprintf("linkerd-controller.%s.serviceaccount.identity.%s.%s", l5dNs, l5dNs, trustDomain),
			})
	}

	if tapIsDisabled {
		envs = append(envs, &criapi.KeyValue{
			Key:   TapDisabled,
			Value: "true",})
	} else if !identityIsDisabled {
		envs = append(envs, &criapi.KeyValue{
			Key:   TapSvcName,
			Value: fmt.Sprintf("linkerd-tap.%s.serviceaccount.identity.%s.%s", l5dNs, l5dNs, trustDomain)})
	}
	return envs
}

func makeMounts(k *KubernetesClient, pod *v1.Pod, podDir string) ([]*criapi.Mount ,error)  {
	endIdMount, err := makeEndIdentityMount(podDir)
	if err != nil {
		logrus.Error("Could not make end identity mount", err)
		return nil, err
	}

	hostsMount, err := makeHostsMount(podDir, pod.Spec.HostAliases, false)
	if err != nil {
		logrus.Error("Could not make hosts mount", err)
		return nil, err
	}

	svcAccountMount, err :=  makeServiceAccountMount(k,pod, podDir)
	if err != nil {
		logrus.Error("Could not make svc account mount", err)
		return nil, err
	}

	return []*criapi.Mount{endIdMount, hostsMount, svcAccountMount,}, nil

}

func getLinkerdConfig(k *KubernetesClient, mapName, mapNamespace string) (*pb.All, error) {
	data, err := k.getConfigMap(mapName, mapNamespace)
	if err != nil {
		logrus.Error("Could not load linkerd config map", err)
		return nil, err
	}
	l5dCfg, err := config.FromConfigMap(data)
	if err != nil {
		logrus.Error("Could not parse linkerd config map", err)
		return nil, err
	}
	return l5dCfg, nil
}


func (p *CRIRuntime) StartProxy(podSandboxID string, pod *v1.Pod) error {
	podDir := filepath.Join(kubeletPodsPath, string(pod.UID))

	l5dCfg, err := getLinkerdConfig(p.kubernetes, k8s.ConfigConfigMapName, p.linkerdNamespace)
	if err != nil {
		return err
	}

	status, err := p.runtimeService.PodSandboxStatus(podSandboxID)
	if err != nil {
		return fmt.Errorf("Error getting pod sandbox status: %v", err)
	}

	mounts, err := makeMounts(p.kubernetes, pod, podDir)
	if err != nil {
		return err
	}

	envVars :=  makeEnvVars(l5dCfg, pod)

	containerConfig := criapi.ContainerConfig{
		LogPath: "/some-log",
		Mounts: mounts,
		Metadata: &criapi.ContainerMetadata{
			Name: k8s.ProxyContainerName,
		},
		Image: &criapi.ImageSpec{
			Image:  fmt.Sprintf(  "%s:%s", l5dCfg.Proxy.ProxyImage.ImageName, l5dCfg.Proxy.ProxyVersion),
		},

		Linux: &criapi.LinuxContainerConfig{
			SecurityContext: &criapi.LinuxContainerSecurityContext{
				RunAsUser:          &criapi.Int64Value{l5dCfg.Proxy.ProxyUid},
				SupplementalGroups: []int64{0},
				Privileged:         false,
				ReadonlyRootfs:     true,
			},
		},
		Envs:   envVars,

		Labels: map[string]string{
			containerNameLabelKey: k8s.ProxyContainerName,
			podNameLabelKey:       pod.Name,
			podNamespaceLabelKey:  pod.Namespace,
			podUIDLabelKey:        string(pod.UID),
		},
	}

	podSandboxConfig := criapi.PodSandboxConfig{
		Metadata:     status.GetMetadata(),
	}

	logrus.Debugf("Creating proxy sidecar container for pod %s", pod.Name)
	containerID, err := p.runtimeService.CreateContainer(podSandboxID, &containerConfig, &podSandboxConfig)
	if err != nil {
		return fmt.Errorf("Error creating sidecar container: %v", err)
	}
	logrus.Debugf("Created proxy sidecar container: %s", containerID)

	err = p.runtimeService.StartContainer(containerID)
	if err != nil {
		return fmt.Errorf("Error starting sidecar container: %v", err)
	}

	time.Sleep(20 * time.Second)

	logrus.Debugf("Started proxy sidecar container: %s", containerID)

	time.Sleep(20 * time.Second)

	st, err2 := p.runtimeService.ContainerStatus(containerID)

	if err2 != nil {
		return fmt.Errorf("Error getting status sidecar container: %v", err2)
	}
	logrus.Debugf("Status of container: %v", st)
	return nil
}


