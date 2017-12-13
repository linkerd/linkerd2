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
    InvalidEnvVar {
        name: String,
        value: Option<String>,
        parse_error: ParseError,
    },
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

// Environment variables to look at when loading the configuration
const ENV_EVENT_BUFFER_CAPACITY: &str = "CONDUIT_PROXY_EVENT_BUFFER_CAPACITY";
const ENV_METRICS_FLUSH_INTERVAL_SECS: &str = "CONDUIT_PROXY_METRICS_FLUSH_INTERVAL_SECS";
const ENV_REPORT_TIMEOUT_SECS: &str = "CONDUIT_PROXY_REPORT_TIMEOUT_SECS";
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
const DEFAULT_REPORT_TIMEOUT_SECS: u64 = 10; // TODO: is this a reasonable default?
const DEFAULT_PRIVATE_LISTENER: &str = "tcp://127.0.0.1:4140";
const DEFAULT_PUBLIC_LISTENER: &str = "tcp://0.0.0.0:4143";
const DEFAULT_CONTROL_LISTENER: &str = "tcp://0.0.0.0:4190";
const DEFAULT_CONTROL_URL: &str = "tcp://proxy-api.conduit.svc.cluster.local:8086";
const DEFAULT_RESOLV_CONF: &str = "/etc/resolv.conf";

// ===== impl Config =====

impl Config {
    /// Load a `Config` by reading ENV variables.
    pub fn load_from_env() -> Result<Self, Error> {
        let event_buffer_capacity = env_var_parse(ENV_EVENT_BUFFER_CAPACITY, parse_number)?
            .unwrap_or(DEFAULT_EVENT_BUFFER_CAPACITY);

        let metrics_flush_interval = Duration::from_secs(
            env_var_parse(ENV_METRICS_FLUSH_INTERVAL_SECS, parse_number)?
                .unwrap_or(DEFAULT_METRICS_FLUSH_INTERVAL_SECS)
        );

        let report_timeout = Duration::from_secs(
            env_var_parse(ENV_REPORT_TIMEOUT_SECS, parse_number)?
                .unwrap_or(DEFAULT_REPORT_TIMEOUT_SECS)
        );

        Ok(Config {
            private_listener: Listener {
                addr: env_var_parse(ENV_PRIVATE_LISTENER, str::parse)?
                    .unwrap_or_else(|| Addr::from_str(DEFAULT_PRIVATE_LISTENER).unwrap()),
            },
            public_listener: Listener {
                addr: env_var_parse(ENV_PUBLIC_LISTENER, str::parse)?
                    .unwrap_or_else(|| Addr::from_str(DEFAULT_PUBLIC_LISTENER).unwrap()),
            },
            control_listener: Listener {
                addr: env_var_parse(ENV_CONTROL_LISTENER, str::parse)?
                    .unwrap_or_else(|| Addr::from_str(DEFAULT_CONTROL_LISTENER).unwrap()),
            },
            private_forward: env_var_parse(ENV_PRIVATE_FORWARD, str::parse)?,

            public_connect_timeout: env_var_parse(ENV_PUBLIC_CONNECT_TIMEOUT, parse_number)?
                .map(Duration::from_millis),

            private_connect_timeout: env_var_parse(ENV_PRIVATE_CONNECT_TIMEOUT, parse_number)?
                .map(Duration::from_millis),

            resolv_conf_path: env_var(ENV_RESOLV_CONF)?
                .unwrap_or(DEFAULT_RESOLV_CONF.into())
                .into(),

            control_host_and_port: env_var_parse(ENV_CONTROL_URL, parse_url)?
                .unwrap_or_else(|| parse_url(DEFAULT_CONTROL_URL).unwrap()),

            event_buffer_capacity,
            metrics_flush_interval,
            report_timeout,
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

fn env_var(name: &str) -> Result<Option<String>, Error> {
    match env::var(name) {
        Ok(value) => Ok(Some(value)),
        Err(env::VarError::NotPresent) => Ok(None),
        Err(env::VarError::NotUnicode(_)) => Err(Error::InvalidEnvVar {
            name: name.to_owned(),
            value: None,
            parse_error: ParseError::NotUnicode,
        }),
    }
}

fn env_var_parse<T, Parse>(name: &str, parse: Parse) -> Result<Option<T>, Error>
    where Parse: FnOnce(&str) -> Result<T, ParseError> {
    match env_var(name)? {
        Some(ref s) => {
            let r = parse(s).map_err(|parse_error| {
                Error::InvalidEnvVar {
                    name: name.to_owned(),
                    value: Some(s.to_owned()),
                    parse_error,
                }
            })?;
            Ok(Some(r))
        },
        None => Ok(None),
    }
}
