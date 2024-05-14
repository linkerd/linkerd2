#![allow(dead_code, unused_imports)]

use kube::ResourceExt;
use linkerd_policy_controller_k8s_api::{self as k8s, gateway as k8s_gateway_api};
use linkerd_policy_test::{
    await_condition, await_route_status, create, find_route_condition, mk_route, update,
    with_temp_ns,
};

#[ignore = "not implemented yet"]
#[tokio::test(flavor = "current_thread")]
async fn inbound_accepted_parent() {
    todo!()
}

#[ignore = "not implemented yet"]
#[tokio::test(flavor = "current_thread")]
async fn inbound_multiple_parents() {
    todo!()
}

#[ignore = "not implemented yet"]
#[tokio::test(flavor = "current_thread")]
async fn inbound_no_parent_ref_patch() {
    todo!()
}

#[ignore = "not implemented yet"]
#[tokio::test(flavor = "current_thread")]
// Tests that inbound routes (routes attached to a `Server`) are properly
// reconciled when the parentReference changes. Additionally, tests that routes
// whose parentRefs do not exist are patched with an appropriate status.
async fn inbound_accepted_reconcile_no_parent() {
    todo!()
}

#[ignore = "not implemented yet"]
#[tokio::test(flavor = "current_thread")]
async fn inbound_accepted_reconcile_parent_delete() {
    todo!()
}
