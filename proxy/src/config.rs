use std::collections::HashMap;
use std::env;
use std::net::SocketAddr;
use std::path::PathBuf;
use std::str::FromStr;
use std::time::Duration;

use http;

use transport::{Host, HostAndPort, HostAndPortError};
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

    /// Where to serve Prometheus metrics.
    pub metrics_listener: Listener,

    /// Where to forward externally received connections.
    pub private_forward: Option<Addr>,

    /// The maximum amount of time to wait for a connection to the public peer.
    pub public_connect_timeout: Duration,

    /// The maximum amount of time to wait for a connection to the private peer.
    pub private_connect_timeout: Duration,

    /// The path to "/etc/resolv.conf"
    pub resolv_conf_path: PathBuf,

    /// Where to talk to the control plane.
    pub control_host_and_port: HostAndPort,

    /// Event queue capacity.
    pub event_buffer_capacity: usize,

    /// Timeout after which to cancel telemetry reports.
    pub report_timeout: Duration,

    /// Timeout after which to cancel binding a request.
    pub bind_timeout: Duration,

    pub pod_name: Option<String>,
    pub pod_namespace: String,
    pub node_name: Option<String>,
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
    EnvironmentUnsupported,
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

    /// The URL is missing the authority part.
    MissingAuthority,

    /// The URL is missing the authority part.
    AuthorityError(HostAndPortError),

    /// The URL contains a path component that isn't "/", which isn't allowed.
    PathNotAllowed,
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
const ENV_REPORT_TIMEOUT_SECS: &str = "CONDUIT_PROXY_REPORT_TIMEOUT_SECS";
pub const ENV_PRIVATE_LISTENER: &str = "CONDUIT_PROXY_PRIVATE_LISTENER";
pub const ENV_PRIVATE_FORWARD: &str = "CONDUIT_PROXY_PRIVATE_FORWARD";
pub const ENV_PUBLIC_LISTENER: &str = "CONDUIT_PROXY_PUBLIC_LISTENER";
pub const ENV_CONTROL_LISTENER: &str = "CONDUIT_PROXY_CONTROL_LISTENER";
pub const ENV_METRICS_LISTENER: &str = "CONDUIT_PROXY_METRICS_LISTENER";
const ENV_PRIVATE_CONNECT_TIMEOUT: &str = "CONDUIT_PROXY_PRIVATE_CONNECT_TIMEOUT";
const ENV_PUBLIC_CONNECT_TIMEOUT: &str = "CONDUIT_PROXY_PUBLIC_CONNECT_TIMEOUT";
pub const ENV_BIND_TIMEOUT: &str = "CONDUIT_PROXY_BIND_TIMEOUT";

const ENV_NODE_NAME: &str = "CONDUIT_PROXY_NODE_NAME";
const ENV_POD_NAME: &str = "CONDUIT_PROXY_POD_NAME";
pub const ENV_POD_NAMESPACE: &str = "CONDUIT_PROXY_POD_NAMESPACE";

pub const ENV_CONTROL_URL: &str = "CONDUIT_PROXY_CONTROL_URL";
const ENV_RESOLV_CONF: &str = "CONDUIT_RESOLV_CONF";

