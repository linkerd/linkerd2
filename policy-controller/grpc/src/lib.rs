#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

mod routes;

pub mod inbound;
pub mod metrics;
pub mod outbound;
pub mod workload;
