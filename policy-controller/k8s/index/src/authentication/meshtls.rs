use crate::ClusterInfo;
use ahash::{AHashMap as HashMap, AHashSet as HashSet};
use anyhow::Result;
use linkerd_policy_controller_core::IdentityMatch;
use linkerd_policy_controller_k8s_api::{
    policy::MeshTLSAuthentication, Namespace, ResourceExt, ServiceAccount,
};
use std::collections::hash_map::Entry;
use tracing::info_span;

#[derive(Debug, PartialEq)]
pub(crate) struct Spec {
    pub matches: Vec<IdentityMatch>,
}

impl kubert::index::IndexNamespacedResource<MeshTLSAuthentication> for crate::Index {
    fn apply(&mut self, authn: MeshTLSAuthentication) {
        let ns = authn
            .namespace()
            .expect("MeshTLSAuthentication must have a namespace");
        let name = authn.name_unchecked();
        let _span = info_span!("apply", %ns, %name).entered();

        let spec = match Spec::try_from_resource(authn, &self.cluster_info) {
            Ok(spec) => spec,
            Err(error) => {
                tracing::warn!(%error, "Invalid MeshTLSAuthentication");
                return;
            }
        };

        if self.authentications.update_meshtls(ns, name, spec) {
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
        authns: Vec<MeshTLSAuthentication>,
        deleted: HashMap<String, HashSet<String>>,
    ) {
        let _span = info_span!("reset");

        let mut changed = false;

        for authn in authns.into_iter() {
            let namespace = authn
                .namespace()
                .expect("meshtlsauthentication must be namespaced");
            let name = authn.name_unchecked();
            let spec = match Spec::try_from_resource(authn, &self.cluster_info) {
                Ok(spec) => spec,
                Err(error) => {
                    tracing::warn!(ns = %namespace, %name, %error, "Invalid MeshTLSAuthentication");
                    return;
                }
            };
            changed = self.authentications.update_meshtls(namespace, name, spec) || changed;
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

impl Spec {
    pub(crate) fn try_from_resource(
        ma: MeshTLSAuthentication,
        cluster: &ClusterInfo,
    ) -> anyhow::Result<Self> {
        let namespace = ma
            .namespace()
            .expect("MeshTLSAuthentication must have a namespace");

        let identities = ma.spec.identities.into_iter().flatten().map(|s| {
            Ok(s.parse::<IdentityMatch>()
                .expect("identity match parsing is infallible"))
        });

        let identity_refs = ma.spec.identity_refs.into_iter().flatten().map(|tgt| {
            if tgt.targets_kind::<ServiceAccount>() {
                let ns = tgt.namespace.as_deref().unwrap_or(&namespace);
                let id = cluster.service_account_identity(ns, &tgt.name);
                Ok(IdentityMatch::Exact(id))
            } else if tgt.targets_kind::<Namespace>() {
                let id = cluster.namespace_identity(tgt.name.as_str());
                Ok(id.parse::<IdentityMatch>()?)
            } else {
                anyhow::bail!("unsupported target type: {:?}", tgt.canonical_kind())
            }
        });

        let matches = identities
            .chain(identity_refs)
            .collect::<Result<Vec<_>>>()?;
        if matches.is_empty() {
            anyhow::bail!("No identities configured");
        }

        Ok(Spec { matches })
    }
}
