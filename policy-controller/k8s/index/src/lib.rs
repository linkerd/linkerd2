//! The policy controller serves discovery requests from inbound proxies, indicating how the proxy
//! should admit connections into a Pod. It watches the following cluster resources:
//!
//! - A `Namespace` may be annotated with a default-allow policy that applies to all pods in the
//!   namespace (unless they are annotated with a default policy).
//! - Each `Pod` enumerate its ports. We maintain an index of each pod's ports, linked to `Server`
//!   objects.
//! - Each `Server` selects over pods in the same namespace.
//! - Each `ServerAuthorization` selects over `Server` instances in the same namespace.  When a
//!   `ServerAuthorization` is updated, we find all of the `Server` instances it selects and update
//!   their authorizations and publishes these updates on the server's broadcast channel.
//!
//! ```text
//! [ Pod ] -> [ Port ] <- [ Server ] <- [ ServerAuthorization ]
//! ```
//!
//! Lookups against this index are are initiated for a single pod & port.
//!
//! The Pod, Server, and ServerAuthorization indices are all scoped within a namespace index, as
//! these resources cannot reference resources in other namespaces. This scoping helps to narrow the
//! search space when processing updates and linking resources.

#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

pub mod authorization_policy;
mod cluster_info;
mod defaults;
pub mod http_route;
mod index;
mod meshtls_authentication;
mod network_authentication;
pub mod outbound_index;
mod pod;
mod server;
mod server_authorization;

#[cfg(test)]
mod tests;

pub use self::{
    cluster_info::ClusterInfo,
    defaults::DefaultPolicy,
    index::{Index, SharedIndex},
    pod::{parse_portset, PortSet},
};
