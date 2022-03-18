use crate::index::{AuthenticationTarget, AuthorizationPolicyTarget, Index};
use anyhow::Result;
use linkerd_policy_controller_k8s_api::{self as k8s, policy::TargetRef, ResourceExt};
use std::collections::hash_map::Entry;

impl kubert::index::IndexNamespacedResource<k8s::policy::AuthorizationPolicy> for Index {
    fn apply(&mut self, policy: k8s::policy::AuthorizationPolicy) {
        let namespace = policy.namespace().unwrap();
        let name = policy.name();

        let target = match target_ref(policy.spec.target_ref) {
            Ok(t) => t,
            Err(error) => {
                tracing::warn!(%namespace, %name, %error, "Invalid target ref");
                return;
            }
        };

        let authns = match policy
            .spec
            .required_authentication_refs
            .into_iter()
            .map(authentication_ref)
            .collect::<Result<Vec<_>>>()
        {
            Ok(a) => a,
            Err(error) => {
                tracing::warn!(%namespace, %name, %error, "Invalid authentication target");
                return;
            }
        };

        if authns.is_empty() {
            tracing::warn!("No authentication targets");
            return;
        }

        self.apply_authorization_policy(namespace, name, target, authns);
    }

    fn delete(&mut self, namespace: String, name: String) {
        if let Entry::Occupied(mut entry) = self.entry(namespace) {
            entry.get_mut().delete_authorization_policy(&*name);
            if entry.get().is_empty() {
                entry.remove();
            }
        }
    }
}

fn target_ref(t: TargetRef) -> Result<AuthorizationPolicyTarget> {
    if let Some(name) = t.name {
        if let Some(group) = t.group {
            if group.eq_ignore_ascii_case("policy.linkerd.io")
                && t.kind.eq_ignore_ascii_case("Server")
            {
                return Ok(AuthorizationPolicyTarget::Server(name));
            }

            anyhow::bail!("unsupported authorization target: {}.{}", group, t.kind);
        }

        anyhow::bail!("unsupported authorization target: {}", t.kind);
    }

    anyhow::bail!("authorization targets must have a 'name'");
}

fn authentication_ref(t: TargetRef) -> Result<AuthenticationTarget> {
    if let Some(name) = t.name {
        if let Some(group) = t.group {
            if group.eq_ignore_ascii_case("policy.linkerd.io") {
                if t.kind.eq_ignore_ascii_case("NetworkAuthentication") {
                    return Ok(AuthenticationTarget::Network {
                        namespace: t.namespace,
                        name,
                    });
                }

                if t.kind.eq_ignore_ascii_case("MeshTLSAuthentication") {
                    return Ok(AuthenticationTarget::MeshTLS {
                        namespace: t.namespace,
                        name,
                    });
                }
            }

            anyhow::bail!("unsupported authentication target: {}.{}", group, t.kind);
        }

        anyhow::bail!("unsupported authentication target: {}", t.kind);
    }

    anyhow::bail!("authentication targets must have a 'name'");
}
