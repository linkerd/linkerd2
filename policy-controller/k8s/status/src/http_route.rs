use anyhow::{Error, Result};
use linkerd_policy_controller_k8s_api::{
    gateway,
    policy::{self, Server},
};

#[derive(Clone, PartialEq)]
pub struct RouteBinding {
    parents: Vec<ParentReference>,
}

#[derive(Clone, PartialEq)]
enum ParentReference {
    Server(String),
}

#[derive(Clone, Debug, thiserror::Error)]
pub enum InvalidParentReference {
    #[error("HTTPRoute resource does not reference a Server resource")]
    DoesNotSelectServer,

    #[error("HTTPRoute resource may not reference a parent by port")]
    SpecifiesPort,

    #[error("HTTPRoute resource may not reference a parent by section name")]
    SpecifiesSection,
}

impl TryFrom<policy::HttpRoute> for RouteBinding {
    type Error = Error;

    fn try_from(value: policy::HttpRoute) -> Result<Self, Self::Error> {
        let parents = ParentReference::collect_from(value.spec.inner.parent_refs)?;
        Ok(RouteBinding { parents })
    }
}

impl RouteBinding {
    pub fn selects_server(&self, name: &str) -> bool {
        self.parents
            .iter()
            .any(|p| matches!(p, ParentReference::Server(n) if n == name))
    }
}

impl ParentReference {
    fn collect_from(
        parent_refs: Option<Vec<gateway::ParentReference>>,
    ) -> Result<Vec<Self>, InvalidParentReference> {
        let parents = parent_refs
            .into_iter()
            .flatten()
            .filter_map(Self::from_parent_ref)
            .collect::<Result<Vec<_>, InvalidParentReference>>()?;
        if parents.is_empty() {
            return Err(InvalidParentReference::DoesNotSelectServer);
        }

        Ok(parents)
    }

    fn from_parent_ref(
        parent_ref: gateway::ParentReference,
    ) -> Option<Result<Self, InvalidParentReference>> {
        if !policy::httproute::parent_ref_targets_kind::<Server>(&parent_ref)
            || parent_ref.name.is_empty()
        {
            return None;
        }

        let gateway::ParentReference {
            group: _,
            kind: _,
            namespace: _,
            name,
            section_name,
            port,
        } = parent_ref;
        if port.is_some() {
            return Some(Err(InvalidParentReference::SpecifiesPort));
        }
        if section_name.is_some() {
            return Some(Err(InvalidParentReference::SpecifiesSection));
        }

        Some(Ok(ParentReference::Server(name)))
    }
}
