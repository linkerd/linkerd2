extern crate http as http_crate;

use crate::workload;
use futures::{prelude::*, StreamExt};
use http_crate::uri::Authority;
use linkerd2_proxy_api::{
    self as api, destination,
    meta::{metadata, Metadata, Resource},
    outbound::{
        self,
        outbound_policies_server::{OutboundPolicies, OutboundPoliciesServer},
    },
};
use linkerd_policy_controller_core::{
    outbound::{
        AppProtocol, DiscoverOutboundPolicy, ExternalPolicyStream, Kind, OutboundDiscoverTarget,
        OutboundPolicy, OutboundPolicyStream, ParentInfo, ResourceTarget, Route,
        WeightedEgressNetwork, WeightedService,
    },
    routes::GroupKindNamespaceName,
};
use std::{net::SocketAddr, num::NonZeroU16, str::FromStr, sync::Arc, time};
use tracing::{info, warn};

mod grpc;
mod http;
mod tcp;
mod tls;

#[derive(Clone, Debug)]
pub struct OutboundPolicyServer<T> {
    index: T,
    // Used to parse named addresses into <svc>.<ns>.svc.<cluster-domain>.
    cluster_domain: Arc<str>,
    allow_l5d_request_headers: bool,
    drain: drain::Watch,
}

impl<T> OutboundPolicyServer<T>
where
    T: DiscoverOutboundPolicy<ResourceTarget, OutboundDiscoverTarget> + Send + Sync + 'static,
{
    pub fn new(
        discover: T,
        cluster_domain: impl Into<Arc<str>>,
        allow_l5d_request_headers: bool,
        drain: drain::Watch,
    ) -> Self {
        Self {
            index: discover,
            cluster_domain: cluster_domain.into(),
            allow_l5d_request_headers,
            drain,
        }
    }

    pub fn svc(self) -> OutboundPoliciesServer<Self> {
        OutboundPoliciesServer::new(self)
    }

    fn lookup(&self, spec: outbound::TrafficSpec) -> Result<OutboundDiscoverTarget, tonic::Status> {
        let target = spec
            .target
            .ok_or_else(|| tonic::Status::invalid_argument("target is required"))?;
        let source_namespace = workload::Workload::from_str(&spec.source_workload)?.namespace;
        let target = match target {
            outbound::traffic_spec::Target::Addr(target) => target,
            outbound::traffic_spec::Target::Authority(auth) => {
                return self.lookup_authority(&auth).map(|(namespace, name, port)| {
                    OutboundDiscoverTarget::Resource(ResourceTarget {
                        kind: Kind::Service,
                        name,
                        namespace,
                        port,
                        source_namespace,
                    })
                })
            }
        };

        let port = target
            .port
            .try_into()
            .map_err(|_| tonic::Status::invalid_argument("port outside valid range"))?;
        let port = NonZeroU16::new(port)
            .ok_or_else(|| tonic::Status::invalid_argument("port cannot be zero"))?;

        let addr = target
            .ip
            .ok_or_else(|| tonic::Status::invalid_argument("traffic target must have an IP"))?
            .try_into()
            .map_err(|error| {
                tonic::Status::invalid_argument(format!("failed to parse target addr: {error}"))
            })?;

        self.index
            .lookup_ip(addr, port, source_namespace)
            .ok_or_else(|| tonic::Status::not_found("No such service"))
    }

    fn lookup_authority(
        &self,
        authority: &str,
    ) -> Result<(String, String, NonZeroU16), tonic::Status> {
        let auth = authority
            .parse::<Authority>()
            .map_err(|_| tonic::Status::invalid_argument("invalid authority"))?;

        let mut host = auth.host();
        if host.is_empty() {
            return Err(tonic::Status::invalid_argument(
                "authority must have a host",
            ));
        }

        host = host
            .trim_end_matches('.')
            .trim_end_matches(&*self.cluster_domain);

        let mut parts = host.split('.');
        let invalid = {
            let domain = &self.cluster_domain;
            move || {
                tonic::Status::not_found(format!(
                    "authority must be of the form <name>.<namespace>.svc.{domain}",
                ))
            }
        };
        let name = parts.next().ok_or_else(invalid)?;
        let namespace = parts.next().ok_or_else(invalid)?;
        if parts.next() != Some("svc") {
            return Err(invalid());
        };

        let port = auth
            .port_u16()
            .and_then(|p| NonZeroU16::try_from(p).ok())
            .unwrap_or_else(|| 80.try_into().unwrap());

        Ok((namespace.to_string(), name.to_string(), port))
    }
}

