use indexmap::IndexSet;
use ipnet::{Contains, Ipv4Net, Ipv6Net};
use std::net;

pub trait AcceptPolicy: ProtocolDetectionDisabled {}

pub trait ProtocolDetectionDisabled {
    fn protocol_detection_disabled(&self, addr: net::SocketAddr) -> bool;
}

#[derive(Debug, Default)]
pub struct Inbound {
    protocol_detection_disabled: Vec<EndpointMatch>,
}

#[derive(Debug, Default)]
pub struct Outbound {
    protocol_detection_disabled: Vec<EndpointMatch>,
}

#[derive(Clone, Debug)]
pub struct EndpointMatch {
    net: Net,
    ports: IndexSet<u16>,
}

#[derive(Clone, Debug)]
pub enum Net {
    V4(Ipv4Net),
    V6(Ipv6Net),
}

// ==== impl Inbound =====

impl Inbound {
    pub fn new(protocol_detection_disabled: Vec<EndpointMatch>) -> Self {
        Self { protocol_detection_disabled }
    }
}

impl ProtocolDetectionDisabled for Inbound {
    fn protocol_detection_disabled(&self, addr: net::SocketAddr) -> bool {
        for m in &self.protocol_detection_disabled {
            if m.protocol_detection_disabled(addr) {
                return true;
            }
        }

        false
    }
}

impl AcceptPolicy for Inbound {}

// ==== impl Outbound =====

impl Outbound {
    pub fn new(protocol_detection_disabled: Vec<EndpointMatch>) -> Self {
        Self { protocol_detection_disabled }
    }
}

impl ProtocolDetectionDisabled for Outbound {
    fn protocol_detection_disabled(&self, addr: net::SocketAddr) -> bool {
        for m in &self.protocol_detection_disabled {
            if m.protocol_detection_disabled(addr) {
                return true;
            }
        }

        false
    }
}

impl AcceptPolicy for Outbound {}

// ==== impl EndpointMatch =====

impl EndpointMatch {
    pub fn new(net: Net, ports: IndexSet<u16>) -> Self {
        Self { net, ports }
    }
}

impl ProtocolDetectionDisabled for EndpointMatch {
    fn protocol_detection_disabled(&self, addr: net::SocketAddr) -> bool {
        self.ports.contains(&addr.port()) && self.net.contains(addr.ip())
    }
}

// ==== impl Net =====

impl Net {
    fn contains(&self, ip: net::IpAddr) -> bool {
        match (self, ip) {
            (&Net::V4(ref net4), net::IpAddr::V4(ref ip4)) => net4.contains(ip4),
            (&Net::V6(ref net6), net::IpAddr::V6(ref ip6)) => net6.contains(ip6),
            _ => false,
        }
    }
}
