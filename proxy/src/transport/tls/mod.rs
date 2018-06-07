// These crates are only used within the `tls` module.
extern crate ring;
extern crate rustls;
extern crate tokio_rustls;
extern crate untrusted;
extern crate webpki;

mod config;
mod cert_resolver;
mod connection;
mod dns_name;
mod identity;

pub use self::{
    config::{CommonSettings, CommonConfig, Error, ServerConfig, ServerConfigWatch},
    connection::Connection,
    dns_name::{DnsName, InvalidDnsName},
    identity::Identity,
};
