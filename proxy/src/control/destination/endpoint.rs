use std::net::SocketAddr;

use telemetry::metrics::DstLabels;
use super::Metadata;
use tls;

/// An individual traffic target.
///
/// Equality, Ordering, and hashability is determined soley by the Endpoint's address.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Endpoint {
    address: SocketAddr,
    metadata: Metadata,
}

// ==== impl Endpoint =====

impl Endpoint {
    pub fn new(address: SocketAddr, metadata: Metadata) -> Self {
        Self {
            address,
            metadata,
        }
    }

    pub fn address(&self) -> SocketAddr {
        self.address
    }

    pub fn metadata(&self) -> &Metadata {
        &self.metadata
    }

    pub fn dst_labels(&self) -> Option<&DstLabels> {
        self.metadata.dst_labels()
    }

    pub fn tls_identity(&self) -> Option<&tls::Identity> {
        self.metadata.tls_identity()
    }
}

impl From<SocketAddr> for Endpoint {
    fn from(address: SocketAddr) -> Self {
        Self {
            address,
            metadata: Metadata::no_metadata()
        }
    }
}
