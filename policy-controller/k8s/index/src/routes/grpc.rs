#![allow(dead_code, unused_variables)]

use anyhow::Result;
use kube::Resource;
use linkerd_policy_controller_core::http_route::{self, GroupKindName};
use linkerd_policy_controller_k8s_api::gateway as k8s_gateway_api;

pub fn try_match(
    k8s_gateway_api::GrpcRouteMatch { headers, method }: k8s_gateway_api::GrpcRouteMatch,
) -> Result<http_route::HttpRouteMatch> {
    todo!()
}

pub(crate) fn gkn_for_gateway_grpc_route(name: String) -> GroupKindName {
    GroupKindName {
        group: k8s_gateway_api::GrpcRoute::group(&()),
        kind: k8s_gateway_api::GrpcRoute::kind(&()),
        name: name.into(),
    }
}
