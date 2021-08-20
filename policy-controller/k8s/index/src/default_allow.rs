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
        authenticated_only: bool,
        cluster_only: bool,
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
    pub authenticated: bool,
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
    pub const ANNOTATION: &'static str = "config.linkerd.io/default-inbound-policy";

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
                authenticated_only: true,
                cluster_only: false,
            }),
            "all-unauthenticated" => Ok(Self::Allow {
                authenticated_only: false,
                cluster_only: false,
            }),
            "cluster-authenticated" => Ok(Self::Allow {
                authenticated_only: true,
                cluster_only: true,
            }),
            "cluster-unauthenticated" => Ok(Self::Allow {
                authenticated_only: false,
                cluster_only: true,
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
                authenticated_only: true,
                cluster_only: false,
            } => "all-authenticated".fmt(f),
            Self::Allow {
                authenticated_only: false,
                cluster_only: false,
            } => "all-unauthenticated".fmt(f),
            Self::Allow {
                authenticated_only: true,
                cluster_only: true,
            } => "cluster-authenticated".fmt(f),
            Self::Allow {
                authenticated_only: false,
                cluster_only: true,
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

    pub fn get(&mut self, default: DefaultAllow, config: PortConfig) -> ServerRx {
        use std::collections::hash_map::Entry;
        match self.rxs.entry((default, config)) {
            Entry::Occupied(entry) => entry.get().clone(),
            Entry::Vacant(entry) => {
                let server =
                    Self::mk_server(default, config, &self.cluster_nets, self.detect_timeout);
                let (tx, rx) = watch::channel(server);
                entry.insert(rx.clone());
                self.txs.push(tx);
                rx
            }
        }
    }

    fn mk_server(
        default: DefaultAllow,
        config: PortConfig,
        cluster_nets: &[IpNet],
        detect_timeout: time::Duration,
    ) -> InboundServer {
        let protocol = Self::mk_protocol(detect_timeout, &config);

        match default {
            DefaultAllow::Allow {
                cluster_only,
                authenticated_only,
            } => {
                let nets = if cluster_only {
                    cluster_nets.to_vec()
                } else {
                    vec![IpNet::V4(Default::default()), IpNet::V6(Default::default())]
                };
                let authn = if authenticated_only || config.authenticated {
                    let all_authed = IdentityMatch::Suffix(vec![]);
                    ClientAuthentication::TlsAuthenticated(vec![all_authed])
                } else {
                    ClientAuthentication::Unauthenticated
                };
                Self::mk_policy(&*format!("_{}", default), protocol, nets, authn)
            }

            DefaultAllow::Deny => InboundServer {
                protocol,
                authorizations: Default::default(),
            },
        }
    }

    fn mk_protocol(timeout: time::Duration, config: &PortConfig) -> ProxyProtocol {
        if config.opaque {
            return ProxyProtocol::Opaque;
        }
        ProxyProtocol::Detect { timeout }
    }

    fn mk_policy(
        name: &str,
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
}

#[cfg(test)]
mod test {
    use super::*;

    #[test]
    fn test_parse_displayed() {
        for default in [
            DefaultAllow::Deny,
            DefaultAllow::Allow {
                authenticated_only: true,
                cluster_only: false,
            },
            DefaultAllow::Allow {
                authenticated_only: false,
                cluster_only: false,
            },
            DefaultAllow::Allow {
                authenticated_only: false,
                cluster_only: true,
            },
            DefaultAllow::Allow {
                authenticated_only: false,
                cluster_only: true,
            },
        ] {
            assert_eq!(
                default.to_string().parse::<DefaultAllow>().unwrap(),
                default,
                "failed to parse displayed {:?}",
                default
            );
        }
    }
}
