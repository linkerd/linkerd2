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
pub mod http;
pub mod transport;

/// Describes a single running proxy instance.
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub struct Process {
    /// Identifies the Kubernetes namespace in which this proxy is process.
    pub scheduled_namespace: String,

    pub start_time: SystemTime,
}

/// Indicates the orientation of traffic, relative to a sidecar proxy.
///
/// Each process exposes two proxies:
/// - The _inbound_ proxy receives traffic from another services forwards it to within the
///   local instance.
/// - The  _outbound_ proxy receives traffic from the local instance and forwards it to a
///   remove service.
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub enum Proxy {
    Inbound(Arc<Process>),
    Outbound(Arc<Process>),
}

impl Process {
    #[cfg(test)]
    pub fn test(ns: &str) -> Arc<Self> {
        Arc::new(Self {
            scheduled_namespace: ns.into(),
            start_time: SystemTime::now(),
        })
    }

    /// Construct a new `Process` from environment variables.
    pub fn new(config: &config::Config) -> Arc<Self> {
        let start_time = SystemTime::now();
        Arc::new(Self {
            scheduled_namespace: config.pod_namespace.clone(),
            start_time,
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
}

#[cfg(test)]
pub mod test_util {
    use http;
    use futures_watch;
    use std::{
        fmt,
        net::SocketAddr,
        sync::Arc,
        time::SystemTime,
    };

    use ctx;
    use telemetry::metrics::DstLabels;

    fn addr() -> SocketAddr {
        ([1, 2, 3, 4], 5678).into()
    }

    pub fn process() -> Arc<ctx::Process> {
        Arc::new(ctx::Process {
            scheduled_namespace: "test".into(),
            start_time: SystemTime::now(),
        })
    }

    pub fn server(proxy: &Arc<ctx::Proxy>) -> Arc<ctx::transport::Server> {
        ctx::transport::Server::new(&proxy, &addr(), &addr(), &Some(addr()))
    }

    pub fn client<L, S>(proxy: &Arc<ctx::Proxy>, labels: L) -> Arc<ctx::transport::Client>
    where
        L: IntoIterator<Item=(S, S)>,
        S: fmt::Display,
    {
        let (labels_watch, _store) = futures_watch::Watch::new(DstLabels::new(labels));
        ctx::transport::Client::new(&proxy, &addr(), Some(labels_watch))
    }

    pub fn request(
        uri: &str,
        server: &Arc<ctx::transport::Server>,
        client: &Arc<ctx::transport::Client>,
        id: usize
    ) -> (Arc<ctx::http::Request>, Arc<ctx::http::Response>) {
        let req = ctx::http::Request::new(
            &http::Request::get(uri).body(()).unwrap(),
            &server,
            &client,
            id,
        );
        let rsp = ctx::http::Response::new(
            &http::Response::builder().status(http::StatusCode::OK).body(()).unwrap(),
            &req,
        );
        (req, rsp)
    }
}
