use std::collections::HashMap;
use std::env;
use std::net::SocketAddr;
use std::path::PathBuf;
use std::str::FromStr;
use std::time::Duration;

use url::{Host, HostAndPort, Url};

use convert::TryFrom;

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

    /// Interval after which to flush metrics.
    pub metrics_flush_interval: Duration,

    /// Timeout after which to cancel telemetry reports.
    pub report_timeout: Duration,
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
    InvalidEnvVar
}

#[derive(Clone, Debug)]
pub enum ParseError {
    NotANumber,
    HostIsNotAnIpAddress,
    NotUnicode,
    UrlError(UrlError),
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

/// The strings used to build a configuration.
pub trait Strings {
    /// Retrieves the value for the key `key`.
    ///
    /// `key` must be one of the `ENV_` values below.
    fn get(&self, key: &str) -> Result<Option<String>, Error>;
}

/// An implementation of `Strings` that reads the values from environment variables.
pub struct Env;

pub struct TestEnv {
    values: HashMap<&'static str, String>
}

// Environment variables to look at when loading the configuration
const ENV_EVENT_BUFFER_CAPACITY: &str = "CONDUIT_PROXY_EVENT_BUFFER_CAPACITY";
pub const ENV_METRICS_FLUSH_INTERVAL_SECS: &str = "CONDUIT_PROXY_METRICS_FLUSH_INTERVAL_SECS";
const ENV_REPORT_TIMEOUT_SECS: &str = "CONDUIT_PROXY_REPORT_TIMEOUT_SECS";
pub const ENV_PRIVATE_LISTENER: &str = "CONDUIT_PROXY_PRIVATE_LISTENER";
pub const ENV_PRIVATE_FORWARD: &str = "CONDUIT_PROXY_PRIVATE_FORWARD";
pub const ENV_PUBLIC_LISTENER: &str = "CONDUIT_PROXY_PUBLIC_LISTENER";
pub const ENV_CONTROL_LISTENER: &str = "CONDUIT_PROXY_CONTROL_LISTENER";
const ENV_PRIVATE_CONNECT_TIMEOUT: &str = "CONDUIT_PROXY_PRIVATE_CONNECT_TIMEOUT";
const ENV_PUBLIC_CONNECT_TIMEOUT: &str = "CONDUIT_PROXY_PUBLIC_CONNECT_TIMEOUT";

// the following are `pub` because they're used in the `ctx` module for populating `Process`.
pub const ENV_NODE_NAME: &str = "CONDUIT_PROXY_NODE_NAME";
pub const ENV_POD_NAME: &str = "CONDUIT_PROXY_POD_NAME";
pub const ENV_POD_NAMESPACE: &str = "CONDUIT_PROXY_POD_NAMESPACE";

pub const ENV_CONTROL_URL: &str = "CONDUIT_PROXY_CONTROL_URL";
const ENV_RESOLV_CONF: &str = "CONDUIT_RESOLV_CONF";

// Default values for various configuration fields
const DEFAULT_EVENT_BUFFER_CAPACITY: usize = 10_000; // FIXME
const DEFAULT_METRICS_FLUSH_INTERVAL_SECS: u64 = 10;
const DEFAULT_REPORT_TIMEOUT_SECS: u64 = 10; // TODO: is this a reasonable default?
const DEFAULT_PRIVATE_LISTENER: &str = "tcp://127.0.0.1:4140";
const DEFAULT_PUBLIC_LISTENER: &str = "tcp://0.0.0.0:4143";
const DEFAULT_CONTROL_LISTENER: &str = "tcp://0.0.0.0:4190";
const DEFAULT_CONTROL_URL: &str = "tcp://proxy-api.conduit.svc.cluster.local:8086";
const DEFAULT_RESOLV_CONF: &str = "/etc/resolv.conf";

// ===== impl Config =====

impl<'a> TryFrom<&'a Strings> for Config {
    type Err = Error;
    /// Load a `Config` by reading ENV variables.
    fn try_from(strings: &Strings) -> Result<Self, Self::Err> {
        // Parse all the environment variables. `env_var` and `env_var_parse`
        // will log any errors so defer returning any errors until all of them
        // have been parsed.
        let private_listener_addr = parse(strings, ENV_PRIVATE_LISTENER, str::parse);
        let public_listener_addr = parse(strings, ENV_PUBLIC_LISTENER, str::parse);
        let control_listener_addr = parse(strings, ENV_CONTROL_LISTENER, str::parse);
        let private_forward = parse(strings, ENV_PRIVATE_FORWARD, str::parse);
        let public_connect_timeout = parse(strings, ENV_PUBLIC_CONNECT_TIMEOUT, parse_number);
        let private_connect_timeout = parse(strings, ENV_PRIVATE_CONNECT_TIMEOUT, parse_number);
        let resolv_conf_path = strings.get(ENV_RESOLV_CONF);
        let control_host_and_port = parse(strings, ENV_CONTROL_URL, parse_url);
        let event_buffer_capacity = parse(strings, ENV_EVENT_BUFFER_CAPACITY, parse_number);
        let metrics_flush_interval_secs =
            parse(strings, ENV_METRICS_FLUSH_INTERVAL_SECS, parse_number);
        let report_timeout = parse(strings, ENV_REPORT_TIMEOUT_SECS, parse_number);

        Ok(Config {
            private_listener: Listener {
                addr: private_listener_addr?
                    .unwrap_or_else(|| Addr::from_str(DEFAULT_PRIVATE_LISTENER).unwrap()),
            },
            public_listener: Listener {
                addr: public_listener_addr?
                    .unwrap_or_else(|| Addr::from_str(DEFAULT_PUBLIC_LISTENER).unwrap()),
            },
            control_listener: Listener {
                addr: control_listener_addr?
                    .unwrap_or_else(|| Addr::from_str(DEFAULT_CONTROL_LISTENER).unwrap()),
            },
            private_forward: private_forward?,
            public_connect_timeout: public_connect_timeout?.map(Duration::from_millis),
            private_connect_timeout: private_connect_timeout?.map(Duration::from_millis),
            resolv_conf_path: resolv_conf_path?
                .unwrap_or(DEFAULT_RESOLV_CONF.into())
                .into(),
            control_host_and_port: control_host_and_port?
                .unwrap_or_else(|| parse_url(DEFAULT_CONTROL_URL).unwrap()),

            event_buffer_capacity: event_buffer_capacity?.unwrap_or(DEFAULT_EVENT_BUFFER_CAPACITY),
            metrics_flush_interval:
                Duration::from_secs(metrics_flush_interval_secs?
                                        .unwrap_or(DEFAULT_METRICS_FLUSH_INTERVAL_SECS)),
            report_timeout:
                Duration::from_secs(report_timeout?.unwrap_or(DEFAULT_REPORT_TIMEOUT_SECS)),
        })
    }
}

// ===== impl Addr =====

impl FromStr for Addr {
    type Err = ParseError;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match parse_url(s)? {
            HostAndPort {
                host: Host::Ipv4(ip),
                port,
            } => Ok(Addr(SocketAddr::new(ip.into(), port))),
            HostAndPort {
                host: Host::Ipv6(ip),
                port,
            } => Ok(Addr(SocketAddr::new(ip.into(), port))),
            HostAndPort {
                host: Host::Domain(_),
                ..
            } => Err(ParseError::HostIsNotAnIpAddress),
        }
    }
}

