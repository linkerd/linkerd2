use chrono::{offset::Utc, DateTime};
use linkerd_policy_controller_k8s_api::policy::{Cidr, Network, TrafficPolicy};
use linkerd_policy_controller_k8s_api::{policy as linkerd_k8s_api, ResourceExt};
use std::{net::IpAddr, sync::Arc};

#[derive(Debug)]
pub(crate) struct EgressNetwork {
    pub networks: Vec<Network>,
    pub name: String,
    pub namespace: String,
    pub creation_timestamp: Option<DateTime<Utc>>,
    pub traffic_policy: TrafficPolicy,
}

#[derive(Debug, PartialEq, Eq)]
struct MatchedEgressNetwork {
    matched_network_size: usize,
    name: String,
    namespace: String,
    creation_timestamp: Option<DateTime<Utc>>,
    pub traffic_policy: TrafficPolicy,
}

// === impl EgressNetwork ===

impl EgressNetwork {
    pub(crate) fn from_resource(
        r: &linkerd_k8s_api::EgressNetwork,
        cluster_networks: Vec<Cidr>,
    ) -> Self {
        let name = r.name_unchecked();
        let namespace = r.namespace().expect("EgressNetwork must have a namespace");
        let creation_timestamp = r.creation_timestamp().map(|d| d.0);
        let traffic_policy = r.spec.traffic_policy.clone();

        let networks = r.spec.networks.clone().unwrap_or_else(|| {
            let (v6, v4) = cluster_networks.iter().cloned().partition(Cidr::is_ipv6);

            vec![
                Network {
                    cidr: "0.0.0.0/0".parse().expect("should parse"),
                    except: Some(v4),
                },
                Network {
                    cidr: "::/0".parse().expect("should parse"),
                    except: Some(v6),
                },
            ]
        });

        EgressNetwork {
            name,
            namespace,
            networks,
            creation_timestamp,
            traffic_policy,
        }
    }
}

// Attempts to find the best matching network for a certain discovery look-up.
// Logic is:
// 1. if there are Egress networks in the source_namespace, only these are considered
// 2. otherwise only networks from the global egress network namespace are considered
// 2. the target IP is matched against the networks of the EgressNetwork
// 3. ambiguity is resolved as by comparing the networks using compare_matched_egress_network
pub(crate) fn resolve_egress_network<'n>(
    addr: IpAddr,
    source_namespace: String,
    global_external_network_namespace: Arc<String>,
    nets: impl Iterator<Item = &'n EgressNetwork>,
) -> Option<super::ResourceRef> {
    let (same_ns, rest): (Vec<_>, Vec<_>) = nets
        .filter(|en| {
            en.namespace == source_namespace || en.namespace == *global_external_network_namespace
        })
        .partition(|un| un.namespace == source_namespace);
    let to_pick_from = if !same_ns.is_empty() { same_ns } else { rest };

    to_pick_from
        .iter()
        .filter_map(|egress_network| {
            let matched_network_size = match_network(&egress_network.networks, addr)?;
            Some(MatchedEgressNetwork {
                name: egress_network.name.clone(),
                namespace: egress_network.namespace.clone(),
                matched_network_size,
                creation_timestamp: egress_network.creation_timestamp,
                traffic_policy: egress_network.traffic_policy.clone(),
            })
        })
        .max_by(compare_matched_egress_network)
        .map(|m| super::ResourceRef {
            kind: super::ResourceKind::EgressNetwork,
            name: m.name,
            namespace: m.namespace,
        })
}

// Finds a CIDR that contains the given IpAddr. When there are
// multiple CIDRS that match this criteria, the CIDR that is most
// specific (as in having the smallest address space) wins.
fn match_network(networks: &[Network], addr: IpAddr) -> Option<usize> {
    networks
        .iter()
        .filter(|c| c.contains(addr))
        .min_by(|a, b| a.block_size().cmp(&b.block_size()))
        .map(Network::block_size)
}

// This logic compares two MatchedEgressNetwork objects with the purpose
// of picking the one that is more specific. The disambiguation rules are
// as follows:
//  1. prefer the more specific network match (smaller address space size)
//  2. prefer older resource
//  3. all being equal, rely on alphabetical sort of namespace/name
fn compare_matched_egress_network(
    a: &MatchedEgressNetwork,
    b: &MatchedEgressNetwork,
) -> std::cmp::Ordering {
    b.matched_network_size
        .cmp(&a.matched_network_size)
        .then_with(|| a.creation_timestamp.cmp(&b.creation_timestamp).reverse())
        .then_with(|| a.namespace.cmp(&b.namespace).reverse())
        .then_with(|| a.name.cmp(&b.name).reverse())
}

#[cfg(test)]
mod test {
    use super::*;
    use once_cell::sync::Lazy;

    const EGRESS_NETS_NS: &str = "linkerd-external";
    static EN_NS: Lazy<Arc<String>> = Lazy::new(|| Arc::new(EGRESS_NETS_NS.to_string()));

