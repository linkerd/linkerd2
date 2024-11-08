use anyhow::Result;
use chrono::{offset::Utc, DateTime};
use linkerd_policy_controller_k8s_api::{
    self as k8s,
    policy::{LocalTargetRef, NamespacedTargetRef},
    ServiceAccount, Time,
};

#[derive(Debug, PartialEq)]
pub(crate) struct Spec {
    pub creation_timestamp: Option<DateTime<Utc>>,
    pub target: Target,
    pub total: Option<Limit>,
    pub identity: Option<Limit>,
    pub overrides: Vec<Override>,
}

#[derive(Debug, PartialEq)]
pub(crate) enum Target {
    Server(String),
}

#[derive(Debug, PartialEq)]
pub(crate) struct Limit {
    pub requests_per_second: u32,
}

#[derive(Debug, PartialEq)]
pub(crate) struct Override {
    pub requests_per_second: u32,
    pub client_refs: Vec<ClientRef>,
}

#[derive(Debug, PartialEq)]
pub(crate) enum ClientRef {
    ServiceAccount {
        namespace: Option<String>,
        name: String,
    },
}

impl TryFrom<k8s::policy::HTTPLocalRateLimitPolicy> for Spec {
    type Error = anyhow::Error;

    fn try_from(rl: k8s::policy::HTTPLocalRateLimitPolicy) -> Result<Self> {
        let creation_timestamp = rl.metadata.creation_timestamp.map(|Time(t)| t);
        Ok(Self {
            creation_timestamp,
            target: target(rl.spec.target_ref)?,
            total: rl.spec.total.map(|lim| Limit {
                requests_per_second: lim.requests_per_second,
            }),
            identity: rl.spec.identity.map(|lim| Limit {
                requests_per_second: lim.requests_per_second,
            }),
            overrides: rl
                .spec
                .overrides
                .unwrap_or_default()
                .into_iter()
                .map(|o| {
                    let client_refs = o
                        .client_refs
                        .into_iter()
                        .map(client_ref)
                        .collect::<Result<Vec<_>>>();
                    client_refs.map(|refs| Override {
                        requests_per_second: o.requests_per_second,
                        client_refs: refs,
                    })
                })
                .collect::<Result<Vec<_>>>()?,
        })
    }
}

fn target(t: LocalTargetRef) -> Result<Target> {
    match t {
        t if t.targets_kind::<k8s::policy::Server>() => Ok(Target::Server(t.name)),
        _ => anyhow::bail!("unsupported rate limit target type: {}", t.canonical_kind()),
    }
}

fn client_ref(t: NamespacedTargetRef) -> Result<ClientRef> {
    if t.targets_kind::<ServiceAccount>() {
        Ok(ClientRef::ServiceAccount {
            namespace: t.namespace.map(Into::into),
            name: t.name,
        })
    } else {
        anyhow::bail!("unsupported client reference: {}", t.canonical_kind());
    }
}