// Default values for various configuration fields
const DEFAULT_EVENT_BUFFER_CAPACITY: usize = 10_000; // FIXME
const DEFAULT_REPORT_TIMEOUT_SECS: u64 = 10; // TODO: is this a reasonable default?
const DEFAULT_PRIVATE_LISTENER: &str = "tcp://127.0.0.1:4140";
const DEFAULT_PUBLIC_LISTENER: &str = "tcp://0.0.0.0:4143";
const DEFAULT_CONTROL_LISTENER: &str = "tcp://0.0.0.0:4190";
const DEFAULT_METRICS_LISTENER: &str = "tcp://127.0.0.1:4191";
const DEFAULT_PRIVATE_CONNECT_TIMEOUT_MS: u64 = 20;
const DEFAULT_PUBLIC_CONNECT_TIMEOUT_MS: u64 = 300;
const DEFAULT_BIND_TIMEOUT_MS: u64 = 10_000; // ten seconds, as in Linkerd.
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
        let metrics_listener_addr = parse(strings, ENV_METRICS_LISTENER, str::parse);
        let private_forward = parse(strings, ENV_PRIVATE_FORWARD, str::parse);
        let public_connect_timeout = parse(strings, ENV_PUBLIC_CONNECT_TIMEOUT, parse_number);
        let private_connect_timeout = parse(strings, ENV_PRIVATE_CONNECT_TIMEOUT, parse_number);
        let bind_timeout = parse(strings, ENV_BIND_TIMEOUT, parse_number);
        let resolv_conf_path = strings.get(ENV_RESOLV_CONF);
        let event_buffer_capacity = parse(strings, ENV_EVENT_BUFFER_CAPACITY, parse_number);
        let report_timeout = parse(strings, ENV_REPORT_TIMEOUT_SECS, parse_number);
        let pod_name = strings.get(ENV_POD_NAME);
        let pod_namespace = strings.get(ENV_POD_NAMESPACE).and_then(|maybe_value| {
            // There cannot be a default pod namespace, and the pod namespace is required.
            maybe_value.ok_or_else(|| {
                error!("{} is not set", ENV_POD_NAMESPACE);
                Error::InvalidEnvVar
            })
        });
        let node_name = strings.get(ENV_NODE_NAME);

        // There is no default controller URL because a default would make it
        // too easy to connect to the wrong controller, which would be dangerous.
        let control_host_and_port = match parse(strings, ENV_CONTROL_URL, parse_url) {
            Ok(Some(x)) => Ok(x),
            Ok(None) => {
                error!("{} is not set", ENV_CONTROL_URL);
                Err(Error::InvalidEnvVar)
            },
            Err(e) => Err(e),
        };

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
            metrics_listener: Listener {
                addr: metrics_listener_addr?
                    .unwrap_or_else(|| Addr::from_str(DEFAULT_METRICS_LISTENER).unwrap()),
            },
            private_forward: private_forward?,
            public_connect_timeout: Duration::from_millis(
                public_connect_timeout?
                    .unwrap_or(DEFAULT_PUBLIC_CONNECT_TIMEOUT_MS)
            ),
            private_connect_timeout:
                Duration::from_millis(private_connect_timeout?
                                          .unwrap_or(DEFAULT_PRIVATE_CONNECT_TIMEOUT_MS)),
            resolv_conf_path: resolv_conf_path?
                .unwrap_or(DEFAULT_RESOLV_CONF.into())
                .into(),
            control_host_and_port: control_host_and_port?,

            event_buffer_capacity: event_buffer_capacity?.unwrap_or(DEFAULT_EVENT_BUFFER_CAPACITY),
            report_timeout:
                Duration::from_secs(report_timeout?.unwrap_or(DEFAULT_REPORT_TIMEOUT_SECS)),
            bind_timeout:
                Duration::from_millis(bind_timeout?.unwrap_or(DEFAULT_BIND_TIMEOUT_MS)),
            pod_name: pod_name?,
            pod_namespace: pod_namespace?,
            node_name: node_name?,
        })
    }
}

impl Config {
    pub fn default_destination_namespace(&self) -> &str {
        &self.pod_namespace
    }
}

// ===== impl Addr =====

impl FromStr for Addr {
    type Err = ParseError;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        let a = parse_url(s)?;
        if let Host::Ip(ip) = a.host {
            return Ok(Addr(SocketAddr::from((ip, a.port))));
        }
        Err(ParseError::HostIsNotAnIpAddress)
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
    let url = s.parse::<http::Uri>().map_err(|_| ParseError::UrlError(UrlError::SyntaxError))?;
    if url.scheme_part().map(|s| s.as_str()) != Some("tcp") {
        return Err(ParseError::UrlError(UrlError::UnsupportedScheme));
    }
    let authority = url.authority_part()
        .ok_or_else(|| ParseError::UrlError(UrlError::MissingAuthority))?;

    if url.path() != "/" {
        return Err(ParseError::UrlError(UrlError::PathNotAllowed));
    }
    // http::Uri doesn't provde an accessor for the fragment; See
    // https://github.com/hyperium/http/issues/127. For now just ignore any
    // fragment that is there.

    HostAndPort::try_from(authority)
        .map_err(|e| ParseError::UrlError(UrlError::AuthorityError(e)))
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
