use anyhow::Result;
use linkerd_policy_controller_k8s_api::{
    self as k8s,
    policy::{LocalTargetRef, NamespacedTargetRef},
    ServiceAccount,
};

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
