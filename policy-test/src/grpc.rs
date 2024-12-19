//! A gRPC client for the inbound policy API.
//!
//! This client currently discovers a destination controller pod via the k8s API and uses port
//! forwarding to connect to a running instance.

use anyhow::Result;
use linkerd2_proxy_api::{
    inbound::inbound_server_policies_client::InboundServerPoliciesClient,
    outbound::outbound_policies_client::OutboundPoliciesClient,
};
use linkerd_policy_controller_grpc::workload;
use linkerd_policy_controller_k8s_api::{self as k8s, ResourceExt};
use std::{future::Future, pin::Pin};
use tokio::io;

pub use linkerd2_proxy_api::*;

#[macro_export]
macro_rules! assert_is_default_all_unauthenticated {
    ($config:expr) => {
        assert_default_all_unauthenticated_labels!($config);
        assert_eq!($config.authorizations.len(), 1);
    };
}

#[macro_export]
macro_rules! assert_default_all_unauthenticated_labels {
    ($config:expr) => {
        assert_eq!(
            $config.labels,
            vec![
                ("group".to_string(), "".to_string()),
                ("kind".to_string(), "default".to_string()),
                ("name".to_string(), "all-unauthenticated".to_string()),
            ]
            .into_iter()
            .collect()
        );
    };
}

#[macro_export]
macro_rules! assert_protocol_detect {
    ($config:expr) => {{
        use linkerd2_proxy_api::inbound;

        assert_eq!(
            $config.protocol,
            Some(inbound::ProxyProtocol {
                kind: Some(inbound::proxy_protocol::Kind::Detect(
                    inbound::proxy_protocol::Detect {
                        timeout: Some(std::time::Duration::from_secs(10).try_into().unwrap()),
                        http_routes: vec![
                            $crate::grpc::defaults::http_route(),
                            $crate::grpc::defaults::probe_route(),
                        ],
                        http_local_rate_limit: None,
                    }
                )),
            }),
        );
    }};
}

#[macro_export]
macro_rules! assert_protocol_detect_external {
    ($config:expr) => {{
        use linkerd2_proxy_api::inbound;

        assert_eq!(
            $config.protocol,
            Some(inbound::ProxyProtocol {
                kind: Some(inbound::proxy_protocol::Kind::Detect(
                    inbound::proxy_protocol::Detect {
                        timeout: Some(std::time::Duration::from_secs(10).try_into().unwrap()),
                        http_routes: vec![$crate::grpc::defaults::http_route()],
                        http_local_rate_limit: None,
                    }
                ))
            })
        )
    }};
}

#[macro_export]
macro_rules! assert_default_accrual_backoff {
    ($backoff:expr) => {{
        use linkerd2_proxy_api::outbound;
        let default_backoff = outbound::ExponentialBackoff {
            min_backoff: Some(std::time::Duration::from_secs(1).try_into().unwrap()),
            max_backoff: Some(std::time::Duration::from_secs(60).try_into().unwrap()),
            jitter_ratio: 0.5 as f32,
        };
        assert_eq!(&default_backoff, $backoff)
    }};
}

#[derive(Debug)]
pub struct InboundPolicyClient {
    client: InboundServerPoliciesClient<GrpcHttp>,
}

#[derive(Debug)]
pub struct OutboundPolicyClient {
    client: OutboundPoliciesClient<GrpcHttp>,
}

#[derive(Debug)]
struct GrpcHttp {
    tx: hyper::client::conn::http2::SendRequest<tonic::body::BoxBody>,
}

async fn get_policy_controller_pod(client: &kube::Client) -> Result<String> {
    let params =
        kube::api::ListParams::default().labels("linkerd.io/control-plane-component=destination");
    let mut pods = kube::Api::<k8s::Pod>::namespaced(client.clone(), "linkerd")
        .list(&params)
        .await?;
    let pod = pods
        .items
        .pop()
        .ok_or_else(|| anyhow::anyhow!("no destination controller pods found"))?;
    Ok(pod.name_unchecked())
}

