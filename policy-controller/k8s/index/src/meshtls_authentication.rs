use crate::index::Index;
use linkerd_policy_controller_core::IdentityMatch;
use linkerd_policy_controller_k8s_api::{self as k8s, ResourceExt};

impl kubert::index::IndexNamespacedResource<k8s::policy::MeshTLSAuthentication> for Index {
    fn apply(&mut self, authn: k8s::policy::MeshTLSAuthentication) {
        let namespace = authn.namespace().unwrap();
        let name = authn.name();

        let identities = authn.spec.identities.into_iter().flatten().map(|s| {
            s.parse::<IdentityMatch>()
                .expect("identity match parsing is infallible")
        });

        let identity_refs = authn
            .spec
            .identity_refs
            .into_iter()
            .flatten()
            .filter_map(|tgt| {
                if !tgt.kind.eq_ignore_ascii_case("ServiceAccount") {
                    return None;
                }
                let ns = tgt.namespace.as_deref().unwrap_or(&namespace);
                let name = tgt.name.as_deref()?;
                let cluster = self.cluster_info();
                Some(IdentityMatch::Exact(format!(
                    "{}.{}.serviceaccount.{}.{}",
                    name, ns, cluster.control_plane_ns, cluster.identity_domain
                )))
            });

        let authns = identities.chain(identity_refs).collect::<Vec<_>>();

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
