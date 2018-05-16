use futures_watch;
use std::{cmp, hash, net::SocketAddr};

use telemetry::metrics::DstLabels;

pub type DstLabelsWatch = futures_watch::Watch<Option<DstLabels>>;

/// An individual traffic target.
///
/// Equality, Ordering, and hashability is determined soley by the Endpoint's address.
#[derive(Clone, Debug)]
pub struct Endpoint {
    address: SocketAddr,
    dst_labels: Option<DstLabelsWatch>,
}

// ==== impl Endpoint =====

impl Endpoint {
    pub fn new(address: SocketAddr, dst_labels: DstLabelsWatch) -> Self {
        Self {
            address,
            dst_labels: Some(dst_labels),
        }
    }

    pub fn address(&self) -> SocketAddr {
        self.address
    }

    pub fn dst_labels(&self) -> Option<&DstLabelsWatch> {
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

impl hash::Hash for Endpoint {
    fn hash<H: hash::Hasher>(&self, state: &mut H) {
        self.address.hash(state)
    }
}

impl cmp::PartialEq for Endpoint {
    fn eq(&self, other: &Self) -> bool {
        self.address.eq(&other.address)
    }
}

impl cmp::Eq for Endpoint {}
