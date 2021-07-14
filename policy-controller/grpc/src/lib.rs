#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

use futures::prelude::*;
use linkerd2_proxy_api::inbound::{
    self as proto,
    inbound_server_discovery_server::{InboundServerDiscovery, InboundServerDiscoveryServer},
};
use linkerd_policy_controller_core::{
    ClientAuthentication, ClientAuthorization, DiscoverInboundServer, IdentityMatch, InboundServer,
    InboundServerStream, IpNet, NetworkMatch, ProxyProtocol,
};
use tracing::trace;

#[derive(Clone, Debug)]
pub struct Server<T> {
    discover: T,
    drain: drain::Watch,
}

impl<T> Server<T>
where
    T: DiscoverInboundServer<(String, String, u16)> + Send + Sync + 'static,
{
    pub fn new(discover: T, drain: drain::Watch) -> Self {
        Self { discover, drain }
    }

    pub async fn serve(
        self,
        addr: std::net::SocketAddr,
        shutdown: impl std::future::Future<Output = ()>,
    ) -> Result<(), tonic::transport::Error> {
        tonic::transport::Server::builder()
            .add_service(InboundServerDiscoveryServer::new(self))
            .serve_with_shutdown(addr, shutdown)
            .await
    }

    fn check_target(
        &self,
        proto::PortSpec { workload, port }: proto::PortSpec,
    ) -> Result<(String, String, u16), tonic::Status> {
        // Parse a workload name in the form namespace:name.
        let (ns, name) = match workload.split_once(':') {
            None => {
                return Err(tonic::Status::invalid_argument(format!(
                    "Invalid workload: {}",
                    workload
                )));
            }
            Some((ns, pod)) if ns.is_empty() || pod.is_empty() => {
                return Err(tonic::Status::invalid_argument(format!(
                    "Invalid workload: {}",
                    workload
                )));
            }
            Some((ns, pod)) => (ns, pod),
        };

        // Ensure that the port is in the valid range.
        let port = {
            if port == 0 || port > std::u16::MAX as u32 {
                return Err(tonic::Status::invalid_argument(format!(
                    "Invalid port: {}",
                    port
                )));
            }
            port as u16
        };

        Ok((ns.to_string(), name.to_string(), port))
    }
}

#[async_trait::async_trait]
impl<T> InboundServerDiscovery for Server<T>
where
    T: DiscoverInboundServer<(String, String, u16)> + Send + Sync + 'static,
{
    async fn get_port(
        &self,
        req: tonic::Request<proto::PortSpec>,
    ) -> Result<tonic::Response<proto::Server>, tonic::Status> {
        let target = self.check_target(req.into_inner())?;

        // Lookup the configuration for an inbound port. If the pod hasn't (yet)
        // been indexed, return a Not Found error.
        let s = self
            .discover
            .get_inbound_server(target)
            .await
            .map_err(|e| tonic::Status::internal(format!("lookup failed: {}", e)))?
            .ok_or_else(|| tonic::Status::not_found("unknown server"))?;

        Ok(tonic::Response::new(to_server(&s)))
    }

    type WatchPortStream = BoxWatchStream;

    async fn watch_port(
        &self,
        req: tonic::Request<proto::PortSpec>,
    ) -> Result<tonic::Response<BoxWatchStream>, tonic::Status> {
        let target = self.check_target(req.into_inner())?;
        let drain = self.drain.clone();
        let rx = self
            .discover
            .watch_inbound_server(target)
            .await
            .map_err(|e| tonic::Status::internal(format!("lookup failed: {}", e)))?
            .ok_or_else(|| tonic::Status::not_found("unknown server"))?;
        Ok(tonic::Response::new(response_stream(drain, rx)))
    }
}

type BoxWatchStream =
    std::pin::Pin<Box<dyn Stream<Item = Result<proto::Server, tonic::Status>> + Send + Sync>>;

fn response_stream(drain: drain::Watch, mut rx: InboundServerStream) -> BoxWatchStream {
    Box::pin(async_stream::try_stream! {
        tokio::pin! {
            let shutdown = drain.signaled();
        }

        loop {
            tokio::select! {
                // When the port is updated with a new server, update the server watch.
                res = rx.next() => match res {
                    Some(s) => {
                        yield to_server(&s);
                    }
                    None => return,
                },

                // If the server starts shutting down, close the stream so that it doesn't hold the
                // server open.
                _ = (&mut shutdown) => {
                    return;
                }
            }
        }
    })
}

