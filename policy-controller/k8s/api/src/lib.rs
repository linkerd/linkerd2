#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

pub mod labels;
pub mod policy;

pub use self::{labels::Labels, watcher::Event};
pub use k8s_openapi::api::{
    self,
    core::v1::{Namespace, Node, NodeSpec, Pod, PodSpec, PodStatus},
};
pub use kube::api::{ObjectMeta, ResourceExt};
use kube::runtime::watcher;
