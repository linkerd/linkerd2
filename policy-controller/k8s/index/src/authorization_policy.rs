use crate::{Index, SharedIndex};
use futures::prelude::*;
use linkerd_policy_controller_k8s_api::{policy::authorization_policy as api, Event, ResourceExt};
use tracing::{instrument, warn};

pub async fn index(idx: SharedIndex, events: impl Stream<Item = Event<api::AuthorizationPolicy>>) {
    tokio::pin!(events);
    while let Some(ev) = events.next().await {
        match ev {
            Event::Applied(ap) => apply(&mut *idx.lock(), ap),
            Event::Deleted(ap) => delete(&mut *idx.lock(), ap),
            Event::Restarted(aps) => restart(&mut *idx.lock(), aps),
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