async fn connect_port_forward(
    client: &kube::Client,
    pod: &str,
) -> Result<impl io::AsyncRead + io::AsyncWrite + Unpin> {
    loop {
        let mut pf = match kube::Api::<k8s::Pod>::namespaced(client.clone(), "linkerd")
            .portforward(pod, &[8090])
            .await
        {
            Err(kube::Error::UpgradeConnection(
                kube::client::UpgradeConnectionError::ProtocolSwitch(status),
            )) => {
                tracing::info!(?status, "Flakey port forward; retrying");
                tokio::time::sleep(tokio::time::Duration::from_secs(1)).await;
                continue;
            }
            res => res?,
        };

        let io = pf.take_stream(8090).expect("must have a stream");
        return Ok(io);
    }
}

// === impl InboundPolicyClient ===

impl InboundPolicyClient {
    pub async fn port_forwarded(client: &kube::Client) -> Self {
        let pod = get_policy_controller_pod(client)
            .await
            .expect("failed to find a policy controller pod");
        let io = connect_port_forward(client, &pod)
            .await
            .expect("failed to establish a port forward");
        let http = GrpcHttp::handshake(io)
            .await
            .expect("failed to connect to the gRPC server");
        Self {
            client: InboundServerPoliciesClient::new(http),
        }
    }

    pub async fn get_port(
        &mut self,
        ns: &str,
        pod: &str,
        port: u16,
    ) -> Result<inbound::Server, tonic::Status> {
        let rsp = self
            .client
            .get_port(tonic::Request::new(inbound::PortSpec {
                workload: format!("{}:{}", ns, pod),
                port: port as u32,
            }))
            .await?;
        Ok(rsp.into_inner())
    }

    //TODO (matei): we should move our tests over to the new token format
    pub async fn watch_port(
        &mut self,
        ns: &str,
        pod: &str,
        port: u16,
    ) -> Result<tonic::Streaming<inbound::Server>, tonic::Status> {
        let rsp = self
            .client
            .watch_port(tonic::Request::new(inbound::PortSpec {
                workload: format!("{}:{}", ns, pod),
                port: port as u32,
            }))
            .await?;
        Ok(rsp.into_inner())
    }

    //TODO (matei): see if we can collapse this into `get_port` once it supports
    //new token format
    pub async fn get_port_for_external_workload(
        &mut self,
        ns: &str,
        name: &str,
        port: u16,
    ) -> Result<inbound::Server, tonic::Status> {
        let token = serde_json::to_string(&workload::Workload {
            kind: workload::Kind::External(name.into()),
            namespace: ns.into(),
        })
        .unwrap();
        let rsp = self
            .client
            .get_port(tonic::Request::new(inbound::PortSpec {
                workload: token,
                port: port as u32,
            }))
            .await?;

        Ok(rsp.into_inner())
    }

    pub async fn watch_port_for_external_workload(
        &mut self,
        ns: &str,
        name: &str,
        port: u16,
    ) -> Result<tonic::Streaming<inbound::Server>, tonic::Status> {
        let token = serde_json::to_string(&workload::Workload {
            kind: workload::Kind::External(name.into()),
            namespace: ns.into(),
        })
        .unwrap();
        let rsp = self
            .client
            .watch_port(tonic::Request::new(inbound::PortSpec {
                workload: token,
                port: port as u32,
            }))
            .await?;

        Ok(rsp.into_inner())
    }
}

// === impl OutboundPolicyClient ===

impl OutboundPolicyClient {
    pub async fn port_forwarded(client: &kube::Client) -> Self {
        let pod = get_policy_controller_pod(client)
            .await
            .expect("failed to find a policy controller pod");
        let io = connect_port_forward(client, &pod)
            .await
            .expect("failed to establish a port forward");
        let http = GrpcHttp::handshake(io)
            .await
            .expect("failed to connect to the gRPC server");
        Self {
            client: OutboundPoliciesClient::new(http),
        }
    }