fn to_server(srv: &InboundServer) -> proto::Server {
    // Convert the protocol object into a protobuf response.
    let protocol = proto::ProxyProtocol {
        kind: match srv.protocol {
            ProxyProtocol::Detect { timeout } => Some(proto::proxy_protocol::Kind::Detect(
                proto::proxy_protocol::Detect {
                    timeout: Some(timeout.into()),
                },
            )),
            ProxyProtocol::Http1 => Some(proto::proxy_protocol::Kind::Http1(
                proto::proxy_protocol::Http1::default(),
            )),
            ProxyProtocol::Http2 => Some(proto::proxy_protocol::Kind::Http2(
                proto::proxy_protocol::Http2::default(),
            )),
            ProxyProtocol::Grpc => Some(proto::proxy_protocol::Kind::Grpc(
                proto::proxy_protocol::Grpc::default(),
            )),
            ProxyProtocol::Opaque => Some(proto::proxy_protocol::Kind::Opaque(
                proto::proxy_protocol::Opaque {},
            )),
            ProxyProtocol::Tls => Some(proto::proxy_protocol::Kind::Tls(
                proto::proxy_protocol::Tls {},
            )),
        },
    };
    trace!(?protocol);

    let authorizations = srv
        .authorizations
        .iter()
        .map(|(n, c)| to_authz(n, c))
        .collect();
    trace!(?authorizations);

    proto::Server {
        protocol: Some(protocol),
        authorizations,
        ..Default::default()
    }
}

fn to_authz(
    name: impl ToString,
    ClientAuthorization {
        networks,
        authentication,
    }: &ClientAuthorization,
) -> proto::Authz {
    let networks = if networks.is_empty() {
        // TODO use cluster networks (from config).
        vec![
            proto::Network {
                net: Some(IpNet::V4(Default::default()).into()),
                except: vec![],
            },
            proto::Network {
                net: Some(IpNet::V6(Default::default()).into()),
                except: vec![],
            },
        ]
    } else {
        networks
            .iter()
            .map(|NetworkMatch { net, except }| proto::Network {
                net: Some((*net).into()),
                except: except.iter().cloned().map(Into::into).collect(),
            })
            .collect()
    };

    match authentication {
        ClientAuthentication::Unauthenticated => {
            let labels = Some(("authn".to_string(), "false".to_string()))
                .into_iter()
                .chain(Some(("tls".to_string(), "false".to_string())))
                .chain(Some(("name".to_string(), name.to_string())))
                .collect();

            proto::Authz {
                labels,
                networks,
                authentication: Some(proto::Authn {
                    permit: Some(proto::authn::Permit::Unauthenticated(
                        proto::authn::PermitUnauthenticated {},
                    )),
                }),
            }
        }

        ClientAuthentication::TlsUnauthenticated => {
            let labels = Some(("authn".to_string(), "false".to_string()))
                .into_iter()
                .chain(Some(("tls".to_string(), "true".to_string())))
                .chain(Some(("name".to_string(), name.to_string())))
                .collect();

            // todo
            proto::Authz {
                labels,
                networks,
                authentication: Some(proto::Authn {
                    permit: Some(proto::authn::Permit::MeshTls(proto::authn::PermitMeshTls {
                        clients: Some(proto::authn::permit_mesh_tls::Clients::Unauthenticated(
                            proto::authn::PermitUnauthenticated {},
                        )),
                    })),
                }),
            }
        }

        // Authenticated connections must have TLS and apply to all
        // networks.
        ClientAuthentication::TlsAuthenticated(identities) => {
            let labels = Some(("authn".to_string(), "true".to_string()))
                .into_iter()
                .chain(Some(("tls".to_string(), "true".to_string())))
                .chain(Some(("name".to_string(), name.to_string())))
                .collect();

            let authn = {
                let suffixes = identities
                    .iter()
                    .filter_map(|i| match i {
                        IdentityMatch::Suffix(s) => {
                            Some(proto::IdentitySuffix { parts: s.to_vec() })
                        }
                        _ => None,
                    })
                    .collect();

                let identities = identities
                    .iter()
                    .filter_map(|i| match i {
                        IdentityMatch::Name(n) => Some(proto::Identity {
                            name: n.to_string(),
                        }),
                        _ => None,
                    })
                    .collect();

                proto::Authn {
                    permit: Some(proto::authn::Permit::MeshTls(proto::authn::PermitMeshTls {
                        clients: Some(proto::authn::permit_mesh_tls::Clients::Identities(
                            proto::authn::permit_mesh_tls::PermitClientIdentities {
                                identities,
                                suffixes,
                            },
                        )),
                    })),
                }
            };

            proto::Authz {
                labels,
                networks,
                authentication: Some(authn),
            }
        }
    }
}
