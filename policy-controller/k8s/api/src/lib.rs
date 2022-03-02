#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

pub mod labels;
pub mod policy;

pub use self::{labels::Labels, watcher::Event};
use futures::prelude::*;
pub use k8s_openapi::api::{
    self,
    core::v1::{Namespace, Node, NodeSpec, Pod, PodSpec, PodStatus},
};
pub use kube::api::{ObjectMeta, ResourceExt};
use kube::runtime::watcher;
use parking_lot::Mutex;
use std::sync::Arc;

pub type EventResult<T> = watcher::Result<watcher::Event<T>>;

/// Processes an `E`-typed event stream of `T`-typed resources.
///
/// The `F`-typed processor is called for each event with exclusive mutable accecss to an `S`-typed
/// store.
///
/// The `H`-typed initialization handle is dropped after the first event is processed to signal to
/// the application that the index has been updated.
///
/// It is assumed that the event stream is infinite. If an error is encountered, the stream is
/// immediately polled again; and if this attempt to read from the stream fails, a backoff is
/// employed before attempting to read from the stream again.
pub async fn index<T, E, H, S, F>(events: E, store: Arc<Mutex<S>>, process: F)
where
    E: Stream<Item = watcher::Event<T>>,
    F: Fn(&mut S, watcher::Event<T>),
{
    tokio::pin!(events);
    while let Some(ev) = events.next().await {
        process(&mut *store.lock(), ev);
    }

    tracing::warn!("k8s event stream terminated");
}
