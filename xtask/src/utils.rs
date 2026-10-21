//! Diagnostics shared by the cluster-facing tasks.

use k8s_openapi::api::{apps::v1::Deployment, core::v1::Pod};
use kube::{api::ListParams, Api, ResourceExt};
use std::fmt::Write;

/// Prints the state of a namespace to stderr when a task fails.
pub struct NamespaceReport;

impl NamespaceReport {
    /// Prints every Deployment and Pod in `namespace` to stderr.
    ///
    /// Listing failures are printed in place of the section, since the report
    /// only adds context to an error the caller returns either way.
    pub async fn report_namespace(client: &kube::Client, namespace: &str) {
        let params = ListParams::default();

        // Report every Deployment, not only the ones the caller waits on, so a
        // stuck rollout of any workload in the namespace is visible.
        eprintln!("Deployments in namespace {namespace}:");
        match Api::<Deployment>::namespaced(client.clone(), namespace)
            .list(&params)
            .await
        {
            Ok(list) => list
                .items
                .iter()
                .for_each(|deployment| eprint!("{}", Self::format_deployment(deployment))),
            Err(error) => eprintln!("  failed to list Deployments: {error}"),
        }

        // Report every Pod so container waiting and termination reasons explain
        // why a Deployment has no available replicas.
        eprintln!("Pods in namespace {namespace}:");
        match Api::<Pod>::namespaced(client.clone(), namespace)
            .list(&params)
            .await
        {
            Ok(list) => list
                .items
                .iter()
                .for_each(|pod| eprint!("{}", Self::format_pod(pod))),
            Err(error) => eprintln!("  failed to list Pods: {error}"),
        }
    }

    /// Formats `deployment` as its name, replica counts and conditions, one per line.
    fn format_deployment(deployment: &Deployment) -> String {
        let desired = deployment
            .spec
            .as_ref()
            .and_then(|spec| spec.replicas)
            .unwrap_or_default();
        let status = deployment.status.as_ref();
        let ready = status
            .and_then(|status| status.ready_replicas)
            .unwrap_or_default();
        let available = status
            .and_then(|status| status.available_replicas)
            .unwrap_or_default();

        let mut out = format!(
            "  {}: replicas desired={desired} ready={ready} available={available}\n",
            deployment.name_any()
        );

        // List each condition with its message, since the message says why a
        // rollout is not progressing.
        for condition in status
            .and_then(|status| status.conditions.as_deref())
            .unwrap_or_default()
        {
            let _ = writeln!(
                out,
                "    condition {}={}: {}",
                condition.type_,
                condition.status,
                condition.message.as_deref().unwrap_or("")
            );
        }
        out
    }

    /// Formats `pod` as its name, phase and each container's readiness, one per line.
    fn format_pod(pod: &Pod) -> String {
        let status = pod.status.as_ref();
        let phase = status
            .and_then(|status| status.phase.as_deref())
            .unwrap_or("Unknown");
        let mut out = format!("  {}: phase={phase}\n", pod.name_any());

        // Show the waiting or terminated reason, which names failures such as
        // image pulls or crash loops that keep a container from becoming ready.
        for container in status
            .and_then(|status| status.container_statuses.as_deref())
            .unwrap_or_default()
        {
            let state = container.state.as_ref();
            let reason = state
                .and_then(|state| state.waiting.as_ref())
                .map(|waiting| ("waiting", waiting.reason.as_deref()))
                .or_else(|| {
                    state
                        .and_then(|state| state.terminated.as_ref())
                        .map(|terminated| ("terminated", terminated.reason.as_deref()))
                });
            let _ = match reason {
                Some((kind, reason)) => writeln!(
                    out,
                    "    container {}: ready={} {kind}={}",
                    container.name,
                    container.ready,
                    reason.unwrap_or("")
                ),
                None => writeln!(
                    out,
                    "    container {}: ready={}",
                    container.name, container.ready
                ),
            };
        }
        out
    }
}
