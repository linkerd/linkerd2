use anyhow::Result;
use chrono::{offset::Utc, DateTime};
use linkerd_policy_controller_k8s_api::{
    self as k8s,
    policy::{LocalTargetRef, NamespacedTargetRef},
    ServiceAccount, Time,
};
use std::fmt;

#[derive(Debug, PartialEq)]
pub(crate) struct Spec {
    pub creation_timestamp: Option<DateTime<Utc>>,
    pub target: Target,
    pub total: Option<Limit>,
    pub identity: Option<Limit>,
    pub overrides: Vec<Override>,
    pub status: Status,
}

#[derive(Clone, Debug, Eq, PartialEq)]
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

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Status {
    pub conditions: Vec<Condition>,
    pub target: Target,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Condition {
    pub type_: ConditionType,
    pub status: bool,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ConditionType {
    Accepted,
}

impl Spec {
    pub fn accepted_by_server(&self, name: &str) -> bool {
        self.status.target == Target::Server(name.to_string())
            && self
                .status
                .conditions
                .iter()
                .any(|condition| condition.type_ == ConditionType::Accepted && condition.status)
    }
}

impl TryFrom<k8s::policy::HttpLocalRateLimitPolicy> for Spec {
    type Error = anyhow::Error;

    fn try_from(rl: k8s::policy::HttpLocalRateLimitPolicy) -> Result<Self> {
        let creation_timestamp = rl.metadata.creation_timestamp.map(|Time(t)| t);
        let conditions = rl.status.map_or(vec![], |status| {
            status
            .conditions
            .iter()
            .filter_map(|condition| {
                let type_ = match condition.type_.as_ref() {
                    "Accepted" => ConditionType::Accepted,
                    condition_type => {
                        tracing::warn!(%status.target_ref.name, %condition_type, "Unexpected condition type found in status");
                        return None;
                    }
                };
                let status = match condition.status.as_ref() {
                    "True" => true,
                    "False" => false,
                    condition_status => {
                        tracing::warn!(%status.target_ref.name, %type_, %condition_status, "Unexpected condition status found in status");
                        return None
                    },
                };
                Some(Condition { type_, status })
            }).collect()
        });
        let target = target(rl.spec.target_ref)?;

        Ok(Self {
            creation_timestamp,
            target: target.clone(),
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
            status: Status { conditions, target },
        })
    }
}

impl fmt::Display for ConditionType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Accepted => write!(f, "Accepted"),
        }
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
