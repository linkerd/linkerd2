mod annotation;
mod authorization_policy;
mod concurrency_limit_policy;
mod grpc_routes;
mod http_routes;
mod ratelimit_policy;
mod server_authorization;

use crate::{
    defaults::DefaultPolicy,
    inbound::index::{Index, SharedIndex},
    inbound::server_authorization::ServerSelector,
    ClusterInfo,
};
use ahash::AHashMap as HashMap;
use kubert::index::IndexNamespacedResource;
use linkerd_policy_controller_core::{
    inbound::{
        AuthorizationRef, ClientAuthentication, ClientAuthorization, GrpcRoute, HttpRoute,
        InboundServer, ProxyProtocol, RouteRef, ServerRef,
    },
    IdentityMatch, IpNet, Ipv4Net, Ipv6Net, NetworkMatch,
};
use linkerd_policy_controller_k8s_api::{
    self as k8s,
    api::core::v1::{Container, ContainerPort},
    policy::{server::Port, LocalTargetRef, NamespacedTargetRef},
    ResourceExt,
};
use maplit::*;
use std::sync::Arc;
use tokio::time;

#[test]
fn pod_must_exist_for_lookup() {
    let test = TestConfig::default();
    test.index
        .write()
        .pod_server_rx("ns-0", "pod-0", 8080.try_into().unwrap())
        .expect_err("pod-0.ns-0 must not exist");
}

struct TestConfig {
    index: SharedIndex,
    detect_timeout: time::Duration,
    default_policy: DefaultPolicy,
    cluster: ClusterInfo,
    _tracing: tracing::subscriber::DefaultGuard,
}

const DEFAULTS: [DefaultPolicy; 6] = [
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
        authenticated_only: true,
        cluster_only: true,
    },
    DefaultPolicy::Allow {
        authenticated_only: false,
        cluster_only: true,
    },
    DefaultPolicy::Audit,
];

pub fn mk_pod_with_containers(
    ns: impl ToString,
    name: impl ToString,
    containers: impl IntoIterator<Item = Container>,
) -> k8s::Pod {
    k8s::Pod {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            ..Default::default()
        },
        spec: Some(k8s::api::core::v1::PodSpec {
            containers: containers.into_iter().collect(),
            ..Default::default()
        }),
        ..k8s::Pod::default()
    }
}

fn mk_pod(
    ns: impl ToString,
    name: impl ToString,
    containers: impl IntoIterator<Item = (impl ToString, impl IntoIterator<Item = ContainerPort>)>,
) -> k8s::Pod {
    let containers = containers.into_iter().map(|(name, ports)| Container {
        name: name.to_string(),
        ports: Some(ports.into_iter().collect()),
        ..Default::default()
    });
    mk_pod_with_containers(ns, name, containers)
}

fn mk_server(
    ns: impl ToString,
    name: impl ToString,
    port: Port,
    srv_labels: impl IntoIterator<Item = (&'static str, &'static str)>,
    pod_labels: impl IntoIterator<Item = (&'static str, &'static str)>,
    proxy_protocol: Option<k8s::policy::server::ProxyProtocol>,
) -> k8s::policy::Server {
    k8s::policy::Server {
        metadata: k8s::ObjectMeta {
            namespace: Some(ns.to_string()),
            name: Some(name.to_string()),
            labels: Some(
                srv_labels
                    .into_iter()
                    .map(|(k, v)| (k.to_string(), v.to_string()))
                    .collect(),
            ),
            ..Default::default()
        },
        spec: k8s::policy::ServerSpec {
            port,
            selector: k8s::policy::server::Selector::Pod(pod_labels.into_iter().collect()),
            proxy_protocol,
            access_policy: None,
        },
    }
}

