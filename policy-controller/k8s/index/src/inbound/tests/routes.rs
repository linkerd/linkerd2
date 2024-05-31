use super::*;
use crate::routes::ImpliedGKN;
use kube::Resource;

mod http;

const POLICY_API_GROUP: &str = "policy.linkerd.io";

fn mk_authorization_policy<Route: ResourceExt + Resource<DynamicType = ()>>(
    name: impl ToString,
    route: &Route,
    authns: impl IntoIterator<Item = NamespacedTargetRef>,
) -> k8s::policy::AuthorizationPolicy {
    let gkn = route.gkn();

    k8s::policy::AuthorizationPolicy {
        metadata: k8s::ObjectMeta {
            namespace: route.namespace().clone(),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: k8s::policy::AuthorizationPolicySpec {
            target_ref: LocalTargetRef {
                group: Some(gkn.group.to_string()),
                kind: gkn.kind.to_string(),
                name: gkn.name.to_string(),
            },
            required_authentication_refs: authns.into_iter().collect(),
        },
    }
}