impl From<Addr> for SocketAddr {
    fn from(addr: Addr) -> SocketAddr {
        addr.0
    }
}

// ===== impl Env =====

impl Strings for Env {
    fn get(&self, key: &str) -> Result<Option<String>, Error> {
        match env::var(key) {
            Ok(value) => Ok(Some(value)),
            Err(env::VarError::NotPresent) => Ok(None),
            Err(env::VarError::NotUnicode(_)) => {
                error!("{} is not encoded in Unicode", key);
                Err(Error::InvalidEnvVar)
            }
        }
    }
}

// ===== impl TestEnv =====

impl TestEnv {
    pub fn new() -> Self {
        Self {
            values: Default::default(),
        }
    }

    pub fn put(&mut self, key: &'static str, value: String) {
        self.values.insert(key, value);
    }
}

impl Strings for TestEnv {
    fn get(&self, key: &str) -> Result<Option<String>, Error> {
        Ok(self.values.get(key).cloned())
    }
}

// ===== Parsing =====

fn parse_number<T>(s: &str) -> Result<T, ParseError> where T: FromStr {
    s.parse().map_err(|_| ParseError::NotANumber)
}

fn parse_url(s: &str) -> Result<HostAndPort, ParseError> {
    let url = Url::parse(&s).map_err(|_| ParseError::UrlError(UrlError::SyntaxError))?;
    let host = url.host()
        .ok_or_else(|| ParseError::UrlError(UrlError::MissingHost))?
        .to_owned();
    if url.scheme() != "tcp" {
        return Err(ParseError::UrlError(UrlError::UnsupportedScheme));
    }
    let port = url.port().ok_or_else(|| ParseError::UrlError(UrlError::MissingPort))?;
    if url.path() != "/" {
        return Err(ParseError::UrlError(UrlError::PathNotAllowed));
    }
    if url.fragment().is_some() {
        return Err(ParseError::UrlError(UrlError::FragmentNotAllowed));
    }
    Ok(HostAndPort {
        host,
        port,
    })
}

fn parse<T, Parse>(strings: &Strings, name: &str, parse: Parse) -> Result<Option<T>, Error>
    where Parse: FnOnce(&str) -> Result<T, ParseError> {
    match strings.get(name)? {
        Some(ref s) => {
            let r = parse(s).map_err(|parse_error| {
                error!("{} is not valid: {:?}", name, parse_error);
                Error::InvalidEnvVar
            })?;
            Ok(Some(r))
        },
        None => Ok(None),
    }
}
