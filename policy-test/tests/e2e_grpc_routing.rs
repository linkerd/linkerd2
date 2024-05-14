use linkerd_policy_controller_k8s_api::{self as k8s, gateway as k8s_gateway_api};

#[allow(unused_imports)]
use linkerd_policy_test::{create, create_ready_pod, curl, web, with_temp_ns, LinkerdInject};

#[ignore = "not implemented yet"]
#[tokio::test(flavor = "current_thread")]
async fn service_based_routing() {
    todo!()
}

#[ignore = "not implemented yet"]
#[tokio::test(flavor = "current_thread")]
async fn method_based_routing() {
    todo!()
}

#[ignore = "not implemented yet"]
#[tokio::test(flavor = "current_thread")]
async fn service_and_method_routing() {
    todo!()
}

// === helpers ===

#[allow(dead_code)]
fn rule(
    method: impl ToString,
    service: impl ToString,
    backend: impl ToString,
) -> k8s_gateway_api::GrpcRouteRule {
    k8s_gateway_api::GrpcRouteRule {
        filters: None,
        matches: Some(vec![k8s_gateway_api::GrpcRouteMatch {
            method: Some(k8s_gateway_api::GrpcMethodMatch::Exact {
                method: Some(method.to_string()),
                service: Some(service.to_string()),
            }),
            ..Default::default()
        }]),
        backend_refs: Some(vec![k8s_gateway_api::GrpcRouteBackendRef {
            weight: None,
            filters: None,
            inner: k8s::gateway::BackendObjectReference {
                kind: None,
                group: None,
                namespace: None,
                port: Some(8080),
                name: backend.to_string(),
            },
        }]),
    }
}
