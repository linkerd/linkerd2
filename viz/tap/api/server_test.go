package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/controller/k8s"
	k8sutils "github.com/linkerd/linkerd2/pkg/k8s"
)

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
			k8sRes: []string{`
apiVersion: v1
kind: ConfigMap
metadata:
  name: extension-apiserver-authentication
  namespace: kube-system
data:
  client-ca-file: 'client-ca-file'
  requestheader-allowed-names: '["name1", "name2"]'
  requestheader-client-ca-file: 'requestheader-client-ca-file'
  requestheader-extra-headers-prefix: '["X-Remote-Extra-"]'
  requestheader-group-headers: '["X-Remote-Group"]'
  requestheader-username-headers: '["X-Remote-User"]'
`,
			},
			clientCAPem:    "requestheader-client-ca-file",
			allowedNames:   []string{"name1", "name2"},
			usernameHeader: "X-Remote-User",
			groupHeader:    "X-Remote-Group",
			err:            nil,
		},
	}

	ctx := context.Background()
	for i, exp := range expectations {
		exp := exp // pin

		t.Run(fmt.Sprintf("%d parses the apiServerAuth ConfigMap", i), func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(exp.k8sRes...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			clientCAPem, allowedNames, usernameHeader, groupHeader, err := serverAuth(ctx, k8sAPI)
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

func TestValidate(t *testing.T) {
	cert := testCertificate()
	cert.Subject.CommonName = "name-any"

	tls := tls.ConnectionState{PeerCertificates: []*x509.Certificate{&cert}}

	req := http.Request{TLS: &tls}

	server := Server{}
	if err := server.validate(&req); err != nil {
		t.Fatalf("No error expected for %q but encountered %q", cert.Subject.CommonName, err.Error())
	}
}

func TestValidate_ClientAllowed(t *testing.T) {
	cert := testCertificate()
	cert.Subject.CommonName = "name-trusted"

	tls := tls.ConnectionState{PeerCertificates: []*x509.Certificate{&cert}}

	req := http.Request{TLS: &tls}

	server := Server{allowedNames: []string{"name-trusted"}}
	if err := server.validate(&req); err != nil {
		t.Fatalf("No error expected for %q but encountered %q", cert.Subject.CommonName, err.Error())
	}
}

func TestValidate_ClientAllowedViaSAN(t *testing.T) {
	cert := testCertificate()
	cert.Subject.CommonName = "name-any"

	tls := tls.ConnectionState{PeerCertificates: []*x509.Certificate{&cert}}

	req := http.Request{TLS: &tls}

	server := Server{allowedNames: []string{"linkerd.io"}}
	if err := server.validate(&req); err != nil {
		t.Fatalf("No error expected for %q but encountered %q", cert.Subject.CommonName, err.Error())
	}
}

func TestValidate_ClientNotAllowed(t *testing.T) {
	cert := testCertificate()
	cert.Subject.CommonName = "name-untrusted"

	tls := tls.ConnectionState{PeerCertificates: []*x509.Certificate{&cert}}

	req := http.Request{TLS: &tls}

	server := Server{allowedNames: []string{"name-trusted"}}
	if err := server.validate(&req); err == nil {
		t.Fatalf("Expected request to be rejected for %q", cert.Subject.CommonName)
	}
}

func TestIsSubjectAlternateName(t *testing.T) {
	testCases := []struct {
		name     string
		expected bool
	}{
		{
			name:     "linkerd.io",
			expected: true,
		},
		{
			name:     "root@localhost",
			expected: true,
		},
		{
			name:     "192.168.1.1",
			expected: true,
		},
		{
			name:     "http://localhost/api/test",
			expected: true,
		},
		{
			name:     "mystique",
			expected: false,
		},
	}

	cert := testCertificate()
	for _, tc := range testCases {
		tc := tc // pin
		t.Run(tc.name, func(t *testing.T) {
			actual := isSubjectAlternateName(&cert, tc.name)
			if actual != tc.expected {
				t.Fatalf("expected %t, but got %t", tc.expected, actual)
			}
		})
	}
}

func testCertificate() x509.Certificate {
	uri, _ := url.Parse("http://localhost/api/test")
	cert := x509.Certificate{
		Subject: pkix.Name{
			CommonName: "linkerd-test",
		},
		DNSNames: []string{
			"localhost",
			"linkerd.io",
		},
		EmailAddresses: []string{
			"root@localhost",
		},
		IPAddresses: []net.IP{
			net.IPv4(127, 0, 0, 1),
			net.IPv4(192, 168, 1, 1),
		},
		URIs: []*url.URL{
			uri,
		},
	}
	return cert
}
