use std::env;
use std::net::SocketAddr;
use std::path::PathBuf;
use std::str::FromStr;
use std::time::Duration;

use url::{Host, HostAndPort, Url};

// TODO:
//
// * Make struct fields private.

/// Tracks all configuration settings for the process.
#[derive(Clone, Debug)]
pub struct Config {
    /// Where to listen for connections that are initiated on the host.
    pub private_listener: Listener,

    /// Where to listen for connections initiated by external sources.
    pub public_listener: Listener,

    /// Where to listen for connectoins initiated by the control planey.
    pub control_listener: Listener,

    /// Where to forward externally received connections.
    pub private_forward: Option<Addr>,

    /// The maximum amount of time to wait for a connection to the public peer.
    pub public_connect_timeout: Option<Duration>,

    /// The maximum amount of time to wait for a connection to the private peer.
    pub private_connect_timeout: Option<Duration>,

    /// The path to "/etc/resolv.conf"
    pub resolv_conf_path: PathBuf,

    /// Where to talk to the control plane.
    pub control_host_and_port: HostAndPort,

    /// Event queue capacity.
    pub event_buffer_capacity: usize,

    /// Interval after which to flush metrics
    pub metrics_flush_interval: Duration,
}

/// Configuration settings for binding a listener.
///
/// TODO: Rename this to be more inline with the actual types.
#[derive(Clone, Debug)]
pub struct Listener {
    /// The address to which the listener should bind.
    pub addr: Addr,
}

/// A logical address. This abstracts over the various strategies for cross
/// process communication.
#[derive(Clone, Copy, Debug)]
pub struct Addr(SocketAddr);

/// Errors produced when loading a `Config` struct.
#[derive(Clone, Debug)]
pub enum Error {
    InvalidAddr,
    ControlPlaneConfigError(String, UrlError),
    NotANumber(String),
}

#[derive(Clone, Copy, Debug)]
pub enum UrlError {
    /// The URl is syntactically invalid according to general URL parsing rules.
    SyntaxError,

    /// The URL has a scheme that isn't supported.
    UnsupportedScheme,

    /// The URL is missing the host part.
    MissingHost,

    /// The URL is missing the port and there is no default port.
    MissingPort,

    /// The URL contains a path component that isn't "/", which isn't allowed.
    PathNotAllowed,

    /// The URL contains a fragment, which isn't allowed.
    FragmentNotAllowed,
}

// Environment variables to look at when loading the configuration
const ENV_EVENT_BUFFER_CAPACITY: &str = "CONDUIT_PROXY_EVENT_BUFFER_CAPACITY";
const ENV_METRICS_FLUSH_INTERVAL_SECS: &str = "CONDUIT_PROXY_METRICS_FLUSH_INTERVAL_SECS";
const ENV_PRIVATE_LISTENER: &str = "CONDUIT_PROXY_PRIVATE_LISTENER";
const ENV_PRIVATE_FORWARD: &str = "CONDUIT_PROXY_PRIVATE_FORWARD";
const ENV_PUBLIC_LISTENER: &str = "CONDUIT_PROXY_PUBLIC_LISTENER";
const ENV_CONTROL_LISTENER: &str = "CONDUIT_PROXY_CONTROL_LISTENER";
const ENV_PRIVATE_CONNECT_TIMEOUT: &str = "CONDUIT_PROXY_PRIVATE_CONNECT_TIMEOUT";
const ENV_PUBLIC_CONNECT_TIMEOUT: &str = "CONDUIT_PROXY_PUBLIC_CONNECT_TIMEOUT";

// the following are `pub` because they're used in the `ctx` module for populating `Process`.
pub const ENV_NODE_NAME: &str = "CONDUIT_PROXY_NODE_NAME";
pub const ENV_POD_NAME: &str = "CONDUIT_PROXY_POD_NAME";
pub const ENV_POD_NAMESPACE: &str = "CONDUIT_PROXY_POD_NAMESPACE";

const ENV_CONTROL_URL: &str = "CONDUIT_PROXY_CONTROL_URL";
const ENV_RESOLV_CONF: &str = "CONDUIT_RESOLV_CONF";

// Default values for various configuration fields
const DEFAULT_EVENT_BUFFER_CAPACITY: usize = 10_000; // FIXME
const DEFAULT_METRICS_FLUSH_INTERVAL_SECS: u64 = 10;
const DEFAULT_PRIVATE_LISTENER: &str = "tcp://127.0.0.1:4140";
const DEFAULT_PUBLIC_LISTENER: &str = "tcp://0.0.0.0:4143";
const DEFAULT_CONTROL_LISTENER: &str = "tcp://0.0.0.0:4190";
const DEFAULT_CONTROL_URL: &str = "tcp://proxy-api.conduit.svc.cluster.local:8086";
const DEFAULT_RESOLV_CONF: &str = "/etc/resolv.conf";

