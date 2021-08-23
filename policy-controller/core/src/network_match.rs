use ipnet::{IpNet, Ipv4Net, Ipv6Net};
use std::net::IpAddr;

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub struct NetworkMatch {
    /// A network to match against.
    pub net: IpNet,

    /// Networks to exclude from the match.
    pub except: Vec<IpNet>,
}

// === impl NetworkMatch ===

impl From<IpAddr> for NetworkMatch {
    fn from(net: IpAddr) -> Self {
        IpNet::from(net).into()
    }
}

impl From<IpNet> for NetworkMatch {
    fn from(net: IpNet) -> Self {
        Self {
            net,
            except: vec![],
        }
    }
}

impl From<Ipv4Net> for NetworkMatch {
    fn from(net: Ipv4Net) -> Self {
        IpNet::from(net).into()
    }
}

impl From<Ipv6Net> for NetworkMatch {
    fn from(net: Ipv6Net) -> Self {
        IpNet::from(net).into()
    }
}
