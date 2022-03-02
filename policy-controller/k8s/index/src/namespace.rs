use crate::{pod::PodIndex, server::SrvIndex, server_authorization::AuthzIndex, DefaultPolicy};
use ahash::AHashMap as HashMap;

#[derive(Debug)]
pub(crate) struct NamespaceIndex {
    pub index: HashMap<String, Namespace>,

    // The global default-allow policy.
    default_policy: DefaultPolicy,
}

#[derive(Debug)]
pub struct Namespace {
    /// Holds the global default-allow policy, which may be overridden per-workload.
    pub default_policy: DefaultPolicy,

    pub pods: PodIndex,
    pub servers: SrvIndex,
    pub authzs: AuthzIndex,
}

// === impl Namespaces ===

impl NamespaceIndex {
    pub fn new(default_policy: DefaultPolicy) -> Self {
        Self {
            default_policy,
            index: HashMap::default(),
        }
    }

    pub fn get_or_default(&mut self, name: impl Into<String>) -> &mut Namespace {
        let default_policy = self.default_policy;
        self.index.entry(name.into()).or_insert_with(|| Namespace {
            default_policy,
            pods: PodIndex::default(),
            servers: SrvIndex::default(),
            authzs: AuthzIndex::default(),
        })
    }

    pub fn iter(&self) -> std::collections::hash_map::Iter<'_, String, Namespace> {
        self.index.iter()
    }
}
