//! Developer tasks for the linkerd2 workspace, run as `cargo xtask <command>`.

#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]
#![warn(missing_docs, clippy::missing_docs_in_private_items)]

mod error;
mod jobs;
mod kube;

use crate::kube::Cluster;
use clap::{Parser, Subcommand};
use error::XtaskError;
use jobs::validator::{PolicyValidatorDenial, PolicyValidatorWaitEndpoint};

/// Developer tasks for the linkerd2 workspace.
#[derive(Debug, Parser)]
#[command(name = "cargo xtask", bin_name = "cargo xtask")]
struct Cli {
    /// Task to run.
    #[command(subcommand)]
    command: Command,
}

/// A task named on the command line.
#[derive(Debug, PartialEq, Eq, Subcommand)]
enum Command {
    /// Wait until the policy validator webhook serves admission requests.
    PolicyValidatorReady,
}

impl Command {
    /// Runs the task to completion.
    async fn run(self) -> Result<(), XtaskError> {
        match self {
            Self::PolicyValidatorReady => {
                let client = Cluster::connect().await?;
                PolicyValidatorWaitEndpoint::new(client.clone())
                    .run()
                    .await?;
                PolicyValidatorDenial::new(client).run().await?;
                Ok(())
            }
        }
    }
}

/// Runs the task named on the command line.
///
/// clap prints usage and exits 2 on a malformed command line; a failed task
/// returns its error, which prints `Error: ...` and exits 1.
#[tokio::main(flavor = "current_thread")]
async fn main() -> Result<(), XtaskError> {
    let Cli { command } = Cli::parse();
    command.run().await
}
