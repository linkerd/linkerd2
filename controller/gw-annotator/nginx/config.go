package nginx

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/linkerd/linkerd2/controller/gw-annotator/gateway"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	// DefaultPrefix is the default annotations prefix used by the nginx
	// ingress controller.
	DefaultPrefix = "nginx"

	// ConfigSnippetKey is the annotations key used for the nginx configuration
	// snippet entry.
	ConfigSnippetKey = ".ingress.kubernetes.io/configuration-snippet"
)

var (
	l5dHeadersRE = regexp.MustCompile(fmt.Sprintf(`(proxy|grpc)_set_header %s.+`, gateway.L5DHeader))
)

// ConfigSnippet represents a list of nginx config entries.
type ConfigSnippet struct {
	AnnotationKey string
	Entries       []string
}

// NewConfigSnippet creates a new NginxConfigSnippet instance from an
// unstructured object, returning also if the operation succeeded or not.
func NewConfigSnippet(obj *unstructured.Unstructured) (*ConfigSnippet, bool) {
	cs := &ConfigSnippet{}

	for k, v := range obj.GetAnnotations() {
		if strings.Contains(k, ConfigSnippetKey) {
			cs.AnnotationKey = k
			for _, entry := range strings.Split(v, "\n") {
				if entry != "" {
					cs.Entries = append(cs.Entries, entry)
				}
			}
			return cs, true
		}
	}

	// Config snippet annotation not found in object, fallback to default
	// TODO (tegioz): potential issue, nginx annotation prefix is configurable
	// by user, so using the default one might not work. We will probably need
	// to make it configurable in L5D as well.
	cs.AnnotationKey = DefaultPrefix + ConfigSnippetKey

	return cs, false
}

// ToString converts the configuration snippet list of entries to a string.
func (cs *ConfigSnippet) ToString() string {
	return strings.Join(cs.Entries, "\n")
}

// ContainsL5DHeader checks if the configuration snippet contains the L5D
// header.
func (cs *ConfigSnippet) ContainsL5DHeader() bool {
	for _, entry := range cs.Entries {
		if l5dHeadersRE.MatchString(entry) {
			return true
		}
	}
	return false
}
