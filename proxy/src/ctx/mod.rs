//! Describes proxy traffic.
//!
//! Contexts are primarily intended to describe traffic contexts for the purposes of
//! telemetry. They may also be useful for, for instance,
//! routing/rate-limiting/policy/etc.
//!
//! As a rule, context types should implement `Clone + Send + Sync`. This allows them to
//! be stored in `http::Extensions`, for instance. Furthermore, because these contexts
//! will be sent to a telemetry processing thread, we want to avoid excessive cloning.
use config;
use std::time::SystemTime;
use std::sync::Arc;
use transport::tls;

pub mod http;
pub mod transport;

/// Describes a single running proxy instance.
#[derive(Clone, Debug)]
pub struct Process {
    /// Identifies the Kubernetes namespace in which this proxy is process.
    pub scheduled_namespace: String,

    pub start_time: SystemTime,

    tls_client_config: tls::ClientConfigWatch,
}

/// Indicates the orientation of traffic, relative to a sidecar proxy.
///
/// Each process exposes two proxies:
/// - The _inbound_ proxy receives traffic from another services forwards it to within the
///   local instance.
/// - The  _outbound_ proxy receives traffic from the local instance and forwards it to a
///   remove service.
#[derive(Clone, Debug)]
pub enum Proxy {
    Inbound(Arc<Process>),
    Outbound(Arc<Process>),
}

impl Process {
    // Test-only, but we can't use `#[cfg(test)]` because it is used by the
    // benchmarks
    pub fn test(ns: &str) -> Arc<Self> {
        Arc::new(Self {
            scheduled_namespace: ns.into(),
            start_time: SystemTime::now(),
            tls_client_config: tls::ClientConfig::no_tls(),
        })
    }

    /// Construct a new `Process` from environment variables.
    pub fn new(config: &config::Config, tls_client_config: tls::ClientConfigWatch) -> Arc<Self> {
        let start_time = SystemTime::now();
        Arc::new(Self {
            scheduled_namespace: config.namespaces.pod.clone(),
            start_time,
            tls_client_config,
        })
    }
}

impl Proxy {
    pub fn inbound(p: &Arc<Process>) -> Arc<Self> {
        Arc::new(Proxy::Inbound(Arc::clone(p)))
    }

    pub fn outbound(p: &Arc<Process>) -> Arc<Self> {
        Arc::new(Proxy::Outbound(Arc::clone(p)))
    }

    pub fn is_inbound(&self) -> bool {
        match *self {
            Proxy::Inbound(_) => true,
            Proxy::Outbound(_) => false,
        }
    }

    pub fn is_outbound(&self) -> bool {
        !self.is_inbound()
    }

    pub fn tls_client_config_watch(&self) -> &tls::ClientConfigWatch {
        match self {
            Proxy::Inbound(process) | Proxy::Outbound(process) => &process.tls_client_config
        }
    }
}

#[cfg(test)]
pub mod test_util {
    use http;
    use std::{
        fmt,
        net::SocketAddr,
        sync::Arc,
    };

    use ctx;
    use control::destination;
    use telemetry::metrics::DstLabels;
    use tls;
    use conditional::Conditional;

    fn addr() -> SocketAddr {
        ([1, 2, 3, 4], 5678).into()
    }

    pub fn process() -> Arc<ctx::Process> {
        ctx::Process::test("test")
    }

    pub fn server(
        proxy: &Arc<ctx::Proxy>,
        tls: ctx::transport::TlsStatus
    ) -> Arc<ctx::transport::Server> {
        ctx::transport::Server::new(&proxy, &addr(), &addr(), &Some(addr()), tls)
    }

    pub fn client<L, S>(
        proxy: &Arc<ctx::Proxy>,
        labels: L,
        tls: ctx::transport::TlsStatus,
    ) -> Arc<ctx::transport::Client>
    where
        L: IntoIterator<Item=(S, S)>,
        S: fmt::Display,
    {
        let meta = destination::Metadata::new(DstLabels::new(labels),
            Conditional::None(tls::ReasonForNoIdentity::NotProvidedByServiceDiscovery));
        ctx::transport::Client::new(&proxy, &addr(), meta, tls)
    }

    pub fn request(
        uri: &str,
        server: &Arc<ctx::transport::Server>,
        client: &Arc<ctx::transport::Client>,
    ) -> (Arc<ctx::http::Request>, Arc<ctx::http::Response>) {
        let req = ctx::http::Request::new(
            &http::Request::get(uri).body(()).unwrap(),
            &server,
            &client,
        );
        let rsp = ctx::http::Response::new(
            &http::Response::builder().status(http::StatusCode::OK).body(()).unwrap(),
            &req,
        );
        (req, rsp)
    }
}
