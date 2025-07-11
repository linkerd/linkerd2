#![deny(warnings, rust_2018_idioms)]
#![allow(clippy::result_large_err)]
#![forbid(unsafe_code)]

mod routes;

pub mod inbound;
pub mod metrics;
pub mod outbound;
pub mod workload;
