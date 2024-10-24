mod grpc;
mod http;
mod tcp;
mod tls;

enum BackendKind {
    Egress,
    Service,
}
