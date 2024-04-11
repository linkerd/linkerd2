use crate::http_route::BackendReference;

pub(crate) fn make_backends(
    namespace: &str,
    backends: impl Iterator<Item = k8s_gateway_api::GrpcRouteBackendRef>,
) -> Vec<BackendReference> {
    backends
        .map(|backend_ref| BackendReference::from_backend_ref(&backend_ref.inner, namespace))
        .collect()
}