fn mk_default_policy(
    da: DefaultPolicy,
    cluster_nets: Vec<IpNet>,
) -> HashMap<AuthorizationRef, ClientAuthorization> {
    let all_nets = vec![Ipv4Net::default().into(), Ipv6Net::default().into()];

    let cluster_nets = cluster_nets.into_iter().map(NetworkMatch::from).collect();

    let authed = ClientAuthentication::TlsAuthenticated(vec![IdentityMatch::Suffix(vec![])]);

    match da {
        DefaultPolicy::Deny => None,
        DefaultPolicy::Allow {
            authenticated_only: true,
            cluster_only: false,
        } => Some((
            AuthorizationRef::Default("all-authenticated"),
            ClientAuthorization {
                authentication: authed,
                networks: all_nets,
            },
        )),
        DefaultPolicy::Allow {
            authenticated_only: false,
            cluster_only: false,
        } => Some((
            AuthorizationRef::Default("all-unauthenticated"),
            ClientAuthorization {
                authentication: ClientAuthentication::Unauthenticated,
                networks: all_nets,
            },
        )),
        DefaultPolicy::Allow {
            authenticated_only: true,
            cluster_only: true,
        } => Some((
            AuthorizationRef::Default("cluster-authenticated"),
            ClientAuthorization {
                authentication: authed,
                networks: cluster_nets,
            },
        )),
        DefaultPolicy::Allow {
            authenticated_only: false,
            cluster_only: true,
        } => Some((
            AuthorizationRef::Default("cluster-unauthenticated"),
            ClientAuthorization {
                authentication: ClientAuthentication::Unauthenticated,
                networks: cluster_nets,
            },
        )),
        DefaultPolicy::Audit => Some((
            AuthorizationRef::Default("audit"),
            ClientAuthorization {
                authentication: ClientAuthentication::Unauthenticated,
                networks: all_nets,
            },
        )),
    }
    .into_iter()
    .collect()
}

fn mk_default_http_routes() -> HashMap<RouteRef, HttpRoute> {
    Some((RouteRef::Default("default"), HttpRoute::default()))
        .into_iter()
        .collect()
}

fn mk_default_grpc_routes() -> HashMap<RouteRef, GrpcRoute> {
    Some((RouteRef::Default("default"), GrpcRoute::default()))
        .into_iter()
        .collect()
}

impl TestConfig {
    fn from_default_policy(default_policy: DefaultPolicy) -> Self {
        Self::from_default_policy_with_probes(default_policy, vec![])
    }

    fn from_default_policy_with_probes(
        default_policy: DefaultPolicy,
        probe_networks: Vec<IpNet>,
    ) -> Self {
        let _tracing = Self::init_tracing();
        let cluster_net = "192.0.2.0/24".parse().unwrap();
        let detect_timeout = time::Duration::from_secs(1);
        let cluster = ClusterInfo {
            networks: vec![cluster_net],
            control_plane_ns: "linkerd".to_string(),
            identity_domain: "cluster.example.com".into(),
            dns_domain: "cluster.example.com".into(),
            default_policy,
            default_detect_timeout: detect_timeout,
            default_opaque_ports: Default::default(),
            probe_networks,
            global_egress_network_namespace: Arc::new("linkerd-egress".to_string()),
        };
        let index = Index::shared(cluster.clone());
        Self {
            index,
            cluster,
            detect_timeout,
            default_policy,
            _tracing,
        }
    }

    fn default_server(&self) -> InboundServer {
        InboundServer {
            reference: ServerRef::Default(self.default_policy.as_str()),
            authorizations: mk_default_policy(self.default_policy, self.cluster.networks.clone()),
            ratelimit: None,
            concurrency_limit: None,
            protocol: ProxyProtocol::Detect {
                timeout: self.detect_timeout,
            },
            http_routes: mk_default_http_routes(),
            grpc_routes: Default::default(),
        }
    }

    fn init_tracing() -> tracing::subscriber::DefaultGuard {
        tracing::subscriber::set_default(
            tracing_subscriber::fmt()
                .with_test_writer()
                .with_max_level(tracing::Level::TRACE)
                .finish(),
        )
    }
}

impl Default for TestConfig {
    fn default() -> TestConfig {
        Self::from_default_policy(DefaultPolicy::Allow {
            authenticated_only: false,
            cluster_only: true,
        })
    }
}
