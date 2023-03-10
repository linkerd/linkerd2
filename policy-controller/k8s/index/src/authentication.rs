use ahash::AHashMap as HashMap;
use std::collections::hash_map::Entry;

pub mod meshtls;
pub mod network;

/// Holds all `NetworkAuthentication` and `MeshTLSAuthentication` indices by-namespace.
///
/// This is separate from `NamespaceIndex` because authorization policies may reference
/// authentication resources across namespaces.
#[derive(Debug, Default)]
pub(crate) struct AuthenticationNsIndex {
    pub(crate) by_ns: HashMap<String, AuthenticationIndex>,
}

#[derive(Debug, Default)]
pub(crate) struct AuthenticationIndex {
    pub(crate) meshtls: HashMap<String, meshtls::Spec>,
    pub(crate) network: HashMap<String, network::Spec>,
}

// === impl AuthenticationNsIndex ===

impl AuthenticationNsIndex {
    pub(crate) fn update_meshtls(
        &mut self,
        namespace: String,
        name: String,
        spec: meshtls::Spec,
    ) -> bool {
        match self.by_ns.entry(namespace).or_default().meshtls.entry(name) {
            Entry::Vacant(entry) => {
                entry.insert(spec);
            }
            Entry::Occupied(mut entry) => {
                if *entry.get() == spec {
                    return false;
                }
                entry.insert(spec);
            }
        }

        true
    }

    fn update_network(&mut self, namespace: String, name: String, spec: network::Spec) -> bool {
        match self.by_ns.entry(namespace).or_default().network.entry(name) {
            Entry::Vacant(entry) => {
                entry.insert(spec);
            }
            Entry::Occupied(mut entry) => {
                if *entry.get() == spec {
                    return false;
                }
                entry.insert(spec);
            }
        }

        true
    }
}

// === impl AuthenticationIndex ===

impl AuthenticationIndex {
    #[inline]
    fn is_empty(&self) -> bool {
        self.meshtls.is_empty() && self.network.is_empty()
    }
}
