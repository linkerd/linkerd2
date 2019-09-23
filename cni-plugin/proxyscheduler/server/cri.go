package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
)


type CRIRuntime struct {
	startedProxy bool
	runtimeService cri.RuntimeService
	imageService   cri.ImageManagerService
	httpClient     http.Client
	kubernetes *KubernetesClient

}


func NewCRIRuntime(kubernetes *KubernetesClient) (*CRIRuntime, error) {
	runtimeService, err := remote.NewRemoteRuntimeService(getRemoteRuntimeEndpoint(), 2*time.Minute)
	if err != nil {
		return nil, err
	}
	imageService, err := remote.NewRemoteImageService(getRemoteRuntimeEndpoint(), 2*time.Minute)
	if err != nil {
		return nil, err
	}

	return &CRIRuntime{
		startedProxy:false,
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
	sidecarLabelKey       = "istio-sidecar"
	sidecarLabelValue     = "true"
	containerNameLabelKey = "io.kubernetes.container.name"
	podNameLabelKey       = "io.kubernetes.pod.name"
	podNamespaceLabelKey  = "io.kubernetes.pod.namespace"
	podUIDLabelKey        = "io.kubernetes.pod.uid"
	managedHostsHeader = "# Kubernetes-managed hosts file (host network + linkerd CNI).\n"
	etcHostsPath = "/etc/hosts"

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


func (p *CRIRuntime) StartProxy(podSandboxID string, pod *v1.Pod) error {

	podDir := fmt.Sprintf("/var/lib/kubelet/pods/%s", pod.UID)

	time.Sleep(20 * time.Second)


	endIdMount, err := makeEndIdentityMount(podDir)
	if (err != nil) {
		logrus.Error("Could not make end identity mount", err)
		return err
	}

	hostsMount, err := makeHostsMount(podDir, pod.Spec.HostAliases, false)
	if (err != nil) {
		logrus.Error("Could not make hosts mount", err)
		return err
	}

	svcAccountMount, err :=  makeServiceAccountMount(p.kubernetes,pod, podDir)
	if (err != nil) {
		logrus.Error("Could not make svc account mount", err)
		return err
	}

	mounts:= []*criapi.Mount{endIdMount, hostsMount, svcAccountMount,}


	envs:= []*criapi.KeyValue{

		{
			Key:   "LINKERD2_PROXY_LOG",
			Value: "info,linkerd2_proxy=info",
		},

		{
			Key:   "LINKERD2_PROXY_DESTINATION_SVC_ADDR",
			Value: "linkerd-destination.linkerd.svc.cluster.local:8086",
		},

		{
			Key:   "LINKERD2_PROXY_CONTROL_LISTEN_ADDR",
			Value: "0.0.0.0:4190",
		},

		{
			Key:   "LINKERD2_PROXY_ADMIN_LISTEN_ADDR",
			Value: "0.0.0.0:4191",
		},

		{
			Key:   "LINKERD2_PROXY_OUTBOUND_LISTEN_ADDR",
			Value: "127.0.0.1:4140",
		},

		{
			Key:   "LINKERD2_PROXY_INBOUND_LISTEN_ADDR",
			Value: "0.0.0.0:4143",
		},

		{
			Key:   "LINKERD2_PROXY_DESTINATION_GET_SUFFIXES",
			Value: "svc.cluster.local.",
		},

		{
			Key:   "LINKERD2_PROXY_DESTINATION_PROFILE_SUFFIXES",
			Value: "svc.cluster.local.",
		},

		{
			Key:   "LINKERD2_PROXY_INBOUND_ACCEPT_KEEPALIVE",
			Value: "10000ms",
		},

		{
			Key:   "LINKERD2_PROXY_OUTBOUND_CONNECT_KEEPALIVE",
			Value: "10000ms",
		},

		{
			Key:   "_pod_ns",
			Value: "emojivoto",
		},

		{
			Key:   "LINKERD2_PROXY_DESTINATION_CONTEXT",
			Value: "ns:emojivoto",
		},

		{
			Key:   "LINKERD2_PROXY_IDENTITY_DIR",
			Value: "/var/run/linkerd/identity/end-entity",
		},

		{
			Key:   "LINKERD2_PROXY_IDENTITY_TRUST_ANCHORS",
			Value: "-----BEGIN CERTIFICATE-----\nMIIBYDCCAQegAwIBAgIBATAKBggqhkjOPQQDAjAYMRYwFAYDVQQDEw1jbHVzdGVy\nLmxvY2FsMB4XDTE5MDMwMzAxNTk1MloXDTI5MDIyODAyMDM1MlowGDEWMBQGA1UE\nAxMNY2x1c3Rlci5sb2NhbDBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABAChpAt0\nxtgO9qbVtEtDK80N6iCL2Htyf2kIv2m5QkJ1y0TFQi5hTVe3wtspJ8YpZF0pl364\n6TiYeXB8tOOhIACjQjBAMA4GA1UdDwEB/wQEAwIBBjAdBgNVHSUEFjAUBggrBgEF\nBQcDAQYIKwYBBQUHAwIwDwYDVR0TAQH/BAUwAwEB/zAKBggqhkjOPQQDAgNHADBE\nAiBQ/AAwF8kG8VOmRSUTPakSSa/N4mqK2HsZuhQXCmiZHwIgZEzI5DCkpU7w3SIv\nOLO4Zsk1XrGZHGsmyiEyvYF9lpY=\n-----END CERTIFICATE-----\n",},

		{
			Key:   "LINKERD2_PROXY_IDENTITY_TOKEN_FILE",
			Value: "/var/run/secrets/kubernetes.io/serviceaccount/token",
		},

		{
			Key:   "LINKERD2_PROXY_IDENTITY_SVC_ADDR",
			Value: "linkerd-identity.linkerd.svc.cluster.local:8080",
		},

		{
			Key:   "_pod_sa",
			Value: pod.Spec.ServiceAccountName,
		},

		{
			Key:   "_l5d_ns",
			Value: "linkerd",
		},

		{
			Key:   "_l5d_trustdomain",
			Value: "cluster.local",
		},

		{
			Key:   "LINKERD2_PROXY_IDENTITY_LOCAL_NAME",
			Value: fmt.Sprintf("%s.emojivoto.serviceaccount.identity.linkerd.cluster.local", pod.Spec.ServiceAccountName),
		},

		{
			Key:   "LINKERD2_PROXY_IDENTITY_SVC_NAME",
			Value: "linkerd-identity.linkerd.serviceaccount.identity.linkerd.cluster.local",
		},

		{
			Key:   "LINKERD2_PROXY_DESTINATION_SVC_NAME",
			Value: "linkerd-controller.linkerd.serviceaccount.identity.linkerd.cluster.local",
		},

		{
			Key:   "LINKERD2_PROXY_TAP_SVC_NAME",
			Value: "linkerd-tap.linkerd.serviceaccount.identity.linkerd.cluster.local",
		},

		}

	time.Sleep(20 * time.Second)

	status, err := p.runtimeService.PodSandboxStatus(podSandboxID)
	if err != nil {
		return fmt.Errorf("Error getting pod sandbox status: %v", err)
	}

	containerConfig := criapi.ContainerConfig{
		LogPath: "/some-log",
		Mounts: mounts,
		Metadata: &criapi.ContainerMetadata{
			Name: "linkerd-proxy",
		},
		Image: &criapi.ImageSpec{
			Image: "gcr.io/linkerd-io/proxy:dev-a30882ef-zaharidichev",
		},

		Linux: &criapi.LinuxContainerConfig{
			SecurityContext: &criapi.LinuxContainerSecurityContext{
				RunAsUser:          &criapi.Int64Value{2102},
				SupplementalGroups: []int64{0}, // all containers get ROOT GID by default
				Privileged:         false,
				ReadonlyRootfs:     true,
			},
		},
		Envs:   envs,

		Labels: map[string]string{
			sidecarLabelKey:       sidecarLabelValue,
			containerNameLabelKey: "linkerd-proxy",
			podNameLabelKey:       pod.Name,
			podNamespaceLabelKey:  pod.Namespace,
			podUIDLabelKey:        string(pod.UID),
		},

	}

	podSandboxConfig := criapi.PodSandboxConfig{
		Metadata:     status.GetMetadata(),
		Labels: map[string]string{
			"somelabel":       "somevalue",
		},

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

func toDebugJSON(obj interface{}) string {
	b, err := toJSON(obj)
	if err != nil {
		b = "error marshalling to JSON: " + err.Error()
	}
	return b
}

func toJSON(obj interface{}) (string, error) {
	bytes, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}





/*func (p *CRIRuntime) StartProxy {



	cfg := criapi.ContainerConfig{
		Metadata: &criapi.ContainerMetadata{
			Name: "linkerd-proxy",
		},

		Image: &criapi.ImageSpec{
			Image: "gcr.io/linkerd-io/proxy:dev-a30882ef-zaharidichev",
		},

		Linux: &criapi.LinuxContainerConfig{
			SecurityContext: &criapi.LinuxContainerSecurityContext{
				RunAsUser:          &criapi.Int64Value{2102},
				SupplementalGroups: []int64{0},
				Privileged:         false,
				ReadonlyRootfs:     true,
			},
		},
	}

	print(cfg)
ls -l
}
*/
