package identity

import (
	"context"
	"fmt"
	"strings"

	"github.com/linkerd/linkerd2/pkg/identity"
	kauthnApi "k8s.io/api/authentication/v1"
	kauthzApi "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	k8s "k8s.io/client-go/kubernetes"
	kauthn "k8s.io/client-go/kubernetes/typed/authentication/v1"
	kauthz "k8s.io/client-go/kubernetes/typed/authorization/v1"
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
	ctx context.Context,
	k8s k8s.Interface,
	domain *TrustDomain,
) (identity.Validator, error) {
	if err := checkAccess(ctx, k8s.AuthorizationV1()); err != nil {
		return nil, err
	}

	authn := k8s.AuthenticationV1()
	return &K8sTokenValidator{authn, domain}, nil
}

// Validate accepts kubernetes bearer tokens and returns a DNS-form linkerd ID.
func (k *K8sTokenValidator) Validate(ctx context.Context, tok []byte) (string, error) {
	// TODO: Set/check `audience`
	tr := kauthnApi.TokenReview{Spec: kauthnApi.TokenReviewSpec{Token: string(tok)}}
	rvw, err := k.authn.TokenReviews().Create(ctx, &tr, metav1.CreateOptions{})
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
		if errs := validation.IsDNS1123Label(l); len(errs) > 0 {
			return "", identity.InvalidToken{Reason: fmt.Sprintf("Not a label: %s", l)}
		}
	}

	return k.domain.Identity(uns[0], uns[2], uns[1])
}

func checkAccess(ctx context.Context, authz kauthz.AuthorizationV1Interface) error {
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
	rvw, err := authz.SelfSubjectAccessReviews().Create(ctx, r, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	if !rvw.Status.Allowed {
		return fmt.Errorf("Unable to create kubernetes token reviews: %s", rvw.Status.Reason)
	}

	return nil
}