#[async_trait::async_trait]
impl<T> OutboundPolicies for OutboundPolicyServer<T>
where
    T: DiscoverOutboundPolicy<ResourceTarget, OutboundDiscoverTarget> + Send + Sync + 'static,
{
    async fn get(
        &self,
        req: tonic::Request<outbound::TrafficSpec>,
    ) -> Result<tonic::Response<outbound::OutboundPolicy>, tonic::Status> {
        let target = self.lookup(req.into_inner())?;

        match target.clone() {
            OutboundDiscoverTarget::Resource(resource) => {
                let original_dst = resource.original_dst();
                let policy = self
                    .index
                    .get_outbound_policy(resource)
                    .await
                    .map_err(|error| {
                        tonic::Status::internal(format!("failed to get outbound policy: {error}"))
                    })?;

                if let Some(policy) = policy {
                    Ok(tonic::Response::new(to_proto(
                        policy,
                        self.allow_l5d_request_headers,
                        original_dst,
                    )))
                } else {
                    Err(tonic::Status::not_found("No such policy"))
                }
            }

            OutboundDiscoverTarget::External(original_dst) => {
                Ok(tonic::Response::new(fallback(original_dst)))
            }
        }
    }

    type WatchStream = BoxWatchStream;

    async fn watch(
        &self,
        req: tonic::Request<outbound::TrafficSpec>,
    ) -> Result<tonic::Response<BoxWatchStream>, tonic::Status> {
        let target = self.lookup(req.into_inner())?;
        let drain = self.drain.clone();

        match target.clone() {
            OutboundDiscoverTarget::Resource(resource) => {
                let original_dst = resource.original_dst();
                let rx = self
                    .index
                    .watch_outbound_policy(resource)
                    .await
                    .map_err(|e| tonic::Status::internal(format!("lookup failed: {e}")))?
                    .ok_or_else(|| tonic::Status::not_found("unknown server"))?;
                Ok(tonic::Response::new(response_stream(
                    drain,
                    rx,
                    self.allow_l5d_request_headers,
                    original_dst,
                )))
            }

            OutboundDiscoverTarget::External(original_dst) => {
                let rx = self.index.watch_external_policy().await;
                Ok(tonic::Response::new(external_stream(
                    drain,
                    rx,
                    original_dst,
                )))
            }
        }
    }
}

type BoxWatchStream = std::pin::Pin<
    Box<dyn Stream<Item = Result<outbound::OutboundPolicy, tonic::Status>> + Send + Sync>,
>;

fn response_stream(
    drain: drain::Watch,
    mut rx: OutboundPolicyStream,
    allow_l5d_request_headers: bool,
    original_dst: Option<SocketAddr>,
) -> BoxWatchStream {
    Box::pin(async_stream::try_stream! {
        tokio::pin! {
            let shutdown = drain.signaled();
        }

        loop {
            tokio::select! {
                // When the port is updated with a new server, update the server watch.
                res = rx.next() => match res {
                    Some(policy) => {
                        yield to_proto(policy, allow_l5d_request_headers, original_dst);
                    }
                    None => return,
                },

                // If the server starts shutting down, close the stream so that it doesn't hold the
                // server open.
                _ = &mut shutdown => {
                    return;
                }
            }
        }
    })
}