    #[test]
    fn test_picks_smallest_cidr() {
        let ip_addr = "192.168.0.4".parse().unwrap();
        let networks = vec![
            EgressNetwork {
                networks: vec![Network {
                    cidr: "192.168.0.1/16".parse().unwrap(),
                    except: None,
                }],
                name: "net-1".to_string(),
                namespace: EGRESS_NETS_NS.to_string(),
                creation_timestamp: None,
                traffic_policy: TrafficPolicy::Allow,
            },
            EgressNetwork {
                networks: vec![Network {
                    cidr: "192.168.0.1/24".parse().unwrap(),
                    except: None,
                }],
                name: "net-2".to_string(),
                namespace: EGRESS_NETS_NS.to_string(),
                creation_timestamp: None,
                traffic_policy: TrafficPolicy::Allow,
            },
        ];

        let resolved = resolve_egress_network(ip_addr, "ns".into(), EN_NS.clone(), networks.iter());
        assert_eq!(resolved.unwrap().name, "net-2".to_string())
    }

    #[test]
    fn test_picks_local_ns() {
        let ip_addr = "192.168.0.4".parse().unwrap();
        let networks = vec![
            EgressNetwork {
                networks: vec![Network {
                    cidr: "192.168.0.1/16".parse().unwrap(),
                    except: None,
                }],
                name: "net-1".to_string(),
                namespace: "ns-1".to_string(),
                creation_timestamp: None,
                traffic_policy: TrafficPolicy::Allow,
            },
            EgressNetwork {
                networks: vec![Network {
                    cidr: "192.168.0.1/24".parse().unwrap(),
                    except: None,
                }],
                name: "net-2".to_string(),
                namespace: EGRESS_NETS_NS.to_string(),
                creation_timestamp: None,
                traffic_policy: TrafficPolicy::Allow,
            },
        ];

        let resolved =
            resolve_egress_network(ip_addr, "ns-1".into(), EN_NS.clone(), networks.iter());
        assert_eq!(resolved.unwrap().name, "net-1".to_string())
    }

    #[test]
    fn does_not_pick_network_in_unralated_ns() {
        let ip_addr = "192.168.0.4".parse().unwrap();
        let networks = vec![EgressNetwork {
            networks: vec![Network {
                cidr: "192.168.0.1/16".parse().unwrap(),
                except: None,
            }],
            name: "net-1".to_string(),
            namespace: "other-ns".to_string(),
            creation_timestamp: None,
            traffic_policy: TrafficPolicy::Allow,
        }];

        let resolved =
            resolve_egress_network(ip_addr, "ns-1".into(), EN_NS.clone(), networks.iter());
        assert!(resolved.is_none());
    }

    #[test]
    fn test_picks_older_resource() {
        let ip_addr = "192.168.0.4".parse().unwrap();
        let networks = vec![
            EgressNetwork {
                networks: vec![Network {
                    cidr: "192.168.0.1/16".parse().unwrap(),
                    except: None,
                }],
                name: "net-1".to_string(),
                namespace: EGRESS_NETS_NS.to_string(),
                creation_timestamp: Some(DateTime::<Utc>::MAX_UTC),
                traffic_policy: TrafficPolicy::Allow,
            },
            EgressNetwork {
                networks: vec![Network {
                    cidr: "192.168.0.1/16".parse().unwrap(),
                    except: None,
                }],
                name: "net-2".to_string(),
                namespace: EGRESS_NETS_NS.to_string(),
                creation_timestamp: Some(DateTime::<Utc>::MIN_UTC),
                traffic_policy: TrafficPolicy::Allow,
            },
        ];

        let resolved = resolve_egress_network(ip_addr, "ns".into(), EN_NS.clone(), networks.iter());
        assert_eq!(resolved.unwrap().name, "net-2".to_string())
    }

    #[test]
    fn test_picks_alphabetical_order() {
        let ip_addr = "192.168.0.4".parse().unwrap();
        let networks = vec![
            EgressNetwork {
                networks: vec![Network {
                    cidr: "192.168.0.1/16".parse().unwrap(),
                    except: None,
                }],
                name: "a".to_string(),
                namespace: EGRESS_NETS_NS.to_string(),
                creation_timestamp: None,
                traffic_policy: TrafficPolicy::Allow,
            },
            EgressNetwork {
                networks: vec![Network {
                    cidr: "192.168.0.1/16".parse().unwrap(),
                    except: None,
                }],
                name: "b".to_string(),
                namespace: EGRESS_NETS_NS.to_string(),
                creation_timestamp: None,
                traffic_policy: TrafficPolicy::Allow,
            },
        ];

        let resolved = resolve_egress_network(ip_addr, "ns".into(), EN_NS.clone(), networks.iter());
        assert_eq!(resolved.unwrap().name, "a".to_string())
    }

    #[test]
    fn test_respects_exception() {
        let ip_addr = "192.168.0.4".parse().unwrap();
        let networks = vec![
            EgressNetwork {
                networks: vec![Network {
                    cidr: "192.168.0.1/16".parse().unwrap(),
                    except: Some(vec!["192.168.0.4".parse().unwrap()]),
                }],
                name: "b".to_string(),
                namespace: EGRESS_NETS_NS.to_string(),
                creation_timestamp: None,
                traffic_policy: TrafficPolicy::Allow,
            },
            EgressNetwork {
                networks: vec![Network {
                    cidr: "192.168.0.1/16".parse().unwrap(),
                    except: None,
                }],
                name: "d".to_string(),
                namespace: EGRESS_NETS_NS.to_string(),
                creation_timestamp: None,
                traffic_policy: TrafficPolicy::Allow,
            },
        ];

        let resolved = resolve_egress_network(ip_addr, "ns".into(), EN_NS.clone(), networks.iter());
        assert_eq!(resolved.unwrap().name, "d".to_string())
    }
}
