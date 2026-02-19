use crate::resource_id::ResourceId;
use linkerd_policy_controller_k8s_api::policy as linkerd_k8s_api;

#[derive(Clone, Eq, PartialEq, Debug)]
pub enum TargetReference {
    Server(ResourceId),
    UnknownKind,
}

impl TargetReference {
    pub(crate) fn make_target_ref(
        namespace: &str,
        cl: &linkerd_k8s_api::ConcurrencyLimitPolicySpec,
    ) -> TargetReference {
        if cl.target_ref.targets_kind::<linkerd_k8s_api::Server>() {
            Self::Server(ResourceId::new(
                namespace.to_string(),
                cl.target_ref.name.clone(),
            ))
        } else {
            Self::UnknownKind
        }
    }
}
