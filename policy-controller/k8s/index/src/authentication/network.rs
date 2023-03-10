use ahash::{AHashMap as HashMap, AHashSet as HashSet};
use linkerd_policy_controller_core::NetworkMatch;
use linkerd_policy_controller_k8s_api::{
    policy::{NetworkAuthentication, NetworkAuthenticationSpec},
    ResourceExt,
};
use std::collections::hash_map::Entry;
use tracing::info_span;

#[derive(Debug, PartialEq)]
pub(crate) struct Spec {
    pub matches: Vec<NetworkMatch>,
}

impl kubert::index::IndexNamespacedResource<NetworkAuthentication> for crate::Index {
    fn apply(&mut self, authn: NetworkAuthentication) {
        let ns = authn.namespace().unwrap();
        let name = authn.name_unchecked();
        let _span = info_span!("apply", %ns, %name).entered();

        let spec = match Spec::try_from(authn.spec) {
            Ok(spec) => spec,
            Err(error) => {
                tracing::warn!(%error, "Invalid NetworkAuthentication");
                return;
            }
        };

        if self.authentications.update_network(ns, name, spec) {
            self.reindex_all();
        }
    }

    fn delete(&mut self, ns: String, name: String) {
        let _span = info_span!("delete", %ns, %name).entered();

        if let Entry::Occupied(mut ns) = self.authentications.by_ns.entry(ns) {
            tracing::debug!("Deleting MeshTLSAuthentication");

            ns.get_mut().network.remove(&name);
            if ns.get().is_empty() {
                ns.remove();
            }
            self.reindex_all();
        } else {
            tracing::warn!("Namespace already deleted!");
        }
    }

    fn reset(
        &mut self,
        authns: Vec<NetworkAuthentication>,
        deleted: HashMap<String, HashSet<String>>,
    ) {
        let _span = info_span!("reset");

        let mut changed = false;

        for authn in authns.into_iter() {
            let namespace = authn
                .namespace()
                .expect("meshtlsauthentication must be namespaced");
            let name = authn.name_unchecked();
            let spec = match Spec::try_from(authn.spec) {
                Ok(spec) => spec,
                Err(error) => {
                    tracing::warn!(ns = %namespace, %name, %error, "Invalid NetworkAuthentication");
                    return;
                }
            };
            changed = self.authentications.update_network(namespace, name, spec) || changed;
        }
        for (namespace, names) in deleted.into_iter() {
            if let Entry::Occupied(mut ns) = self.authentications.by_ns.entry(namespace) {
                for name in names.into_iter() {
                    ns.get_mut().meshtls.remove(&name);
                }
                if ns.get().is_empty() {
                    ns.remove();
                }
            }
        }

        if changed {
            self.reindex_all();
        }
    }
}

impl TryFrom<NetworkAuthenticationSpec> for Spec {
    type Error = anyhow::Error;

    fn try_from(spec: NetworkAuthenticationSpec) -> anyhow::Result<Self> {
        let matches = spec
            .networks
            .into_iter()
            .map(|n| NetworkMatch {
                net: n.cidr.into(),
                except: n.except.into_iter().flatten().map(Into::into).collect(),
            })
            .collect::<Vec<_>>();

        if matches.is_empty() {
            anyhow::bail!("No networks configured");
        }

        Ok(Spec { matches })
    }
}
