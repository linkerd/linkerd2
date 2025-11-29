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
	"testing"

	"github.com/go-test/deep"
	"github.com/linkerd/linkerd2/controller/k8s"
	k8sutils "github.com/linkerd/linkerd2/pkg/k8s"
)

func TestAPIServerAuth(t *testing.T) {
	expectations := []struct {
		k8sRes            []string
		clientCAPem       string
		allowedNames      []string
		usernameHeader    string
		groupHeader       string
		extraHeaderPrefix string
		err               error
	}{
		{
			err: fmt.Errorf("failed to load [%s] config: configmaps %q not found", k8sutils.ExtensionAPIServerAuthenticationConfigMapName, k8sutils.ExtensionAPIServerAuthenticationConfigMapName),
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
			clientCAPem:       "requestheader-client-ca-file",
			allowedNames:      []string{"name1", "name2"},
			usernameHeader:    "X-Remote-User",
			groupHeader:       "X-Remote-Group",
			extraHeaderPrefix: "X-Remote-Extra-",
			err:               nil,
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

			clientCAPem, allowedNames, usernameHeader, groupHeader, extraHeaderPrefix, err := serverAuth(ctx, k8sAPI)

			if err != nil && exp.err != nil {
				if err.Error() != exp.err.Error() {
					t.Errorf("apiServerAuth returned unexpected error: %q, expected: %q", err, exp.err)
				}
			} else if err != nil {
				t.Fatalf("Unexpected error: %s", err)
			} else if exp.err != nil {
				t.Fatalf("Did not encounter expected error: %s", err)
			}

			if clientCAPem != exp.clientCAPem {
				t.Errorf("apiServerAuth returned unexpected clientCAPem: %q, expected: %q", clientCAPem, exp.clientCAPem)
			}
			if diff := deep.Equal(allowedNames, exp.allowedNames); diff != nil {
				t.Errorf("%v", diff)
			}
			if usernameHeader != exp.usernameHeader {
				t.Errorf("apiServerAuth returned unexpected usernameHeader: %q, expected: %q", usernameHeader, exp.usernameHeader)
			}
			if groupHeader != exp.groupHeader {
				t.Errorf("apiServerAuth returned unexpected groupHeader: %q, expected: %q", groupHeader, exp.groupHeader)
			}
			if extraHeaderPrefix != exp.extraHeaderPrefix {
				t.Errorf("apiServerAuth returned unexpected extraHeaderPrefix: %q, expected: %q", extraHeaderPrefix, exp.extraHeaderPrefix)
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
