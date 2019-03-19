package identity

import (
	"context"
	"fmt"
	"strings"

	kauthnApi "k8s.io/api/authentication/v1"
	kauthzApi "k8s.io/api/authorization/v1"
	k8s "k8s.io/client-go/kubernetes"
	kauthn "k8s.io/client-go/kubernetes/typed/authentication/v1"
	kauthz "k8s.io/client-go/kubernetes/typed/authorization/v1"

	"github.com/linkerd/linkerd2/pkg/identity"
)

// K8sTokenValidator implements Validator for Kubernetes bearer tokens.
type K8sTokenValidator struct {
	authn  kauthn.AuthenticationV1Interface
	domain *TrustDomain
}

// NewK8sTokenValidator takes a kubernetes client and trust domain to create a
// K8sTokenValidator.
//
// The kubernetes client is used immediately to validate that the client has
// sufficient privileges to perform token reviews. An error is returned if this
// access check fails.
func NewK8sTokenValidator(
	k8s k8s.Interface,
	domain *TrustDomain,
) (*K8sTokenValidator, error) {
	if err := checkAccess(k8s.AuthorizationV1()); err != nil {
		return nil, err
	}

	authn := k8s.AuthenticationV1()
	return &K8sTokenValidator{authn, domain}, nil
}

func init() {
	var _ identity.Validator = &K8sTokenValidator{}
}

// Validate accepts kubernetes bearer tokens and returns a DNS-form linkerd ID.
func (k *K8sTokenValidator) Validate(_ context.Context, tok []byte) (string, error) {
	// TODO: Set/check `audience`
	tr := kauthnApi.TokenReview{Spec: kauthnApi.TokenReviewSpec{Token: string(tok)}}
	rvw, err := k.authn.TokenReviews().Create(&tr)
	if err != nil {
		return "", err
	}

	if rvw.Status.Error != "" {
		return "", identity.InvalidToken{Reason: rvw.Status.Error}
	}
	if !rvw.Status.Authenticated {
		return "", identity.NotAuthenticated{}
	}

	// Determine the identity associated with the token's userinfo.
	uns := strings.Split(rvw.Status.User.Username, ":")
	if len(uns) != 4 || uns[0] != "system" {
		msg := fmt.Sprintf("Username must be in form system:TYPE:NS:SA: %s", rvw.Status.User.Username)
		return "", identity.InvalidToken{Reason: msg}
	}
	uns = uns[1:]
	for _, l := range uns {
		if !isLabel(l) {
			return "", identity.InvalidToken{Reason: fmt.Sprintf("Not a label: %s", l)}
		}
	}

	return k.domain.Identity(uns[0], uns[2], uns[1])
}

func checkAccess(authz kauthz.AuthorizationV1Interface) error {
	r := &kauthzApi.SelfSubjectAccessReview{
		Spec: kauthzApi.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &kauthzApi.ResourceAttributes{
				Group:    "authentication.k8s.io",
				Version:  "v1",
				Resource: "tokenreviews",
				Verb:     "create",
			},
		},
	}
	rvw, err := authz.SelfSubjectAccessReviews().Create(r)
	if err != nil {
		return err
	}
	if !rvw.Status.Allowed {
		return fmt.Errorf("Unable to create kubernetes token reviews: %s", rvw.Status.Reason)
	}

	return nil
}
