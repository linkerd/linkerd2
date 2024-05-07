#![allow(dead_code, unused_imports, unused_variables)]

use super::{super::*, *};
use crate::routes::grpc::gkn_for_gateway_grpc_route;
use linkerd_policy_controller_core::http_route::{HttpRouteMatch, Method, PathMatch};

#[test]
#[ignore = "not yet implemented"]
fn route_attaches_to_server() {
    todo!()
}

#[test]
#[ignore = "not yet implemented"]
fn routes_created_for_probes() {
    todo!()
}

fn mk_route(
    ns: impl ToString,
    name: impl ToString,
    server: impl ToString,
) -> k8s::gateway::GrpcRoute {
    todo!()
}
