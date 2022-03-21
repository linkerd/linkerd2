use crate::ClusterInfo;
use linkerd_policy_controller_core::IdentityMatch;
use linkerd_policy_controller_k8s_api::{
    policy::MeshTLSAuthentication, ResourceExt, ServiceAccount,
};

#[derive(Debug, PartialEq)]
pub(crate) struct Spec {
    pub matches: Vec<IdentityMatch>,
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
            s.parse::<IdentityMatch>()
                .expect("identity match parsing is infallible")
        });

        let identity_refs = ma
            .spec
            .identity_refs
            .into_iter()
            .flatten()
            .filter_map(|tgt| {
                if tgt.targets_kind::<ServiceAccount>() {
                    let ns = tgt.namespace.as_deref().unwrap_or(&namespace);
                    let name = tgt.name.as_deref()?;
                    let id = cluster.service_account_identity(ns, name);
                    Some(IdentityMatch::Exact(id))
                } else {
                    None
                }
            });

        let matches = identities.chain(identity_refs).collect::<Vec<_>>();
        if matches.is_empty() {
            anyhow::bail!("No identities configured");
        }

        Ok(Spec { matches })
    }
}
