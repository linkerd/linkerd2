use crate::ServerRx;
use anyhow::{anyhow, Error, Result};
use linkerd_policy_controller_core::{
    ClientAuthentication, ClientAuthorization, IdentityMatch, InboundServer, IpNet, NetworkMatch,
    ProxyProtocol,
};
use linkerd_policy_controller_k8s_api as k8s;
use tokio::{sync::watch, time};

#[derive(Copy, Clone, Debug, PartialEq, Eq)]
pub enum DefaultAllow {
    AllAuthenticated,
    AllUnauthenticated,
    ClusterAuthenticated,
    ClusterUnauthenticated,
    Deny,
}

/// Default server configs to use when no server matches.
#[derive(Clone, Debug)]
pub(crate) struct DefaultAllows {
    all_authed_rx: ServerRx,
    all_unauthed_rx: ServerRx,
    cluster_authed_rx: ServerRx,
    cluster_unauthed_rx: ServerRx,
    deny_rx: ServerRx,
}

// === impl DefaultAllow ===

impl DefaultAllow {
    pub const ANNOTATION: &'static str = "policy.linkerd.io/default-allow";

    pub fn from_annotation(meta: &k8s::ObjectMeta) -> Result<Option<Self>> {
        if let Some(v) = meta.annotations.get(Self::ANNOTATION) {
            let mode = v.parse()?;
            Ok(Some(mode))
        } else {
            Ok(None)
        }
    }
}

impl std::str::FromStr for DefaultAllow {
    type Err = Error;

    fn from_str(s: &str) -> Result<Self> {
        match s {
            "all-authenticated" => Ok(Self::AllAuthenticated),
            "all-unauthenticated" => Ok(Self::AllUnauthenticated),
            "cluster-authenticated" => Ok(Self::ClusterAuthenticated),
            "cluster-unauthenticated" => Ok(Self::ClusterUnauthenticated),
            "deny" => Ok(Self::Deny),
            s => Err(anyhow!("invalid mode: {}", s)),
        }
    }
}

impl std::fmt::Display for DefaultAllow {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::AllAuthenticated => "all-authenticated".fmt(f),
            Self::AllUnauthenticated => "all-unauthenticated".fmt(f),
            Self::ClusterAuthenticated => "cluster-authenticated".fmt(f),
            Self::ClusterUnauthenticated => "cluster-unauthenticated".fmt(f),
            Self::Deny => "deny".fmt(f),
        }
    }
}

// === impl DefaultAllows ===

impl DefaultAllows {
    /// Create default allow policy receivers.
    ///
    /// These receivers are never updated. The senders are spawned onto a background task so that
    /// the receivers continue to be live. The background task completes once all receivers are
    /// dropped.
    pub fn spawn(cluster_nets: Vec<IpNet>, detect_timeout: time::Duration) -> Self {
        let any_authenticated =
            ClientAuthentication::TlsAuthenticated(vec![IdentityMatch::Suffix(vec![])]);

        let all_nets = [IpNet::V4(Default::default()), IpNet::V6(Default::default())];

        let (all_authed_tx, all_authed_rx) = watch::channel(mk_detect_config(
            "_all_authed",
            detect_timeout,
            all_nets.iter().cloned(),
            any_authenticated.clone(),
        ));

        let (all_unauthed_tx, all_unauthed_rx) = watch::channel(mk_detect_config(
            "_all_unauthed",
            detect_timeout,
            all_nets.iter().cloned(),
            ClientAuthentication::Unauthenticated,
        ));

        let (cluster_authed_tx, cluster_authed_rx) = watch::channel(mk_detect_config(
            "_cluster_authed",
            detect_timeout,
            cluster_nets.iter().cloned(),
            any_authenticated,
        ));

        let (cluster_unauthed_tx, cluster_unauthed_rx) = watch::channel(mk_detect_config(
            "_cluster_unauthed",
            detect_timeout,
            cluster_nets.into_iter(),
            ClientAuthentication::Unauthenticated,
        ));

        let (deny_tx, deny_rx) = watch::channel(InboundServer {
            protocol: ProxyProtocol::Detect {
                timeout: detect_timeout,
            },
            authorizations: Default::default(),
        });

        // Ensure the senders are not dropped until all receivers are dropped.
        tokio::spawn(async move {
            tokio::join!(
                all_authed_tx.closed(),
                all_unauthed_tx.closed(),
                cluster_authed_tx.closed(),
                cluster_unauthed_tx.closed(),
                deny_tx.closed(),
            );
        });

        Self {
            all_authed_rx,
            all_unauthed_rx,
            cluster_authed_rx,
            cluster_unauthed_rx,
            deny_rx,
        }
    }

    pub fn get(&self, mode: DefaultAllow) -> ServerRx {
        match mode {
            DefaultAllow::AllAuthenticated => self.all_authed_rx.clone(),
            DefaultAllow::AllUnauthenticated => self.all_unauthed_rx.clone(),
            DefaultAllow::ClusterAuthenticated => self.cluster_authed_rx.clone(),
            DefaultAllow::ClusterUnauthenticated => self.cluster_unauthed_rx.clone(),
            DefaultAllow::Deny => self.deny_rx.clone(),
        }
    }
}

fn mk_detect_config(
    name: &'static str,
    timeout: time::Duration,
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
        protocol: ProxyProtocol::Detect { timeout },
        authorizations: Some((name.to_string(), authz)).into_iter().collect(),
    }
}

#[cfg(test)]
mod test {
    use super::*;

    #[test]
    fn test_parse_displayed() {
        for default in &[
            DefaultAllow::Deny,
            DefaultAllow::AllAuthenticated,
            DefaultAllow::AllUnauthenticated,
            DefaultAllow::ClusterAuthenticated,
            DefaultAllow::ClusterUnauthenticated,
        ] {
            assert_eq!(
                default.to_string().parse::<DefaultAllow>().unwrap(),
                *default,
                "failed to parse displayed {:?}",
                *default
            );
        }
    }
}