fn external_stream(
    drain: drain::Watch,
    mut rx: ExternalPolicyStream,
    original_dst: SocketAddr,
) -> BoxWatchStream {
    Box::pin(async_stream::try_stream! {
        tokio::pin! {
            let shutdown = drain.signaled();
        }

        loop {
            tokio::select! {
                res = rx.next() => match res {
                    Some(_) => {
                        yield fallback(original_dst);
                    }
                    None => return,
                },

                // If the server starts shutting down, close the stream so that it doesn't hold the
                // server open.
                _ = &mut shutdown => {
                    return;
                }
            }
        }
    })
}

fn fallback(original_dst: SocketAddr) -> outbound::OutboundPolicy {
    // This encoder sets deprecated timeouts for older proxies.
    let metadata = Some(Metadata {
        kind: Some(metadata::Kind::Default("egress-fallback".to_string())),
    });

    let backend = outbound::Backend {
        metadata: metadata.clone(),
        queue: Some(default_queue_config()),
        kind: Some(outbound::backend::Kind::Forward(
            destination::WeightedAddr {
                addr: Some(original_dst.into()),
                weight: 1,
                ..Default::default()
            },
        )),
    };

    let opaque = outbound::proxy_protocol::Opaque {
        routes: vec![outbound::OpaqueRoute {
            metadata: Some(Metadata {
                kind: Some(metadata::Kind::Default("egress-fallback".to_string())),
            }),
            rules: vec![outbound::opaque_route::Rule {
                backends: Some(outbound::opaque_route::Distribution {
                    kind: Some(outbound::opaque_route::distribution::Kind::FirstAvailable(
                        outbound::opaque_route::distribution::FirstAvailable {
                            backends: vec![outbound::opaque_route::RouteBackend {
                                backend: Some(backend.clone()),
                                filters: Vec::new(),
                            }],
                        },
                    )),
                }),
                filters: Vec::new(),
            }],
        }],
    };

    let http_routes = vec![outbound::HttpRoute {
        hosts: Vec::default(),
        metadata: metadata.clone(),
        rules: vec![outbound::http_route::Rule {
            backends: Some(outbound::http_route::Distribution {
                kind: Some(outbound::http_route::distribution::Kind::FirstAvailable(
                    outbound::http_route::distribution::FirstAvailable {
                        backends: vec![outbound::http_route::RouteBackend {
                            backend: Some(backend),
                            ..Default::default()
                        }],
                    },
                )),
            }),
            matches: vec![api::http_route::HttpRouteMatch::default()],
            filters: Vec::default(),
            ..Default::default()
        }],
    }];

    outbound::OutboundPolicy {
        metadata,
        protocol: Some(outbound::ProxyProtocol {
            kind: Some(outbound::proxy_protocol::Kind::Detect(
                outbound::proxy_protocol::Detect {
                    timeout: Some(
                        time::Duration::from_secs(10)
                            .try_into()
                            .expect("failed to convert detect timeout to protobuf"),
                    ),
                    opaque: Some(opaque),
                    http1: Some(outbound::proxy_protocol::Http1 {
                        routes: http_routes.clone(),
                        failure_accrual: None,
                    }),
                    http2: Some(outbound::proxy_protocol::Http2 {
                        routes: http_routes,
                        failure_accrual: None,
                    }),
                },
            )),
        }),
    }
}

