use crate::{Index, SharedIndex};
use futures::prelude::*;
use linkerd_policy_controller_k8s_api::{
    self as k8s, policy::authorization_policy as api, ResourceExt,
};
use tracing::{instrument, warn};

pub async fn index(
    idx: SharedIndex,
    events: impl Stream<Item = k8s::WatchEvent<api::AuthorizationPolicy>>,
) {
    tokio::pin!(events);
    while let Some(ev) = events.next().await {
        match ev {
            k8s::WatchEvent::Applied(ap) => apply(&mut *idx.write(), ap),
            k8s::WatchEvent::Deleted(ap) => delete(&mut *idx.write(), ap),
            k8s::WatchEvent::Restarted(aps) => restart(&mut *idx.write(), aps),
        }
    }
}

/// Obtains or constructs an `Authz` and links it to the appropriate `Servers`.
#[instrument(skip_all, fields(
    ns = %policy.namespace().unwrap(),
    name = %policy.name(),
))]
fn apply(index: &mut Index, policy: api::AuthorizationPolicy) {
    let _ns = index
        .namespaces
        .get_or_default(policy.namespace().expect("namespace required"));
    unimplemented!()
}

#[instrument(skip_all, fields(
    ns = %policy.namespace().unwrap(),
    name = %policy.name(),
))]
fn delete(index: &mut Index, policy: api::AuthorizationPolicy) {
    if let Some(_ns) = index
        .namespaces
        .index
        .get_mut(policy.namespace().unwrap().as_str())
    {
        unimplemented!()
    }
}

#[instrument(skip_all)]
fn restart(_index: &mut Index, _policies: Vec<api::AuthorizationPolicy>) {
    unimplemented!()
}
