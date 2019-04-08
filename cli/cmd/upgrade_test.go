package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	pb "github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/pflag"
)

const upgradeVersion = "TEST-VERSION"

func testUpgradeOptions() *upgradeOptions {
	o := newUpgradeOptionsWithDefaults()
	o.linkerdVersion = upgradeVersion
	return o
}

func TestRenderUpgrade(t *testing.T) {
	k8sConfigs := []string{`
kind: ConfigMap
apiVersion: v1
metadata:
  name: linkerd-config
  namespace: linkerd
  labels:
    linkerd.io/control-plane-component: controller
  annotations:
    linkerd.io/created-by: linkerd/cli edge-19.4.1
data:
  global: |
    {"linkerdNamespace":"linkerd","cniEnabled":false,"version":"edge-19.4.1","identityContext":{"trustDomain":"cluster.local","trustAnchorsPem":"-----BEGIN CERTIFICATE-----\nMIIBgzCCASmgAwIBAgIBATAKBggqhkjOPQQDAjApMScwJQYDVQQDEx5pZGVudGl0\neS5saW5rZXJkLmNsdXN0ZXIubG9jYWwwHhcNMTkwNDA0MjM1MzM3WhcNMjAwNDAz\nMjM1MzU3WjApMScwJQYDVQQDEx5pZGVudGl0eS5saW5rZXJkLmNsdXN0ZXIubG9j\nYWwwWTATBgcqhkjOPQIBBggqhkjOPQMBBwNCAAT+Sb5X4wi4XP0X3rJwMp23VBdg\nEMMU8EU+KG8UI2LmC5Vjg5RWLOW6BJjBmjXViKM+b+1/oKAeOg6FrJk8qyFlo0Iw\nQDAOBgNVHQ8BAf8EBAMCAQYwHQYDVR0lBBYwFAYIKwYBBQUHAwEGCCsGAQUFBwMC\nMA8GA1UdEwEB/wQFMAMBAf8wCgYIKoZIzj0EAwIDSAAwRQIhAKUFG3sYOS++bakW\nYmJZU45iCdTLtaelMDSFiHoC9eBKAiBDWzzo+/CYLLmn33bAEn8pQnogP4Fx06aj\n+U9K4WlbzA==\n-----END CERTIFICATE-----\n","issuanceLifetime":"86400s","clockSkewAllowance":"20s"},"autoInjectContext":null}
  proxy: |
    {"proxyImage":{"imageName":"gcr.io/linkerd-io/proxy","pullPolicy":"IfNotPresent"},"proxyInitImage":{"imageName":"gcr.io/linkerd-io/proxy-init","pullPolicy":"IfNotPresent"},"controlPort":{"port":4190},"ignoreInboundPorts":[],"ignoreOutboundPorts":[],"inboundPort":{"port":4143},"adminPort":{"port":4191},"outboundPort":{"port":4140},"resource":{"requestCpu":"","requestMemory":"","limitCpu":"","limitMemory":""},"proxyUid":"2102","logLevel":{"level":"warn,linkerd2_proxy=info"},"disableExternalProfiles":true}
  install: |
    {"uuid":"57af298c-58b0-43fc-8d88-3c338789bfbc","cliVersion":"edge-19.4.1","flags":[]}`,
		`
kind: Secret
apiVersion: v1
metadata:
  name: linkerd-identity-issuer
  namespace: linkerd
  labels:
    linkerd.io/control-plane-component: identity
  annotations:
    linkerd.io/created-by: linkerd/cli edge-19.4.1
    linkerd.io/identity-issuer-expiry: 2020-04-03T23:53:57Z
data:
  crt.pem: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUJnekNDQVNtZ0F3SUJBZ0lCQVRBS0JnZ3Foa2pPUFFRREFqQXBNU2N3SlFZRFZRUURFeDVwWkdWdWRHbDAKZVM1c2FXNXJaWEprTG1Oc2RYTjBaWEl1Ykc5allXd3dIaGNOTVRrd05EQTBNak0xTXpNM1doY05NakF3TkRBegpNak0xTXpVM1dqQXBNU2N3SlFZRFZRUURFeDVwWkdWdWRHbDBlUzVzYVc1clpYSmtMbU5zZFhOMFpYSXViRzlqCllXd3dXVEFUQmdjcWhrak9QUUlCQmdncWhrak9QUU1CQndOQ0FBVCtTYjVYNHdpNFhQMFgzckp3TXAyM1ZCZGcKRU1NVThFVStLRzhVSTJMbUM1VmpnNVJXTE9XNkJKakJtalhWaUtNK2IrMS9vS0FlT2c2RnJKazhxeUZsbzBJdwpRREFPQmdOVkhROEJBZjhFQkFNQ0FRWXdIUVlEVlIwbEJCWXdGQVlJS3dZQkJRVUhBd0VHQ0NzR0FRVUZCd01DCk1BOEdBMVVkRXdFQi93UUZNQU1CQWY4d0NnWUlLb1pJemowRUF3SURTQUF3UlFJaEFLVUZHM3NZT1MrK2Jha1cKWW1KWlU0NWlDZFRMdGFlbE1EU0ZpSG9DOWVCS0FpQkRXenpvKy9DWUxMbW4zM2JBRW44cFFub2dQNEZ4MDZhagorVTlLNFdsYnpBPT0KLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo=
  key.pem: LS0tLS1CRUdJTiBFQyBQUklWQVRFIEtFWS0tLS0tCk1IY0NBUUVFSUhaaEFWTnNwSlRzMWZ4YmZ4VmptTTJvMTNTOFd4U2VVdTlrNFhZK0NPY3JvQW9HQ0NxR1NNNDkKQXdFSG9VUURRZ0FFL2ttK1YrTUl1Rno5Rjk2eWNES2R0MVFYWUJEREZQQkZQaWh2RkNOaTVndVZZNE9VVml6bAp1Z1NZd1pvMTFZaWpQbS90ZjZDZ0hqb09oYXlaUEtzaFpRPT0KLS0tLS1FTkQgRUMgUFJJVkFURSBLRVktLS0tLQo=`,
	}

	options := testUpgradeOptions()
	flags := options.recordableFlagSet(pflag.ExitOnError)

	clientset, _, err := k8s.NewFakeClientSets(k8sConfigs...)
	if err != nil {
		t.Fatalf("Error mocking k8s client: %s", err)
	}

	values, configs, err := options.validateAndBuild(clientset, flags)
	if err != nil {
		t.Fatalf("validateAndBuild failed with %s", err)
	}

	if configs.GetGlobal().GetVersion() != upgradeVersion {
		t.Errorf("version not upgraded in config")
	}

	var buf bytes.Buffer
	if err = values.render(&buf, configs); err != nil {
		t.Fatalf("could not render upgrade configuration: %s", err)
	}
	diffTestdata(t, "upgrade_default.golden", buf.String())
}