fn to_proto(
    policy: OutboundPolicy,
    allow_l5d_request_headers: bool,
    original_dst: Option<SocketAddr>,
) -> outbound::OutboundPolicy {
    let backend: outbound::Backend = default_backend(&policy, original_dst);

    let accrual = policy.accrual.map(|accrual| outbound::FailureAccrual {
        kind: Some(match accrual {
            linkerd_policy_controller_core::outbound::FailureAccrual::Consecutive {
                max_failures,
                backoff,
            } => outbound::failure_accrual::Kind::ConsecutiveFailures(
                outbound::failure_accrual::ConsecutiveFailures {
                    max_failures,
                    backoff: Some(outbound::ExponentialBackoff {
                        min_backoff: convert_duration("min_backoff", backoff.min_penalty),
                        max_backoff: convert_duration("max_backoff", backoff.max_penalty),
                        jitter_ratio: backoff.jitter,
                    }),
                },
            ),
        }),
    });

    let mut grpc_routes = policy.grpc_routes.clone().into_iter().collect::<Vec<_>>();
    let mut http_routes = policy.http_routes.clone().into_iter().collect::<Vec<_>>();
    let mut tls_routes = policy.tls_routes.clone().into_iter().collect::<Vec<_>>();
    let mut tcp_routes = policy.tcp_routes.clone().into_iter().collect::<Vec<_>>();

    let kind = match (policy.opaque, &policy.app_protocol) {
        (true, _) | (_, Some(AppProtocol::Opaque)) => {
            outbound::proxy_protocol::Kind::Opaque(outbound::proxy_protocol::Opaque {
                routes: vec![default_outbound_opaq_route(backend, &policy.parent_info)],
            })
        }
        (_, Some(AppProtocol::Http1)) => {
            http_routes.sort_by(timestamp_then_name);
            http::http1_only_protocol(
                backend,
                http_routes.into_iter(),
                accrual,
                policy.http_retry.clone(),
                policy.timeouts.clone(),
                allow_l5d_request_headers,
                &policy.parent_info,
                original_dst,
            )
        }
        (_, Some(AppProtocol::Http2)) => {
            http_routes.sort_by(timestamp_then_name);
            http::http2_only_protocol(
                backend,
                http_routes.into_iter(),
                accrual,
                policy.http_retry.clone(),
                policy.timeouts.clone(),
                allow_l5d_request_headers,
                &policy.parent_info,
                original_dst,
            )
        }
        (_, Some(AppProtocol::Tcp)) => {
            tcp_routes.sort_by(timestamp_then_name);
            tcp::protocol(
                backend,
                tcp_routes.into_iter(),
                &policy.parent_info,
                original_dst,
            )
        }
        (_, Some(AppProtocol::Grpc)) => {
            let mut grpc_routes = policy.grpc_routes.clone().into_iter().collect::<Vec<_>>();
            grpc_routes.sort_by(timestamp_then_name);
            grpc::protocol(
                backend,
                grpc_routes.into_iter(),
                accrual,
                policy.grpc_retry.clone(),
                policy.timeouts.clone(),
                allow_l5d_request_headers,
                &policy.parent_info,
                original_dst,
            )
        }
        (_, Some(AppProtocol::Tls)) => {
            tls_routes.sort_by(timestamp_then_name);
            tls::protocol(
                backend,
                tls_routes.into_iter(),
                &policy.parent_info,
                original_dst,
            )
        }
        (_, Some(AppProtocol::Unknown(_)) | None) => {
            if let Some(AppProtocol::Unknown(protocol)) = &policy.app_protocol {
                warn!(resource = ?policy.parent_info, port = policy.port.get(), "Unknown appProtocol \"{protocol}\"");
            }

            if !grpc_routes.is_empty() {
                grpc_routes.sort_by(timestamp_then_name);
                grpc::protocol(
                    backend,
                    grpc_routes.into_iter(),
                    accrual,
                    policy.grpc_retry.clone(),
                    policy.timeouts.clone(),
                    allow_l5d_request_headers,
                    &policy.parent_info,
                    original_dst,
                )
            } else if !http_routes.is_empty() {
                http_routes.sort_by(timestamp_then_name);
                http::protocol(
                    backend,
                    http_routes.into_iter(),
                    accrual,
                    policy.http_retry.clone(),
                    policy.timeouts.clone(),
                    allow_l5d_request_headers,
                    &policy.parent_info,
                    original_dst,
                )
            } else if !tls_routes.is_empty() {
                tls_routes.sort_by(timestamp_then_name);
                tls::protocol(
                    backend,
                    tls_routes.into_iter(),
                    &policy.parent_info,
                    original_dst,
                )
            } else if !tcp_routes.is_empty() {
                tcp_routes.sort_by(timestamp_then_name);
                tcp::protocol(
                    backend,
                    tcp_routes.into_iter(),
                    &policy.parent_info,
                    original_dst,
                )
            } else {
                http_routes.sort_by(timestamp_then_name);
                http::protocol(
                    backend,
                    http_routes.into_iter(),
                    accrual,
                    policy.http_retry.clone(),
                    policy.timeouts.clone(),
                    allow_l5d_request_headers,
                    &policy.parent_info,
                    original_dst,
                )
            }
        }
    };

    let (parent_group, parent_kind, namespace, name) = match policy.parent_info {
        ParentInfo::EgressNetwork {
            namespace, name, ..
        } => ("policy.linkerd.io", "EgressNetwork", namespace, name),
        ParentInfo::Service {
            name, namespace, ..
        } => ("core", "Service", namespace, name),
    };

    let metadata = Metadata {
        kind: Some(metadata::Kind::Resource(api::meta::Resource {
            group: parent_group.into(),
            kind: parent_kind.into(),
            namespace,
            name,
            port: u16::from(policy.port).into(),
            ..Default::default()
        })),
    };

    info!(
        ?metadata,
        ?policy.app_protocol,
        "created outbound policy"
    );

    outbound::OutboundPolicy {
        metadata: Some(metadata),
        protocol: Some(outbound::ProxyProtocol { kind: Some(kind) }),
    }
}

