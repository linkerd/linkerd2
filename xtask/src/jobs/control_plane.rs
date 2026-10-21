//! Readiness check for the Linkerd control plane Deployments.
//!
//! `just _linkerd-ready` runs this after `linkerd install` and before any test
//! touches the cluster, so tests start against a control plane whose
//! Deployments all report `Available`.

use crate::{error::XtaskError, utils::NamespaceReport};
use futures::{future, StreamExt};
use k8s_openapi::api::apps::v1::Deployment;
use kube::{
    runtime::{reflector, watcher, WatchStreamExt},
    Api, ResourceExt,
};
use std::{pin::pin, sync::Arc, time::Duration};
use tokio::time;

/// Namespace of the Linkerd control plane.
const NAMESPACE: &str = "linkerd";
/// Label carried by every control plane Deployment.
const SELECTOR: &str = "linkerd.io/control-plane-component";
/// Upper bound for all Deployments to become available.
const TIMEOUT: Duration = Duration::from_secs(120);

/// Waits for every control plane Deployment to be available.
pub struct ControlPlaneReady {
    /// Client for the cluster running the Linkerd control plane.
    client: kube::Client,
}

impl ControlPlaneReady {
    /// Creates a checker that talks to the cluster through `client`.
    pub fn new(client: kube::Client) -> Self {
        Self { client }
    }

    /// Waits until every Deployment matching `SELECTOR` has `Available=True`.
    ///
    /// Readiness is a property of the whole set, so a reflector store holds all
    /// matching Deployments and is checked only once the initial list is in it.
    pub async fn run(&self) -> Result<(), XtaskError> {
        let api = Api::<Deployment>::namespaced(self.client.clone(), NAMESPACE);
        let config = watcher::Config::default().labels(SELECTOR);
        let (store, writer) = reflector::store();

        // Resolve on the first event after which the store is current and every
        // Deployment in it is available.
        let ready = reflector(writer, watcher(api, config).default_backoff()).filter(|event| {
            future::ready(
                Self::store_is_current(event) && Self::all_deployments_available(&store.state()),
            )
        });
        let mut ready = pin!(ready);

        if time::timeout(TIMEOUT, ready.next()).await.is_err() {
            // Tests would run against a control plane that cannot serve them.
            // Report the namespace state and name what is missing so the failure points at it.
            NamespaceReport::report_namespace(&self.client, NAMESPACE).await;
            return Err(XtaskError::ControlPlaneReady(
                Self::list_unavailable_deployments(&store.state()),
            ));
        }

        Ok(())
    }

    /// Checks whether the store reflects the cluster after `event`.
    ///
    /// Every variant is listed so a new `watcher::Event` variant fails to compile.
    fn store_is_current(event: &Result<watcher::Event<Deployment>, watcher::Error>) -> bool {
        match event {
            // The reflector swaps the buffered list into the store at `InitDone`.
            // Later events update that list in place, so the store is current.
            Ok(watcher::Event::InitDone | watcher::Event::Apply(_) | watcher::Event::Delete(_)) => {
                true
            }
            // During a list or relist the store still holds the previous list.
            // That list is empty on the first list and stale after a relist.
            Ok(watcher::Event::Init | watcher::Event::InitApply(_)) => false,
            // Errors are skipped because the backoff stream retries them.
            Err(_) => false,
        }
    }

    /// Checks whether `deployments` is non-empty and every one is available.
    ///
    /// An empty set is not ready, since the install may not have created the
    /// Deployments yet.
    fn all_deployments_available(deployments: &[Arc<Deployment>]) -> bool {
        !deployments.is_empty() && Self::list_unavailable_deployments(deployments).is_empty()
    }

    /// Returns the names of the Deployments in `deployments` that are not available.
    fn list_unavailable_deployments(deployments: &[Arc<Deployment>]) -> Vec<String> {
        deployments
            .iter()
            .filter(|deployment| !Self::is_deployment_available(deployment))
            .map(|deployment| deployment.name_any())
            .collect()
    }

    /// Checks whether `deployment` has condition `Available` with status `True`.
    fn is_deployment_available(deployment: &Deployment) -> bool {
        deployment
            .status
            .as_ref()
            .and_then(|status| status.conditions.as_ref())
            .is_some_and(|conditions| {
                conditions
                    .iter()
                    .any(|c| c.type_ == "Available" && c.status == "True")
            })
    }
}
