#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

mod identity_match;
pub mod inbound;
mod network_match;
pub mod outbound;
pub mod routes;

pub use self::{identity_match::IdentityMatch, network_match::NetworkMatch};
pub use ipnet::{IpNet, Ipv4Net, Ipv6Net};

pub const POLICY_CONTROLLER_NAME: &str = "linkerd.io/policy-controller";