fn timestamp_then_name<R: Route>(
    (left_id, left_route): &(GroupKindNamespaceName, R),
    (right_id, right_route): &(GroupKindNamespaceName, R),
) -> std::cmp::Ordering {
    let by_ts = match (
        &left_route.creation_timestamp(),
        &right_route.creation_timestamp(),
    ) {
        (Some(left_ts), Some(right_ts)) => left_ts.cmp(right_ts),
        (None, None) => std::cmp::Ordering::Equal,
        // Routes with timestamps are preferred over routes without.
        (Some(_), None) => return std::cmp::Ordering::Less,
        (None, Some(_)) => return std::cmp::Ordering::Greater,
    };

    by_ts.then_with(|| left_id.name.cmp(&right_id.name))
}

fn default_backend(policy: &OutboundPolicy, original_dst: Option<SocketAddr>) -> outbound::Backend {
    match policy.parent_info.clone() {
        ParentInfo::Service {
            authority,
            namespace,
            name,
            ..
        } => outbound::Backend {
            metadata: Some(Metadata {
                kind: Some(metadata::Kind::Resource(api::meta::Resource {
                    group: "core".to_string(),
                    kind: "Service".to_string(),
                    name,
                    namespace,
                    section: Default::default(),
                    port: u16::from(policy.port).into(),
                })),
            }),
            queue: Some(default_queue_config()),
            kind: Some(outbound::backend::Kind::Balancer(
                outbound::backend::BalanceP2c {
                    discovery: Some(outbound::backend::EndpointDiscovery {
                        kind: Some(outbound::backend::endpoint_discovery::Kind::Dst(
                            outbound::backend::endpoint_discovery::DestinationGet {
                                path: authority.clone(),
                            },
                        )),
                    }),
                    load: Some(default_balancer_config()),
                },
            )),
        },

        ParentInfo::EgressNetwork {
            namespace, name, ..
        } => {
            debug_assert!(
                original_dst.is_some(),
                "Must not serve EgressNetwork for named lookups; IP:PORT required"
            );
            let metadata = Some(Metadata {
                kind: Some(metadata::Kind::Resource(api::meta::Resource {
                    group: "policy.linkerd.io".to_string(),
                    kind: "EgressNetwork".to_string(),
                    name,
                    namespace,
                    section: Default::default(),
                    port: u16::from(policy.port).into(),
                })),
            });

            let Some(addr) = original_dst else {
                tracing::error!(
                    ?metadata,
                    "Unexpected state: EgressNetworks should only be returned when lookup is by IP:PORT; synthesizing invalid backend"
                );
                return outbound::Backend {
                    metadata,
                    queue: None,
                    kind: None,
                };
            };

            outbound::Backend {
                metadata,
                queue: Some(default_queue_config()),
                kind: Some(outbound::backend::Kind::Forward(
                    destination::WeightedAddr {
                        addr: Some(addr.into()),
                        weight: 1,
                        ..Default::default()
                    },
                )),
            }
        }
    }
}

