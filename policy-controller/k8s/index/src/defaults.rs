use ahash::AHashMap as HashMap;
use anyhow::{anyhow, Error, Result};
use linkerd_policy_controller_core::{
    inbound::{AuthorizationRef, ClientAuthentication, ClientAuthorization},
    IdentityMatch, IpNet,
};
use std::hash::Hash;

use crate::ClusterInfo;

/// Indicates the default behavior to apply when no Server is found for a port.
#[derive(Copy, Clone, Debug, PartialEq, Eq, Hash)]
pub enum DefaultPolicy {
    Allow {
        /// Indicates that, by default, all traffic must be authenticated.
        authenticated_only: bool,

        /// Indicates that all traffic must, by default, be from an IP address within the cluster.
        cluster_only: bool,
    },

    /// Indicates that all traffic is denied unless explicitly permitted by an authorization policy.
    Deny,

    /// Indicates that all traffic is let through, but gets audited
    Audit,
}

// === impl DefaultPolicy ===

impl std::str::FromStr for DefaultPolicy {
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
            "audit" => Ok(Self::Audit),
            s => Err(anyhow!("invalid mode: {s:?}")),
        }
    }
}

impl DefaultPolicy {
    pub(crate) fn as_str(&self) -> &'static str {
        match self {
            Self::Allow {
                authenticated_only: true,
                cluster_only: false,
            } => "all-authenticated",
            Self::Allow {
                authenticated_only: false,
                cluster_only: false,
            } => "all-unauthenticated",
            Self::Allow {
                authenticated_only: true,
                cluster_only: true,
            } => "cluster-authenticated",
            Self::Allow {
                authenticated_only: false,
                cluster_only: true,
            } => "cluster-unauthenticated",
            Self::Deny => "deny",
            Self::Audit => "audit",
        }
    }

    pub(crate) fn default_authzs(
        self,
        config: &ClusterInfo,
    ) -> HashMap<AuthorizationRef, ClientAuthorization> {
        let mut authzs = HashMap::default();
        let auth_ref = AuthorizationRef::Default(self.as_str());

        if let DefaultPolicy::Allow {
            authenticated_only,
            cluster_only,
        } = self
        {
            authzs.insert(
                auth_ref,
                Self::default_client_authz(config, authenticated_only, cluster_only),
            );
        } else if let DefaultPolicy::Audit = self {
            authzs.insert(auth_ref, Self::default_client_authz(config, false, false));
        }

        authzs
    }

    fn default_client_authz(
        config: &ClusterInfo,
        authenticated_only: bool,
        cluster_only: bool,
    ) -> ClientAuthorization {
        let authentication = if authenticated_only {
            ClientAuthentication::TlsAuthenticated(vec![IdentityMatch::Suffix(vec![])])
        } else {
            ClientAuthentication::Unauthenticated
        };
        let networks = if cluster_only {
            config.networks.iter().copied().map(Into::into).collect()
        } else {
            vec![
                "0.0.0.0/0".parse::<IpNet>().unwrap().into(),
                "::/0".parse::<IpNet>().unwrap().into(),
            ]
        };

        ClientAuthorization {
            authentication,
            networks,
        }
    }
}

impl std::fmt::Display for DefaultPolicy {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        self.as_str().fmt(f)
    }
}

#[cfg(test)]
mod test {
    use super::*;

    #[test]
    fn test_parse_displayed() {
        for default in [
            DefaultPolicy::Deny,
            DefaultPolicy::Allow {
                authenticated_only: true,
                cluster_only: false,
            },
            DefaultPolicy::Allow {
                authenticated_only: false,
                cluster_only: false,
            },
            DefaultPolicy::Allow {
                authenticated_only: false,
                cluster_only: true,
            },
            DefaultPolicy::Allow {
                authenticated_only: false,
                cluster_only: true,
            },
            DefaultPolicy::Audit,
        ] {
            assert_eq!(
                default.to_string().parse::<DefaultPolicy>().unwrap(),
                default,
                "failed to parse displayed {default:?}"
            );
        }
    }
}
