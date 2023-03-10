use crate::NsUpdate;
use ahash::{AHashMap as HashMap, AHashSet as HashSet};
use anyhow::Result;
use linkerd_policy_controller_k8s_api::{
    self as k8s,
    policy::{LocalTargetRef, NamespacedTargetRef},
    ResourceExt, ServiceAccount,
};
use tracing::info_span;

#[derive(Debug, PartialEq)]
pub(crate) struct Spec {
    pub target: Target,
    pub authentications: Vec<AuthenticationTarget>,
}

#[derive(Debug, PartialEq)]
pub(crate) enum Target {
    HttpRoute(String),
    Server(String),
    Namespace,
}

#[derive(Debug, PartialEq)]
pub(crate) enum AuthenticationTarget {
    MeshTLS {
        namespace: Option<String>,
        name: String,
    },
    Network {
        namespace: Option<String>,
        name: String,
    },
    ServiceAccount {
        namespace: Option<String>,
        name: String,
    },
}

#[inline]
pub fn validate(ap: k8s::policy::AuthorizationPolicySpec) -> Result<()> {
    Spec::try_from(ap)?;
    Ok(())
}

impl kubert::index::IndexNamespacedResource<k8s::policy::AuthorizationPolicy> for crate::Index {
    fn apply(&mut self, policy: k8s::policy::AuthorizationPolicy) {
        let ns = policy.namespace().unwrap();
        let name = policy.name_unchecked();
        let _span = info_span!("apply", %ns, saz = %name).entered();

        let spec = match Spec::try_from(policy.spec) {
            Ok(spec) => spec,
            Err(error) => {
                tracing::warn!(%error, "Invalid authorization policy");
                return;
            }
        };

        self.ns_or_default_with_reindex(ns, |ns| ns.policy.update_authz_policy(name, spec))
    }

    fn delete(&mut self, ns: String, ap: String) {
        let _span = info_span!("delete", %ns, %ap).entered();
        self.ns_with_reindex(ns, |ns| {
            ns.policy.authorization_policies.remove(&ap).is_some()
        })
    }

    fn reset(
        &mut self,
        policies: Vec<k8s::policy::AuthorizationPolicy>,
        deleted: HashMap<String, HashSet<String>>,
    ) {
        let _span = info_span!("reset");

        // Aggregate all of the updates by namespace so that we only reindex
        // once per namespace.
        type Ns = NsUpdate<Spec>;
        let mut updates_by_ns = HashMap::<String, Ns>::default();
        for policy in policies.into_iter() {
            let namespace = policy
                .namespace()
                .expect("authorizationpolicy must be namespaced");
            let name = policy.name_unchecked();
            match Spec::try_from(policy.spec) {
                Ok(spec) => updates_by_ns
                    .entry(namespace)
                    .or_default()
                    .added
                    .push((name, spec)),
                Err(error) => {
                    tracing::error!(ns = %namespace, %name, %error, "Illegal server authorization update")
                }
            }
        }
        for (ns, names) in deleted.into_iter() {
            updates_by_ns.entry(ns).or_default().removed = names;
        }

        for (namespace, Ns { added, removed }) in updates_by_ns.into_iter() {
            if added.is_empty() {
                // If there are no live resources in the namespace, we do not
                // want to create a default namespace instance, we just want to
                // clear out all resources for the namespace (and then drop the
                // whole namespace, if necessary).
                self.ns_with_reindex(namespace, |ns| {
                    ns.policy.authorization_policies.clear();
                    true
                });
            } else {
                // Otherwise, we take greater care to reindex only when the
                // state actually changed. The vast majority of resets will see
                // no actual data change.
                self.ns_or_default_with_reindex(namespace, |ns| {
                    let mut changed = !removed.is_empty();
                    for name in removed.into_iter() {
                        ns.policy.authorization_policies.remove(&name);
                    }
                    for (name, spec) in added.into_iter() {
                        changed = ns.policy.update_authz_policy(name, spec) || changed;
                    }
                    changed
                });
            }
        }
    }
}

impl TryFrom<k8s::policy::AuthorizationPolicySpec> for Spec {
    type Error = anyhow::Error;

    fn try_from(ap: k8s::policy::AuthorizationPolicySpec) -> Result<Self> {
        let target = target(ap.target_ref)?;

        let authentications = ap
            .required_authentication_refs
            .into_iter()
            .map(authentication_ref)
            .collect::<Result<Vec<_>>>()?;

        Ok(Self {
            target,
            authentications,
        })
    }
}

fn target(t: LocalTargetRef) -> Result<Target> {
    match t {
        t if t.targets_kind::<k8s::policy::Server>() => Ok(Target::Server(t.name)),
        t if t.targets_kind::<k8s::Namespace>() => Ok(Target::Namespace),
        t if t.targets_kind::<k8s::policy::HttpRoute>() => Ok(Target::HttpRoute(t.name)),
        _ => anyhow::bail!(
            "unsupported authorization target type: {}",
            t.canonical_kind()
        ),
    }
}

fn authentication_ref(t: NamespacedTargetRef) -> Result<AuthenticationTarget> {
    if t.targets_kind::<k8s::policy::MeshTLSAuthentication>() {
        Ok(AuthenticationTarget::MeshTLS {
            namespace: t.namespace.map(Into::into),
            name: t.name,
        })
    } else if t.targets_kind::<k8s::policy::NetworkAuthentication>() {
        Ok(AuthenticationTarget::Network {
            namespace: t.namespace.map(Into::into),
            name: t.name,
        })
    } else if t.targets_kind::<ServiceAccount>() {
        Ok(AuthenticationTarget::ServiceAccount {
            namespace: t.namespace.map(Into::into),
            name: t.name,
        })
    } else {
        anyhow::bail!("unsupported authentication target: {}", t.canonical_kind());
    }
}