fn default_outbound_opaq_route(
    backend: outbound::Backend,
    parent_info: &ParentInfo,
) -> outbound::OpaqueRoute {
    match parent_info {
        ParentInfo::EgressNetwork { traffic_policy, .. } => {
            tcp::default_outbound_egress_route(backend, traffic_policy)
        }
        ParentInfo::Service { .. } => {
            let metadata = Some(Metadata {
                kind: Some(metadata::Kind::Default("opaq".to_string())),
            });
            let rules = vec![outbound::opaque_route::Rule {
                backends: Some(outbound::opaque_route::Distribution {
                    kind: Some(outbound::opaque_route::distribution::Kind::FirstAvailable(
                        outbound::opaque_route::distribution::FirstAvailable {
                            backends: vec![outbound::opaque_route::RouteBackend {
                                backend: Some(backend),
                                filters: Vec::new(),
                            }],
                        },
                    )),
                }),
                filters: Vec::new(),
            }];

            outbound::OpaqueRoute { metadata, rules }
        }
    }
}

fn default_balancer_config() -> outbound::backend::balance_p2c::Load {
    outbound::backend::balance_p2c::Load::PeakEwma(outbound::backend::balance_p2c::PeakEwma {
        default_rtt: Some(
            time::Duration::from_millis(30)
                .try_into()
                .expect("failed to convert ewma default_rtt to protobuf"),
        ),
        decay: Some(
            time::Duration::from_secs(10)
                .try_into()
                .expect("failed to convert ewma decay to protobuf"),
        ),
    })
}

fn default_queue_config() -> outbound::Queue {
    outbound::Queue {
        capacity: 100,
        failfast_timeout: Some(
            time::Duration::from_secs(3)
                .try_into()
                .expect("failed to convert failfast_timeout to protobuf"),
        ),
    }
}

pub(crate) fn convert_duration(
    name: &'static str,
    duration: time::Duration,
) -> Option<prost_types::Duration> {
    duration
        .try_into()
        .map_err(|error| {
            tracing::warn!(%error, "Invalid {name} duration");
        })
        .ok()
}

pub(crate) fn service_meta(svc: WeightedService) -> Metadata {
    Metadata {
        kind: Some(metadata::Kind::Resource(Resource {
            group: "core".to_string(),
            kind: "Service".to_string(),
            name: svc.name,
            namespace: svc.namespace,
            section: Default::default(),
            port: u16::from(svc.port).into(),
        })),
    }
}

pub(crate) fn egress_net_meta(
    egress_net: WeightedEgressNetwork,
    original_dst_port: Option<u16>,
) -> Metadata {
    let port = egress_net
        .port
        .map(NonZeroU16::get)
        .or(original_dst_port)
        .unwrap_or_default();

    Metadata {
        kind: Some(metadata::Kind::Resource(Resource {
            group: "policy.linkerd.io".to_string(),
            kind: "EgressNetwork".to_string(),
            name: egress_net.name,
            namespace: egress_net.namespace,
            section: Default::default(),
            port: port.into(),
        })),
    }
}
