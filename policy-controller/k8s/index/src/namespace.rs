use crate::{authz::AuthzIndex, pod::PodIndex, server::SrvIndex, DefaultAllow};
use std::collections::HashMap;

#[derive(Debug)]
pub(crate) struct NamespaceIndex {
    pub index: HashMap<String, Namespace>,

    // The global default-allow policy.
    default_allow: DefaultAllow,
}

#[derive(Debug)]
pub(crate) struct Namespace {
    /// Holds the global default-allow policy, which may be overridden per-workload.
    pub default_allow: DefaultAllow,

    pub pods: PodIndex,
    pub servers: SrvIndex,
    pub authzs: AuthzIndex,
}

// === impl Namespaces ===

impl NamespaceIndex {
    pub fn new(default_allow: DefaultAllow) -> Self {
        Self {
            default_allow,
            index: HashMap::default(),
        }
    }

    pub fn get_or_default(&mut self, name: impl Into<String>) -> &mut Namespace {
        let default_allow = self.default_allow;
        self.index.entry(name.into()).or_insert_with(|| Namespace {
            default_allow,
            pods: PodIndex::default(),
            servers: SrvIndex::default(),
            authzs: AuthzIndex::default(),
        })
    }

    pub fn iter(&self) -> impl Iterator<Item = (&String, &Namespace)> {
        self.index.iter()
    }
}
