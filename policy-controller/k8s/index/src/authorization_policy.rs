#![allow(dead_code)]

use anyhow::{bail, Result};
use linkerd_policy_controller_k8s_api::{self as k8s, policy::TargetRef};

#[derive(Debug)]
pub(crate) struct Spec {
    pub target: Target,
    pub authentications: Vec<AuthenticationTarget>,
}

#[derive(Debug)]
pub(crate) enum Target {
    Server(String),
}

#[derive(Debug)]
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

impl Target {
    pub(crate) fn server(&self) -> Option<&str> {
        match self {
            Self::Server(s) => Some(s),
        }
    }
}

fn target(t: TargetRef) -> Result<Target> {
    if let Some(name) = t.name {
        if let Some(group) = t.group {
            if group.eq_ignore_ascii_case("policy.linkerd.io")
                && t.kind.eq_ignore_ascii_case("Server")
            {
                return Ok(Target::Server(name));
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
