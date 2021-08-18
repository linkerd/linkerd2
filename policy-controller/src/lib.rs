#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

pub use linkerd_policy_controller_grpc as grpc;
pub use linkerd_policy_controller_k8s_index as k8s;
pub use linkerd_policy_controller_k8s_api as api;

pub mod admin;
pub mod admission;