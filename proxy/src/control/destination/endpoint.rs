use std::net::SocketAddr;

use telemetry::metrics::DstLabels;


/// An individual traffic target.
///
/// Equality, Ordering, and hashability is determined soley by the Endpoint's address.
#[derive(Clone, Debug, Hash, PartialEq, Eq)]
pub struct Endpoint {
    address: SocketAddr,
    dst_labels: Option<DstLabels>,
}

// ==== impl Endpoint =====

impl Endpoint {
    pub fn new(address: SocketAddr, dst_labels: Option<DstLabels>) -> Self {
        Self {
            address,
            dst_labels,
        }
    }

    pub fn address(&self) -> SocketAddr {
        self.address
    }

    pub fn dst_labels(&self) -> Option<&DstLabels> {
        self.dst_labels.as_ref()
    }
}

impl From<SocketAddr> for Endpoint {
    fn from(address: SocketAddr) -> Self {
        Self {
            address,
            dst_labels: None,
        }
    }
}
