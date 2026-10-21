//! Errors returned by the developer tasks.

use std::fmt;

/// Failure of a developer task.
pub enum XtaskError {
    /// No Kubernetes client could be built; names what failed and its inputs.
    Client(String),
    /// The policy validator webhook did not become ready; carries the full explanation.
    PolicyValidator(String),
    /// Control plane Deployments did not become available; names those that are not.
    ///
    /// Empty when no Deployment matched, so none could become available.
    ControlPlaneReady(Vec<String>),
}

impl fmt::Display for XtaskError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Client(message) | Self::PolicyValidator(message) => f.write_str(message),
            Self::ControlPlaneReady(names) if names.is_empty() => {
                f.write_str("no control plane Deployments found")
            }
            Self::ControlPlaneReady(names) => write!(
                f,
                "control plane Deployments not Available: {}",
                names.join(", ")
            ),
        }
    }
}

/// Delegates to `Display` so the `Error: ...` line printed when `main` fails stays readable.
impl fmt::Debug for XtaskError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        fmt::Display::fmt(self, f)
    }
}

impl std::error::Error for XtaskError {}