// ===== impl Config =====

impl Config {
    /// Load a `Config` by reading ENV variables.
    pub fn load_from_env() -> Result<Self, Error> {
        let event_buffer_capacity = match env::var(ENV_EVENT_BUFFER_CAPACITY).ok() {
            None => DEFAULT_EVENT_BUFFER_CAPACITY,
            Some(c) => match c.parse() {
                Ok(c) => c,
                Err(_) => return Err(Error::NotANumber(c)),
            },
        };

        let metrics_flush_interval = match env::var(ENV_METRICS_FLUSH_INTERVAL_SECS).ok() {
            None => Duration::from_secs(DEFAULT_METRICS_FLUSH_INTERVAL_SECS),
            Some(c) => match c.parse() {
                Ok(c) => Duration::from_secs(c),
                Err(_) => return Err(Error::NotANumber(c)),
            },
        };

        Ok(Config {
            private_listener: Listener {
                addr: Addr::from_env_or(ENV_PRIVATE_LISTENER, DEFAULT_PRIVATE_LISTENER)?,
            },
            public_listener: Listener {
                addr: Addr::from_env_or(ENV_PUBLIC_LISTENER, DEFAULT_PUBLIC_LISTENER)?,
            },
            control_listener: Listener {
                addr: Addr::from_env_or(ENV_CONTROL_LISTENER, DEFAULT_CONTROL_LISTENER)?,
            },
            private_forward: Addr::from_env_opt(ENV_PRIVATE_FORWARD)?,

            public_connect_timeout: env::var(ENV_PUBLIC_CONNECT_TIMEOUT)
                .ok()
                .and_then(|c| c.parse().ok())
                .map(Duration::from_millis),

            private_connect_timeout: env::var(ENV_PRIVATE_CONNECT_TIMEOUT)
                .ok()
                .and_then(|c| c.parse().ok())
                .map(Duration::from_millis),

            resolv_conf_path: env::var(ENV_RESOLV_CONF)
                .unwrap_or_else(|_| DEFAULT_RESOLV_CONF.into())
                .into(),

            control_host_and_port: control_host_and_port_from_env(
                ENV_CONTROL_URL,
                DEFAULT_CONTROL_URL,
            )?,
            event_buffer_capacity,
            metrics_flush_interval,
        })
    }
}

// ===== impl Addr =====

impl Addr {
    fn from_env_opt(key: &str) -> Result<Option<Addr>, Error> {
        match env::var(key) {
            Ok(a) => a.parse().map(Some),
            Err(_) => Ok(None),
        }
    }

    fn from_env_or(key: &str, default: &str) -> Result<Addr, Error> {
        let s = env::var(key).unwrap_or_else(|_| default.into());

        s.parse()
    }
}

impl FromStr for Addr {
    type Err = Error;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match Url::parse(s) {
            Err(_) => Err(Error::InvalidAddr),
            Ok(u) => match u.scheme() {
                "tcp" => match u.with_default_port(|_| Err(())) {
                    Ok(HostAndPort {
                        host: Host::Ipv4(ip),
                        port,
                    }) => Ok(Addr(SocketAddr::new(ip.into(), port))),
                    Ok(HostAndPort {
                        host: Host::Ipv6(ip),
                        port,
                    }) => Ok(Addr(SocketAddr::new(ip.into(), port))),
                    _ => Err(Error::InvalidAddr),
                },
                _ => Err(Error::InvalidAddr),
            },
        }
    }
}

impl From<Addr> for SocketAddr {
    fn from(addr: Addr) -> SocketAddr {
        addr.0
    }
}

fn control_host_and_port_from_env(key: &str, default: &str) -> Result<HostAndPort, Error> {
    let s = env::var(key).unwrap_or_else(|_| default.into());
    let url = Url::parse(&s).map_err(|_| {
        Error::ControlPlaneConfigError(s.clone(), UrlError::SyntaxError)
    })?;
    let host = url.host()
        .ok_or_else(|| {
            Error::ControlPlaneConfigError(s.clone(), UrlError::MissingHost)
        })?
        .to_owned();
    if url.scheme() != "tcp" {
        return Err(Error::ControlPlaneConfigError(
            s.clone(),
            UrlError::UnsupportedScheme,
        ));
    }
    let port = url.port().ok_or_else(|| {
        Error::ControlPlaneConfigError(s.clone(), UrlError::MissingPort)
    })?;
    if url.path() != "/" {
        return Err(Error::ControlPlaneConfigError(
            s.clone(),
            UrlError::PathNotAllowed,
        ));
    }
    if url.fragment().is_some() {
        return Err(Error::ControlPlaneConfigError(
            s.clone(),
            UrlError::FragmentNotAllowed,
        ));
    }
    Ok(HostAndPort {
        host,
        port,
    })
}
