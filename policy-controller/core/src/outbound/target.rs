use std::{net::SocketAddr, num::NonZeroU16};

/// OutboundDiscoverTarget allows us to express the fact that
/// a policy resolution can be fulfilled by either a resource
/// we know about (a specific EgressNetwork or a Service) or
/// by our fallback mechanism.
#[derive(Clone, Debug)]
pub enum OutboundDiscoverTarget {
    Resource(ResourceTarget),
    Fallback(SocketAddr),
}

#[derive(Clone, Debug)]
pub struct ResourceTarget {
    pub name: String,
    pub namespace: String,
    pub port: NonZeroU16,
    pub source_namespace: String,
    pub kind: Kind,
}

#[derive(Debug, Copy, Clone, Hash, PartialEq, Eq)]
pub enum Kind {
    EgressNetwork(SocketAddr),
    Service,
}

impl ResourceTarget {
    pub fn original_dst(&self) -> Option<SocketAddr> {
        match self.kind {
            Kind::EgressNetwork(original_dst) => Some(original_dst),
            Kind::Service => None,
        }
    }
}
