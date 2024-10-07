#[derive(
    Clone, Debug, Eq, PartialEq, serde::Deserialize, serde::Serialize, schemars::JsonSchema,
)]
#[serde(rename_all = "camelCase")]
pub struct Network {
    pub cidr: Cidr,
    pub except: Option<Vec<Cidr>>,
}

#[derive(
    Copy, Clone, Debug, Eq, PartialEq, serde::Deserialize, serde::Serialize, schemars::JsonSchema,
)]
#[serde(untagged)]
pub enum Cidr {
    Addr(std::net::IpAddr),
    Net(ipnet::IpNet),
}

#[derive(Debug, thiserror::Error)]
#[error("not a valid CIDR or IP address: {0}")]
pub struct CidrParseError(String);

// === impl Cidr ===

impl Cidr {
    #[inline]
    pub fn contains(&self, other: &Self) -> bool {
        match (self, other) {
            (Self::Net(this), Self::Net(other)) => this.contains(other),
            (Self::Net(this), Self::Addr(other)) => this.contains(other),
            (Self::Addr(this), Self::Net(other)) => ipnet::IpNet::from(*this).contains(other),
            (Self::Addr(this), Self::Addr(other)) => this == other,
        }
    }

    pub fn size(&self) -> usize {
        match self {
            Cidr::Net(net) => net.hosts().count(),
            Cidr::Addr(_) => 1,
        }
    }
}

impl std::str::FromStr for Cidr {
    type Err = CidrParseError;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        if let Ok(net) = s.parse() {
            return Ok(Self::Net(net));
        }

        if let Ok(addr) = s.parse() {
            return Ok(Self::Addr(addr));
        }

        Err(CidrParseError(s.to_string()))
    }
}

impl From<Cidr> for ipnet::IpNet {
    fn from(cidr: Cidr) -> ipnet::IpNet {
        match cidr {
            Cidr::Net(net) => net,
            Cidr::Addr(addr) => ipnet::IpNet::from(addr),
        }
    }
}

impl From<ipnet::IpNet> for Cidr {
    fn from(net: ipnet::IpNet) -> Self {
        Self::Net(net)
    }
}

impl From<ipnet::Ipv4Net> for Cidr {
    fn from(net: ipnet::Ipv4Net) -> Self {
        Self::Net(net.into())
    }
}

impl From<ipnet::Ipv6Net> for Cidr {
    fn from(net: ipnet::Ipv6Net) -> Self {
        Self::Net(net.into())
    }
}

impl From<std::net::IpAddr> for Cidr {
    fn from(net: std::net::IpAddr) -> Self {
        Self::Addr(net)
    }
}

impl From<std::net::Ipv4Addr> for Cidr {
    fn from(addr: std::net::Ipv4Addr) -> Self {
        Self::Addr(addr.into())
    }
}

impl From<std::net::Ipv6Addr> for Cidr {
    fn from(addr: std::net::Ipv6Addr) -> Self {
        Self::Addr(addr.into())
    }
}

impl std::fmt::Display for Cidr {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Addr(addr) => addr.fmt(f),
            Self::Net(net) => net.fmt(f),
        }
    }
}
