#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

pub mod labels;
pub mod policy;

pub use self::labels::Labels;
pub use k8s_gateway_api as gateway;
pub use k8s_openapi::{
    api::{
        self,
        core::v1::{
            Container, ContainerPort, HTTPGetAction, Namespace, Node, NodeSpec, Pod, PodSpec,
            PodStatus, Probe, Service, ServiceAccount,
        },
    },
    apimachinery::{
        self,
        pkg::{
            apis::meta::v1::{Condition, Time},
            util::intstr::IntOrString,
        },
    },
};
pub use kube::{
    api::{Api, ListParams, ObjectMeta, Patch, PatchParams, Resource, ResourceExt},
    runtime::watcher::Event as WatchEvent,
    Client,
};
