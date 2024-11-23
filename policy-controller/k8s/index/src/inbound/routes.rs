use anyhow::Result;
use linkerd_policy_controller_core::POLICY_CONTROLLER_NAME;
use linkerd_policy_controller_k8s_api::{gateway, policy};
use std::fmt;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RouteBinding<R> {
    pub parents: Vec<ParentRef>,
    pub route: R,
    pub statuses: Vec<Status>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ParentRef {
    Server(String),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Status {
    pub parent: ParentRef,
    pub conditions: Vec<Condition>,
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

#[derive(Clone, Debug, thiserror::Error)]
pub enum InvalidParentRef {
    #[error("route resource may not reference a parent Server in an other namespace")]
    ServerInAnotherNamespace,

    #[error("route resource may not reference a parent by port")]
    SpecifiesPort,

    #[error("route resource may not reference a parent by section name")]
    SpecifiesSection,
}

impl<R> RouteBinding<R> {
    #[inline]
    pub fn selects_server(&self, name: &str) -> bool {
        self.parents
            .iter()
            .any(|p| matches!(p, ParentRef::Server(n) if n == name))
    }

    #[inline]
    pub fn accepted_by_server(&self, name: &str) -> bool {
        self.statuses.iter().any(|status| {
            status.parent == ParentRef::Server(name.to_string())
                && status
                    .conditions
                    .iter()
                    .any(|condition| condition.type_ == ConditionType::Accepted && condition.status)
        })
    }
}

impl ParentRef {
    pub(crate) fn collect_from(
        route_ns: Option<&str>,
        parent_refs: Option<Vec<gateway::ParentReference>>,
    ) -> Result<Vec<Self>, InvalidParentRef> {
        let parents = parent_refs
            .into_iter()
            .flatten()
            .filter_map(|parent_ref| Self::from_parent_ref(route_ns, parent_ref))
            .collect::<Result<Vec<_>, InvalidParentRef>>()?;

        Ok(parents)
    }

    fn from_parent_ref(
        route_ns: Option<&str>,
        parent_ref: gateway::ParentReference,
    ) -> Option<Result<Self, InvalidParentRef>> {
        // Skip parent refs that don't target a `Server` resource.
        if !policy::httproute::parent_ref_targets_kind::<policy::Server>(&parent_ref)
            || parent_ref.name.is_empty()
        {
            return None;
        }

        let gateway::ParentReference {
            group: _,
            kind: _,
            namespace,
            name,
            section_name,
            port,
        } = parent_ref;

        if namespace.is_some() && namespace.as_deref() != route_ns {
            return Some(Err(InvalidParentRef::ServerInAnotherNamespace));
        }
        if port.is_some() {
            return Some(Err(InvalidParentRef::SpecifiesPort));
        }
        if section_name.is_some() {
            return Some(Err(InvalidParentRef::SpecifiesSection));
        }

        Some(Ok(ParentRef::Server(name)))
    }
}

impl Status {
    pub fn collect_from(status: gateway::RouteStatus) -> Vec<Self> {
        status
            .parents
            .iter()
            .filter(|status| status.controller_name == POLICY_CONTROLLER_NAME)
            .filter_map(Self::from_parent_status)
            .collect::<Vec<_>>()
    }

    fn from_parent_status(status: &gateway::RouteParentStatus) -> Option<Self> {
        // Only match parent statuses that belong to resources of
        // `kind: Server`.
        match status.parent_ref.kind.as_deref() {
            Some("Server") => (),
            _ => return None,
        }

        let conditions = status
            .conditions
            .iter()
            .filter_map(|condition| {
                let type_ = match condition.type_.as_ref() {
                    "Accepted" => ConditionType::Accepted,
                    condition_type => {
                        tracing::warn!(%status.parent_ref.name, %condition_type, "Unexpected condition type found in parent status");
                        return None;
                    }
                };
                let status = match condition.status.as_ref() {
                    "True" => true,
                    "False" => false,
                    condition_status => {
                        tracing::warn!(%status.parent_ref.name, %type_, %condition_status, "Unexpected condition status found in parent status");
                        return None
                    },
                };
                Some(Condition { type_, status })
            })
            .collect();

        Some(Status {
            parent: ParentRef::Server(status.parent_ref.name.to_string()),
            conditions,
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
