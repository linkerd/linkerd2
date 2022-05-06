use anyhow::{bail, Result};
use linkerd_policy_controller_k8s_api::{
    self as k8s,
    policy::{LocalTargetRef, NamespacedTargetRef},
};

#[derive(Debug, PartialEq)]
pub(crate) struct Spec {
    pub target: Target,
    pub authentications: Vec<AuthenticationTarget>,
}

#[derive(Debug, PartialEq)]
pub(crate) enum Target {
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
        if authentications.is_empty() {
            bail!("No authentication targets");
        }

        Ok(Self {
            target,
            authentications,
        })
    }
}

fn target(t: LocalTargetRef) -> Result<Target> {
    if t.targets_kind::<k8s::policy::Server>() {
        return Ok(Target::Server(t.name));
    }
    if t.targets_kind::<k8s::Namespace>() {
        return Ok(Target::Namespace);
    }

    anyhow::bail!(
        "unsupported authorization target type: {}",
        t.canonical_kind()
    );
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
    } else {
        anyhow::bail!("unsupported authentication target: {}", t.canonical_kind());
    }
}
