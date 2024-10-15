extern crate http as http_crate;

use crate::workload;
use futures::prelude::*;
use http_crate::uri::Authority;
use linkerd2_proxy_api::{
    self as api,
    meta::{metadata, Metadata},
    outbound::{
        self,
        outbound_policies_server::{OutboundPolicies, OutboundPoliciesServer},
    },
};
use linkerd_policy_controller_core::{
    outbound::{
        DiscoverOutboundPolicy, OutboundDiscoverTarget, OutboundPolicy, OutboundPolicyStream,
        OutboundRoute, TargetKind,
    },
    routes::GroupKindNamespaceName,
};
use std::{net::SocketAddr, num::NonZeroU16, str::FromStr, sync::Arc, time};

mod grpc;
mod http;

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
    T: DiscoverOutboundPolicy<OutboundDiscoverTarget> + Send + Sync + 'static,
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
                    OutboundDiscoverTarget {
                        kind: TargetKind::Service,
                        name,
                        namespace,
                        port,
                        source_namespace,
                    }
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
    T: DiscoverOutboundPolicy<OutboundDiscoverTarget> + Send + Sync + 'static,
{
    async fn get(
        &self,
        req: tonic::Request<outbound::TrafficSpec>,
    ) -> Result<tonic::Response<outbound::OutboundPolicy>, tonic::Status> {
        let service = self.lookup(req.into_inner())?;

        let policy = self
            .index
            .get_outbound_policy(service)
            .await
            .map_err(|error| {
                tonic::Status::internal(format!("failed to get outbound policy: {error}"))
            })?;

        if let Some(policy) = policy {
            Ok(tonic::Response::new(to_service(
                policy,
                self.allow_l5d_request_headers,
                original_dst,
            )))
        } else {
            Err(tonic::Status::not_found("No such policy"))
        }
    }

    type WatchStream = BoxWatchStream;

    async fn watch(
        &self,
        req: tonic::Request<outbound::TrafficSpec>,
    ) -> Result<tonic::Response<BoxWatchStream>, tonic::Status> {
        let service = self.lookup(req.into_inner())?;
        let original_dst = service.original_dst;

        let drain = self.drain.clone();

        let rx = self
            .index
            .watch_outbound_policy(service)
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
                        yield to_service(policy, allow_l5d_request_headers,original_dst);
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

fn to_service(
    outbound: OutboundPolicy,
    allow_l5d_request_headers: bool,
    original_dst: Option<SocketAddr>,
) -> outbound::OutboundPolicy {
    let backend: outbound::Backend = default_backend(&outbound);

    let kind = if outbound.opaque {
        outbound::proxy_protocol::Kind::Opaque(outbound::proxy_protocol::Opaque {
            routes: vec![default_outbound_opaq_route(backend)],
        })
    } else {
        let accrual = outbound.accrual.map(|accrual| outbound::FailureAccrual {
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

        let mut http_routes = outbound.http_routes.into_iter().collect::<Vec<_>>();
        let mut grpc_routes = outbound.grpc_routes.into_iter().collect::<Vec<_>>();

        if !grpc_routes.is_empty() {
            grpc_routes.sort_by(timestamp_then_name);
            grpc::protocol(
                backend,
                grpc_routes.into_iter(),
                accrual,
                outbound.grpc_retry,
                outbound.timeouts,
                allow_l5d_request_headers,
                original_dst,
            )
        } else {
            http_routes.sort_by(timestamp_then_name);
            http::protocol(
                backend,
                http_routes.into_iter(),
                accrual,
                outbound.http_retry,
                outbound.timeouts,
                allow_l5d_request_headers,
                original_dst,
            )
        }
    };

    let metadata = Metadata {
        kind: Some(metadata::Kind::Resource(api::meta::Resource {
            group: "core".to_string(),
            kind: "Service".to_string(),
            namespace: outbound.namespace,
            name: outbound.name,
            port: u16::from(outbound.port).into(),
            ..Default::default()
        })),
    };

    outbound::OutboundPolicy {
        metadata: Some(metadata),
        protocol: Some(outbound::ProxyProtocol { kind: Some(kind) }),
    }
}

fn timestamp_then_name<LM, LR, RM, RR>(
    (left_id, left_route): &(GroupKindNamespaceName, OutboundRoute<LM, LR>),
    (right_id, right_route): &(GroupKindNamespaceName, OutboundRoute<RM, RR>),
) -> std::cmp::Ordering {
    let by_ts = match (
        &left_route.creation_timestamp,
        &right_route.creation_timestamp,
    ) {
        (Some(left_ts), Some(right_ts)) => left_ts.cmp(right_ts),
        (None, None) => std::cmp::Ordering::Equal,
        // Routes with timestamps are preferred over routes without.
        (Some(_), None) => return std::cmp::Ordering::Less,
        (None, Some(_)) => return std::cmp::Ordering::Greater,
    };

    by_ts.then_with(|| left_id.name.cmp(&right_id.name))
}

fn default_backend(outbound: &OutboundPolicy) -> outbound::Backend {
    outbound::Backend {
        metadata: Some(Metadata {
            kind: Some(metadata::Kind::Resource(api::meta::Resource {
                group: "core".to_string(),
                kind: "Service".to_string(),
                name: outbound.name.clone(),
                namespace: outbound.namespace.clone(),
                section: Default::default(),
                port: u16::from(outbound.port).into(),
            })),
        }),
        queue: Some(default_queue_config()),
        kind: Some(outbound::backend::Kind::Balancer(
            outbound::backend::BalanceP2c {
                discovery: Some(outbound::backend::EndpointDiscovery {
                    kind: Some(outbound::backend::endpoint_discovery::Kind::Dst(
                        outbound::backend::endpoint_discovery::DestinationGet {
                            path: outbound.authority.clone(),
                        },
                    )),
                }),
                load: Some(default_balancer_config()),
            },
        )),
    }
}

fn default_outbound_opaq_route(backend: outbound::Backend) -> outbound::OpaqueRoute {
    let metadata = Some(Metadata {
        kind: Some(metadata::Kind::Default("opaq".to_string())),
    });
    let rules = vec![outbound::opaque_route::Rule {
        backends: Some(outbound::opaque_route::Distribution {
            kind: Some(outbound::opaque_route::distribution::Kind::FirstAvailable(
                outbound::opaque_route::distribution::FirstAvailable {
                    backends: vec![outbound::opaque_route::RouteBackend {
                        backend: Some(backend),
                    }],
                },
            )),
        }),
    }];
    outbound::OpaqueRoute { metadata, rules }
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
            tracing::error!(%error, "Invalid {name} duration");
        })
        .ok()
}
