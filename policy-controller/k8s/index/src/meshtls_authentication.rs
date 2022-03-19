use crate::index::Index;
use linkerd_policy_controller_core::IdentityMatch;
use linkerd_policy_controller_k8s_api::{self as k8s, ResourceExt};

impl kubert::index::IndexNamespacedResource<k8s::policy::MeshTLSAuthentication> for Index {
    fn apply(&mut self, authn: k8s::policy::MeshTLSAuthentication) {
        let namespace = authn.namespace().unwrap();
        let name = authn.name();

        let authns = authn
            .spec
            .identities
            .into_iter()
            .flatten()
            .map(|s| {
                s.parse::<IdentityMatch>()
                    .expect("identity match parsing is infallible")
            })
            .collect::<Vec<_>>();

        if authns.is_empty() {
            tracing::warn!("No authentication targets");
            return;
        }

        self.apply_meshtls_authentication(namespace, name, authns);
    }

    fn delete(&mut self, namespace: String, name: String) {
        self.delete_meshtls_authentication(namespace, &name);
    }
}