func TestUpgradeFromOldConfig(t *testing.T) {
	k8sConfigs := []string{`
kind: ConfigMap
apiVersion: v1
metadata:
  name: linkerd-config
  namespace: linkerd
  labels:
    linkerd.io/control-plane-component: controller
  annotations:
    linkerd.io/created-by: linkerd/cli edge-19.4.1
data:
  global: |
    {"linkerdNamespace":"linkerd","cniEnabled":false,"version":"edge-19.4.1","identityContext":null,"autoInjectContext":null}
  proxy: |
    {"proxyImage":{"imageName":"gcr.io/linkerd-io/proxy","pullPolicy":"IfNotPresent"},"proxyInitImage":{"imageName":"gcr.io/linkerd-io/proxy-init","pullPolicy":"IfNotPresent"},"controlPort":{"port":4190},"ignoreInboundPorts":[],"ignoreOutboundPorts":[],"inboundPort":{"port":4143},"adminPort":{"port":4191},"outboundPort":{"port":4140},"resource":{"requestCpu":"","requestMemory":"","limitCpu":"","limitMemory":""},"proxyUid":"2102","logLevel":{"level":"warn,linkerd2_proxy=info"},"disableExternalProfiles":true}
  install: |
    {"uuid":"57af298c-58b0-43fc-8d88-3c338789bfbc","cliVersion":"edge-19.3.1","flags":[]}
`,
	}

	options := newUpgradeOptionsWithDefaults()
	options.proxyAutoInject = true
	flags := options.recordableFlagSet(pflag.ExitOnError)

	clientset, _, err := k8s.NewFakeClientSets(k8sConfigs...)
	if err != nil {
		t.Fatalf("Error mocking k8s client: %s", err)
	}

	values, configs, err := options.validateAndBuild(clientset, flags)
	if err != nil {
		t.Fatalf("validateAndBuild failed with %s", err)
	}

	if values.Identity == nil ||
		values.Identity.TrustAnchorsPEM == "" ||
		values.Identity.TrustDomain == "" ||
		values.Identity.Issuer == nil ||
		values.Identity.Issuer.CrtPEM == "" ||
		values.Identity.Issuer.KeyPEM == "" {
		t.Errorf("issuer values not generated")
	}
	if configs.GetGlobal().GetIdentityContext().GetTrustAnchorsPem() == "" {
		t.Errorf("identity config not generated")
	}
	if configs.GetGlobal().GetAutoInjectContext() == nil {
		t.Errorf("autoinject config not generated")
	}

	global := pb.Global{}
	if err := json.Unmarshal([]byte(values.Configs.Global), &global); err != nil {
		t.Fatalf("Could not unmarshal global config: %s", err)
	}
	if configs.GetGlobal().GetIdentityContext().GetTrustAnchorsPem() == "" {
		t.Errorf("identity config not serialized")
	}
	if configs.GetGlobal().GetAutoInjectContext() == nil {
		t.Errorf("autoinject config not serialized")
	}
}
