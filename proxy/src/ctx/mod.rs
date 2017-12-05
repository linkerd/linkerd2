//! Describes proxy traffic.
//!
//! Contexts are primarily intended to describe traffic contexts for the purposes of
//! telemetry. They may also be useful for, for instance,
//! routing/rate-limiting/policy/etc.
//!
//! As a rule, context types should implement `Clone + Send + Sync`. This allows them to
//! be stored in `http::Extensions`, for instance. Furthermore, because these contexts
//! will be sent to a telemetry processing thread, we want to avoid excessive cloning.
use control::pb::proxy::telemetry as proto;
use std::env;
use std::sync::Arc;
pub mod http;
pub mod transport;

/// Describes a single running proxy instance.
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub struct Process {
    /// Identifies the logical host (or VM) that the process is running on.
    ///
    /// Empty if unknown.
    pub node: String,

    /// Identifies the logical instance name, as scheduled by a scheduler (like
    /// kubernetes).
    ///
    /// Empty if unknown.
    pub scheduled_instance: String,

    /// Identifies the namespace for the `scheduled_instance`.
    ///
    /// Empty if unknown.
    pub scheduled_namespace: String,
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
    pub fn new(node: &str, instance: &str, ns: &str) -> Arc<Self> {
        Arc::new(Self {
            node: node.into(),
            scheduled_instance: instance.into(),
            scheduled_namespace: ns.into(),
        })
    }

    /// Construct a new `Process` from environment variables.
    pub fn from_env() -> Arc<Self> {
        fn get_var(key: &str) -> String {
            env::var(key).unwrap_or_else(|why| {
                warn!(
                    "Process::from_env(): Failed to get value of {} environment variable: {:?}",
                    key,
                    why
                );
                String::from("")
            })
        }

        let node = get_var(::config::ENV_NODE_NAME);
        let scheduled_instance = get_var(::config::ENV_POD_NAME);
        let scheduled_namespace = get_var(::config::ENV_POD_NAMESPACE);
        Arc::new(Self {
            node,
            scheduled_instance,
            scheduled_namespace,
        })
    }
}

impl<'a> Into<proto::Process> for &'a Process {
    fn into(self) -> proto::Process {
        // TODO: can this be implemented without cloning Strings?
        proto::Process {
            node: self.node.clone(),
            scheduled_instance: self.scheduled_instance.clone(),
            scheduled_namespace: self.scheduled_namespace.clone(),
        }
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
