//! Kubernetes client construction shared by the cluster-facing tasks.

use crate::error::XtaskError;

/// Builds the Kubernetes client the tasks talk to.
pub struct Cluster;

impl Cluster {
    /// Environment variable naming the kubeconfig context; the justfile exports it.
    const CONTEXT_VAR: &'static str = "POLICY_TEST_CONTEXT";

    /// Connects to the cluster that the policy tests target.
    ///
    /// Honors `POLICY_TEST_CONTEXT` the same way the policy tests do, so the
    /// readiness check and the tests always address the same cluster.
    pub async fn connect() -> Result<::kube::Client, XtaskError> {
        match std::env::var(Self::CONTEXT_VAR).ok() {
            None => ::kube::Client::try_default().await.map_err(|error| {
                XtaskError::Client(format!(
                    "failed to infer in-cluster or default kubeconfig: {error}"
                ))
            }),
            Some(context) => {
                let opts = ::kube::config::KubeConfigOptions {
                    context: Some(context.clone()),
                    cluster: None,
                    user: None,
                };
                let config = ::kube::Config::from_kubeconfig(&opts)
                    .await
                    .map_err(|error| {
                        XtaskError::Client(format!(
                            "failed to load kubeconfig context {context}: {error}"
                        ))
                    })?;
                ::kube::Client::try_from(config).map_err(|error| {
                    XtaskError::Client(format!(
                        "failed to build client from kubeconfig context {context}: {error}"
                    ))
                })
            }
        }
    }
}
