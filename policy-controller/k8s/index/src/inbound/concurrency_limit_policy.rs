use anyhow::Result;
use chrono::{offset::Utc, DateTime};
use linkerd_policy_controller_k8s_api::{self as k8s, policy::LocalTargetRef, Time};
use std::fmt;

#[derive(Debug, PartialEq)]
pub(crate) struct Spec {
    pub creation_timestamp: Option<DateTime<Utc>>,
    pub target: Target,
    pub max_in_flight_requests: u32,
    pub status: Status,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub(crate) enum Target {
    Server(String),
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

impl TryFrom<k8s::policy::HttpLocalConcurrencyLimitPolicy> for Spec {
    type Error = anyhow::Error;

    fn try_from(cl: k8s::policy::HttpLocalConcurrencyLimitPolicy) -> Result<Self> {
        let creation_timestamp = cl.metadata.creation_timestamp.map(|Time(t)| t);
        let conditions = cl.status.map_or(vec![], |status| {
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
                            return None;
                        }
                    };
                    Some(Condition { type_, status })
                })
                .collect()
        });
        let target = target(cl.spec.target_ref)?;

        Ok(Self {
            creation_timestamp,
            target: target.clone(),
            max_in_flight_requests: cl.spec.max_in_flight_requests,
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
        _ => anyhow::bail!(
            "unsupported concurrency limit target type: {}",
            t.canonical_kind()
        ),
    }
}
