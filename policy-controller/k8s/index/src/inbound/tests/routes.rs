use super::*;

mod grpc;
mod http;

const POLICY_API_GROUP: &str = "policy.linkerd.io";

fn mk_authorization_policy(
    ns: impl ToString,
    name: impl ToString,
    route: impl ToString,
    authns: impl IntoIterator<Item = NamespacedTargetRef>,
) -> k8s::policy::AuthorizationPolicy {
    k8s::policy::AuthorizationPolicy {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: k8s::policy::AuthorizationPolicySpec {
            target_ref: LocalTargetRef {
                group: Some(POLICY_API_GROUP.to_string()),
                kind: "HttpRoute".to_string(),
                name: route.to_string(),
            },
            required_authentication_refs: authns.into_iter().collect(),
        },
    }
}