    pub async fn get(
        &mut self,
        ns: &str,
        svc: &k8s::Service,
        port: u16,
    ) -> Result<outbound::OutboundPolicy, tonic::Status> {
        use std::net::Ipv4Addr;
        let address = svc
            .spec
            .as_ref()
            .expect("Service must have a spec")
            .cluster_ip
            .as_ref()
            .expect("Service must have a cluster ip");
        let ip = address.parse::<Ipv4Addr>().unwrap();
        let rsp = self
            .client
            .get(tonic::Request::new(outbound::TrafficSpec {
                source_workload: format!("{}:client", ns),
                target: Some(outbound::traffic_spec::Target::Addr(net::TcpAddress {
                    ip: Some(net::IpAddress {
                        ip: Some(net::ip_address::Ip::Ipv4(ip.into())),
                    }),
                    port: port as u32,
                })),
            }))
            .await?;
        Ok(rsp.into_inner())
    }

    pub async fn watch(
        &mut self,
        ns: &str,
        svc: &k8s::Service,
        port: u16,
    ) -> Result<tonic::Streaming<outbound::OutboundPolicy>, tonic::Status> {
        let address = svc
            .spec
            .as_ref()
            .expect("Service must have a spec")
            .cluster_ip
            .as_ref()
            .expect("Service must have a cluster ip");
        self.watch_ip(ns, address, port).await
    }

    pub async fn watch_ip(
        &mut self,
        ns: &str,
        addr: &str,
        port: u16,
    ) -> Result<tonic::Streaming<outbound::OutboundPolicy>, tonic::Status> {
        use std::net::Ipv4Addr;
        let ip = addr.parse::<Ipv4Addr>().unwrap();
        let rsp = self
            .client
            .watch(tonic::Request::new(outbound::TrafficSpec {
                source_workload: format!("{}:client", ns),
                target: Some(outbound::traffic_spec::Target::Addr(net::TcpAddress {
                    ip: Some(net::IpAddress {
                        ip: Some(net::ip_address::Ip::Ipv4(ip.into())),
                    }),
                    port: port as u32,
                })),
            }))
            .await?;
        Ok(rsp.into_inner())
    }
}

// === impl GrpcHttp ===

impl GrpcHttp {
    async fn handshake<I>(io: I) -> Result<Self>
    where
        I: io::AsyncRead + io::AsyncWrite + Unpin + Send + 'static,
    {
        let (tx, conn) = hyper::client::conn::http2::Builder::new(crate::rt::TokioExecutor)
            .handshake(io)
            .await?;
        tokio::spawn(conn);
        Ok(Self { tx })
    }
}

impl hyper::service::Service<hyper::Request<tonic::body::BoxBody>> for GrpcHttp {
    type Response = hyper::Response<hyper::Body>;
    type Error = hyper::Error;
    type Future = Pin<Box<dyn Future<Output = Result<Self::Response, Self::Error>> + Send>>;

    fn poll_ready(
        &mut self,
        cx: &mut std::task::Context<'_>,
    ) -> std::task::Poll<Result<(), Self::Error>> {
        self.tx.poll_ready(cx)
    }

    fn call(&mut self, req: hyper::Request<tonic::body::BoxBody>) -> Self::Future {
        use futures::FutureExt;

        let (mut parts, body) = req.into_parts();

        let mut uri = parts.uri.into_parts();
        uri.scheme = Some(hyper::http::uri::Scheme::HTTP);
        uri.authority = Some(
            "linkerd-destination.linkerd.svc.cluster.local:8090"
                .parse()
                .unwrap(),
        );
        parts.uri = hyper::Uri::from_parts(uri).unwrap();

        self.tx
            .send_request(hyper::Request::from_parts(parts, body))
            .boxed()
    }
}

pub mod defaults {
    use super::*;

    pub fn proxy_protocol(
        local_rate_limit: Option<inbound::HttpLocalRateLimit>,
    ) -> inbound::ProxyProtocol {
        use inbound::proxy_protocol::{Http1, Kind};
        inbound::ProxyProtocol {
            kind: Some(Kind::Http1(Http1 {
                routes: vec![http_route(), probe_route()],
                local_rate_limit,
            })),
        }
    }

    pub fn proxy_protocol_no_ratelimit() -> inbound::ProxyProtocol {
        proxy_protocol(None)
    }

    pub fn proxy_protocol_external() -> inbound::ProxyProtocol {
        use inbound::proxy_protocol::{Http1, Kind};
        inbound::ProxyProtocol {
            kind: Some(Kind::Http1(Http1 {
                routes: vec![http_route()],
                local_rate_limit: None,
            })),
        }
    }

