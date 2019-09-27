package tap

import (
	"crypto/tls"
	"fmt"
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/controller/k8s"
	k8sutils "github.com/linkerd/linkerd2/pkg/k8s"
)

func TestNewAPIServer(t *testing.T) {
	expectations := []struct {
		k8sRes []string
		err    error
	}{
		{
			err: fmt.Errorf("failed to load [%s] config: configmaps %q not found", k8sutils.ExtensionAPIServerAuthenticationConfigMapName, k8sutils.ExtensionAPIServerAuthenticationConfigMapName),
		},
		{
			err: nil,
			k8sRes: []string{
				k8sutils.ExtensionAPIServerConfigMapResource(map[string]string{
					"client-ca-file":                     `'client-ca-file'`,
					"requestheader-allowed-names":        `'["name1", "name2"]'`,
					"requestheader-extra-headers-prefix": `'["X-Remote-Extra-"]'`,
					"requestheader-group-headers":        `'["X-Remote-Group"]'`,
					"requestheader-username-headers":     `'["X-Remote-User"]'`,
					k8sutils.ExtensionAPIServerAuthenticationRequestHeaderClientCAFileKey: `'requestheader-client-ca-file'`,
				}),
			},
		},
	}

	for i, exp := range expectations {
		exp := exp // pin

		t.Run(fmt.Sprintf("%d returns a configured API Server", i), func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(exp.k8sRes...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			fakeGrpcServer := newGRPCTapServer(4190, "controller-ns", "cluster.local", k8sAPI)

			_, _, err = NewAPIServer("localhost:0", tls.Certificate{}, k8sAPI, fakeGrpcServer, false)
			if !reflect.DeepEqual(err, exp.err) {
				t.Errorf("NewAPIServer returned unexpected error: %s, expected: %s", err, exp.err)
			}
		})
	}
}

func TestAPIServerAuth(t *testing.T) {
	expectations := []struct {
		k8sRes         []string
		clientCAPem    string
		allowedNames   []string
		usernameHeader string
		groupHeader    string
		err            error
	}{
		{
			err: fmt.Errorf("failed to load [%s] config: configmaps \"%s\" not found", k8sutils.ExtensionAPIServerAuthenticationConfigMapName, k8sutils.ExtensionAPIServerAuthenticationConfigMapName),
		},
		{
			k8sRes: []string{
				k8sutils.ExtensionAPIServerConfigMapResource(map[string]string{
					"client-ca-file":                     `'client-ca-file'`,
					"requestheader-allowed-names":        `'["name1", "name2"]'`,
					"requestheader-extra-headers-prefix": `'["X-Remote-Extra-"]'`,
					"requestheader-group-headers":        `'["X-Remote-Group"]'`,
					"requestheader-username-headers":     `'["X-Remote-User"]'`,
					k8sutils.ExtensionAPIServerAuthenticationRequestHeaderClientCAFileKey: `'requestheader-client-ca-file'`,
				}),
			},
			clientCAPem:    "requestheader-client-ca-file",
			allowedNames:   []string{"name1", "name2"},
			usernameHeader: "X-Remote-User",
			groupHeader:    "X-Remote-Group",
			err:            nil,
		},
	}

	for i, exp := range expectations {
		exp := exp // pin

		t.Run(fmt.Sprintf("%d parses the apiServerAuth ConfigMap", i), func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(exp.k8sRes...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			clientCAPem, allowedNames, usernameHeader, groupHeader, err := apiServerAuth(k8sAPI)
			if !reflect.DeepEqual(err, exp.err) {
				t.Errorf("apiServerAuth returned unexpected error: %s, expected: %s", err, exp.err)
			}
			if clientCAPem != exp.clientCAPem {
				t.Errorf("apiServerAuth returned unexpected clientCAPem: %s, expected: %s", clientCAPem, exp.clientCAPem)
			}
			if !reflect.DeepEqual(allowedNames, exp.allowedNames) {
				t.Errorf("apiServerAuth returned unexpected allowedNames: %s, expected: %s", allowedNames, exp.allowedNames)
			}
			if usernameHeader != exp.usernameHeader {
				t.Errorf("apiServerAuth returned unexpected usernameHeader: %s, expected: %s", usernameHeader, exp.usernameHeader)
			}
			if groupHeader != exp.groupHeader {
				t.Errorf("apiServerAuth returned unexpected groupHeader: %s, expected: %s", groupHeader, exp.groupHeader)
			}
		})
	}
}
