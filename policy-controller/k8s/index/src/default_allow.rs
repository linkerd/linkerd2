use crate::{ServerRx, ServerTx};
use anyhow::{anyhow, Error, Result};
use linkerd_policy_controller_core::{
    ClientAuthentication, ClientAuthorization, IdentityMatch, InboundServer, IpNet, NetworkMatch,
    ProxyProtocol,
};
use linkerd_policy_controller_k8s_api as k8s;
use std::{collections::HashMap, hash::Hash};
use tokio::{sync::watch, time};

#[derive(Copy, Clone, Debug, PartialEq, Eq, Hash)]
pub enum DefaultAllow {
    Allow {
        cluster_nets: bool,
        requires_id: bool,
    },
    Deny,
}

#[derive(Debug, PartialEq, Eq)]
pub(crate) struct PodConfig {
    default: DefaultAllow,
    ports: HashMap<u16, PortConfig>,
}

#[derive(Copy, Clone, Debug, Default, PartialEq, Eq, Hash)]
pub(crate) struct PortConfig {
    pub requires_id: bool,
    pub opaque: bool,
}

/// Default server configs to use when no server matches.
#[derive(Debug)]
pub(crate) struct DefaultAllowCache {
    cluster_nets: Vec<IpNet>,
    detect_timeout: time::Duration,

    rxs: HashMap<(DefaultAllow, PortConfig), ServerRx>,
    txs: Vec<ServerTx>,
}

// === impl PodConfig ===

impl PodConfig {
    pub fn new(default: DefaultAllow, ports: impl IntoIterator<Item = (u16, PortConfig)>) -> Self {
        Self {
            default,
            ports: ports.into_iter().collect(),
        }
    }
}

// === impl PortConfig ===

impl From<DefaultAllow> for PodConfig {
    fn from(default: DefaultAllow) -> Self {
        Self::new(default, None)
    }
}

// === impl DefaultAllow ===

impl DefaultAllow {
    pub const ANNOTATION: &'static str = "policy.linkerd.io/default-allow";

    pub fn from_annotation(meta: &k8s::ObjectMeta) -> Result<Option<Self>> {
        if let Some(ann) = meta.annotations.as_ref() {
            if let Some(v) = ann.get(Self::ANNOTATION) {
                let mode = v.parse()?;
                return Ok(Some(mode));
            }
        }

        Ok(None)
    }
}

impl std::str::FromStr for DefaultAllow {
    type Err = Error;

    fn from_str(s: &str) -> Result<Self> {
        match s {
            "all-authenticated" => Ok(Self::Allow {
                cluster_nets: false,
                requires_id: true,
            }),
            "all-unauthenticated" => Ok(Self::Allow {
                cluster_nets: false,
                requires_id: false,
            }),
            "cluster-authenticated" => Ok(Self::Allow {
                cluster_nets: true,
                requires_id: true,
            }),
            "cluster-unauthenticated" => Ok(Self::Allow {
                cluster_nets: true,
                requires_id: false,
            }),
            "deny" => Ok(Self::Deny),
            s => Err(anyhow!("invalid mode: {:?}", s)),
        }
    }
}

impl std::fmt::Display for DefaultAllow {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Allow {
                cluster_nets: false,
                requires_id: true,
            } => "all-authenticated".fmt(f),
            Self::Allow {
                cluster_nets: false,
                requires_id: false,
            } => "all-unauthenticated".fmt(f),
            Self::Allow {
                cluster_nets: true,
                requires_id: true,
            } => "cluster-authenticated".fmt(f),
            Self::Allow {
                cluster_nets: true,
                requires_id: false,
            } => "cluster-unauthenticated".fmt(f),
            Self::Deny => "deny".fmt(f),
        }
    }
}

// === impl DefaultAllowCache ===

impl DefaultAllowCache {
    /// Create default allow policy receivers.
    ///
    /// These receivers are never updated. The senders are moved into a background task so that
    /// the receivers continue to be live. The returned background task completes once all receivers
    /// are dropped.
    pub(crate) fn new(cluster_nets: Vec<IpNet>, detect_timeout: time::Duration) -> Self {
        Self {
            cluster_nets,
            detect_timeout,
            rxs: HashMap::default(),
            txs: Vec::default(),
        }
    }

    pub fn get(&mut self, mode: DefaultAllow, config: PortConfig) -> ServerRx {
        use std::collections::hash_map::Entry;
        match self.rxs.entry((mode, config)) {
            Entry::Occupied(entry) => entry.get().clone(),
            Entry::Vacant(entry) => {
                let protocol = mk_protocol(self.detect_timeout, config.opaque);

                let policy = match mode {
                    DefaultAllow::Allow { cluster_nets, .. } => {
                        let nets = if cluster_nets {
                            self.cluster_nets.clone()
                        } else {
                            vec![IpNet::V4(Default::default()), IpNet::V6(Default::default())]
                        };

                        let authn = if config.requires_id {
                            ClientAuthentication::TlsAuthenticated(vec![IdentityMatch::Suffix(
                                vec![],
                            )])
                        } else {
                            ClientAuthentication::Unauthenticated
                        };

                        mk_policy(&*mode.to_string(), protocol, nets, authn)
                    }

                    DefaultAllow::Deny => InboundServer {
                        protocol,
                        authorizations: Default::default(),
                    },
                };

                let (tx, rx) = watch::channel(policy);
                entry.insert(rx.clone());
                self.txs.push(tx);
                rx
            }
        }
    }
}

fn mk_protocol(timeout: time::Duration, opaque: bool) -> ProxyProtocol {
    if opaque {
        return ProxyProtocol::Opaque;
    }
    ProxyProtocol::Detect { timeout }
}

fn mk_policy(
    name: &'static str,
    protocol: ProxyProtocol,
    nets: impl IntoIterator<Item = IpNet>,
    authentication: ClientAuthentication,
) -> InboundServer {
    let networks = nets
        .into_iter()
        .map(|net| NetworkMatch {
            net,
            except: vec![],
        })
        .collect::<Vec<_>>();
    let authz = ClientAuthorization {
        networks,
        authentication,
    };

    InboundServer {
        protocol,
        authorizations: Some((name.to_string(), authz)).into_iter().collect(),
    }
}

#[cfg(test)]
mod test {
    use super::*;

    #[test]
    fn test_parse_displayed() {
        for default in [
            DefaultAllow::Deny,
            DefaultAllow::Allow {
                cluster_nets: false,
                requires_id: true,
            },
            DefaultAllow::Allow {
                cluster_nets: false,
                requires_id: false,
            },
            DefaultAllow::Allow {
                cluster_nets: true,
                requires_id: false,
            },
            DefaultAllow::Allow {
                cluster_nets: true,
                requires_id: false,
            },
        ] {
            assert_eq!(
                default.to_string().parse().unwrap(),
                default,
                "failed to parse displayed {:?}",
                default
            );
        }
    }
}