    pub fn http_local_ratelimit(
        name: &str,
        rps_total: Option<u32>,
        rps_identity: Option<u32>,
        overrides: Vec<(u32, Vec<String>)>,
    ) -> inbound::HttpLocalRateLimit {
        use inbound::http_local_rate_limit::{r#override, Limit, Override};
        use meta::{metadata, Metadata, Resource};

        let overrides = overrides
            .iter()
            .map(|ovr| {
                let identities = r#override::ClientIdentities {
                    identities: ovr
                        .1
                        .iter()
                        .map(|name| inbound::Identity {
                            name: name.to_string(),
                        })
                        .collect(),
                };
                Override {
                    limit: Some(Limit {
                        requests_per_second: ovr.0,
                    }),
                    clients: Some(identities),
                }
            })
            .collect();

        inbound::HttpLocalRateLimit {
            metadata: Some(Metadata {
                kind: Some(metadata::Kind::Resource(Resource {
                    group: "policy.linkerd.io".to_string(),
                    kind: "HTTPLocalRateLimitPolicy".to_string(),
                    name: name.to_owned(),
                    ..Default::default()
                })),
            }),
            total: rps_total.map(|requests_per_second| Limit {
                requests_per_second,
            }),
            identity: rps_identity.map(|requests_per_second| Limit {
                requests_per_second,
            }),
            overrides,
        }
    }

    pub fn http_route() -> inbound::HttpRoute {
        use http_route::{path_match, HttpRouteMatch, PathMatch};
        use inbound::{http_route::Rule, HttpRoute};
        use meta::{metadata, Metadata};

        HttpRoute {
            metadata: Some(Metadata {
                kind: Some(metadata::Kind::Default("default".to_owned())),
            }),
            rules: vec![Rule {
                matches: vec![HttpRouteMatch {
                    path: Some(PathMatch {
                        kind: Some(path_match::Kind::Prefix("/".to_owned())),
                    }),
                    ..HttpRouteMatch::default()
                }],
                ..Rule::default()
            }],
            ..HttpRoute::default()
        }
    }

    pub fn probe_route() -> inbound::HttpRoute {
        use http_route::{path_match, HttpRouteMatch, PathMatch};
        use inbound::{
            authn::{Permit, PermitUnauthenticated},
            http_route::Rule,
            Authn, Authz, HttpRoute, Network,
        };
        use ipnet::IpNet;
        use maplit::{convert_args, hashmap};
        use meta::{metadata, Metadata};

        HttpRoute {
            metadata: Some(Metadata {
                kind: Some(metadata::Kind::Default("probe".to_string())),
            }),
            authorizations: vec![Authz {
                networks: vec![
                    Network {
                        net: Some("0.0.0.0/0".parse::<IpNet>().unwrap().into()),
                        ..Network::default()
                    },
                    Network {
                        net: Some("::/0".parse::<IpNet>().unwrap().into()),
                        ..Network::default()
                    },
                ],
                authentication: Some(Authn {
                    permit: Some(Permit::Unauthenticated(PermitUnauthenticated {})),
                }),
                labels: convert_args!(hashmap!(
                    "kind" => "default",
                    "name" => "probe",
                    "group" => "",
                )),
                metadata: Some(Metadata {
                    kind: Some(metadata::Kind::Default("probe".to_string())),
                }),
            }],
            rules: vec![Rule {
                matches: vec![
                    HttpRouteMatch {
                        path: Some(PathMatch {
                            kind: Some(path_match::Kind::Exact("/live".to_string())),
                        }),
                        method: Some(hyper::Method::GET.into()),
                        ..HttpRouteMatch::default()
                    },
                    HttpRouteMatch {
                        path: Some(PathMatch {
                            kind: Some(path_match::Kind::Exact("/ready".to_string())),
                        }),
                        method: Some(hyper::Method::GET.into()),
                        ..HttpRouteMatch::default()
                    },
                ],
                ..Rule::default()
            }],
            ..HttpRoute::default()
        }
    }
}
