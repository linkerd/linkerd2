use kube::Resource;
use linkerd_policy_controller_core::http_route::GroupKindName;

pub(crate) fn gkn_for_gateway_grpc_route(name: String) -> GroupKindName {
    GroupKindName {
        group: k8s_gateway_api::GrpcRoute::group(&()),
        kind: k8s_gateway_api::GrpcRoute::kind(&()),
        name: name.into(),
    }
}
