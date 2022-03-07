#[derive(
    Clone,
    Debug,
    Default,
    kube::CustomResource,
    serde::Deserialize,
    serde::Serialize,
    schemars::JsonSchema,
)]
#[kube(
    group = "policy.linkerd.io",
    version = "v1alpha1",
    kind = "NetworkAuthentication",
    namespaced
)]
#[serde(rename_all = "camelCase")]
pub struct NetworkAuthenticationSpec {
    pub networks: Vec<Network>,
}

#[derive(Clone, Debug, Default, serde::Deserialize, serde::Serialize, schemars::JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct Network {
    pub cidr: ipnet::IpNet,
    pub except: Option<Vec<Except>>,
}

#[derive(Clone, Debug, serde::Deserialize, serde::Serialize, schemars::JsonSchema)]
#[serde(untagged)]
pub enum Except {
    IpAddr(std::net::IpAddr),
    IpNet(ipnet::IpNet),
}

#[derive(Debug, thiserror::Error)]
#[error("not a valid CIDR or IP address: {0}")]
pub struct ExceptParseError(String);

// === impl Except ===

impl Except {
    pub fn into_net(self) -> ipnet::IpNet {
        match self {
            Except::IpNet(net) => net,
            Except::IpAddr(addr) => ipnet::IpNet::from(addr),
        }
    }

    pub fn contains(&self, other: &ipnet::IpNet) -> bool {
        match self {
            Except::IpNet(net) => net.contains(other),
            Except::IpAddr(addr) => ipnet::IpNet::from(*addr).contains(other),
        }
    }

    pub fn contained_by(&self, other: &ipnet::IpNet) -> bool {
        match self {
            Except::IpNet(net) => other.contains(net),
            Except::IpAddr(addr) => other.contains(addr),
        }
    }
}

impl std::str::FromStr for Except {
    type Err = ExceptParseError;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        if let Ok(net) = s.parse() {
            return Ok(Except::IpNet(net));
        }

        if let Ok(addr) = s.parse() {
            return Ok(Except::IpAddr(addr));
        }

        Err(ExceptParseError(s.to_string()))
    }
}

impl std::fmt::Display for Except {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Except::IpAddr(addr) => addr.fmt(f),
            Except::IpNet(net) => net.fmt(f),
        }
    }
}
