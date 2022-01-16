use crate::{authz::AuthzIndex, pod::PodIndex, server::SrvIndex, DefaultPolicy};
use ahash::AHashMap as HashMap;

#[derive(Debug)]
pub(crate) struct NamespaceIndex {
    pub index: HashMap<String, Namespace>,

    // The global default-allow policy.
    default_policy: DefaultPolicy,
}

#[derive(Debug)]
pub(crate) struct Namespace {
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

    pub fn iter(&self) -> impl Iterator<Item = (&String, &Namespace)> + ExactSizeIterator {
        self.index.iter()
    }
}
