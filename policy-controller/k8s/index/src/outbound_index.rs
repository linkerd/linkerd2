use crate::http_route::InboundRouteBinding;
use ahash::AHashMap as HashMap;
use anyhow::Result;
use k8s_gateway_api::HttpBackendRef;
use linkerd_policy_controller_core::{
    http_route::{Backend, OutboundHttpRoute, OutboundHttpRouteRule, WeightedDst},
    OutboundPolicy,
};
use linkerd_policy_controller_k8s_api::{
    policy::{httproute::HttpRouteRule, HttpRoute},
    ResourceExt, Service,
};
use parking_lot::RwLock;
use std::{net::IpAddr, num::NonZeroU16, sync::Arc};
use tokio::sync::watch;

use super::http_route::convert;

pub type SharedIndex = Arc<RwLock<Index>>;

#[derive(Debug)]
pub struct Index {
    namespaces: NamespaceIndex,
    services: HashMap<IpAddr, ServiceRef>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ServiceRef {
    pub name: String,
    pub namespace: String,
}

#[derive(Debug)]
pub struct NamespaceIndex {
    by_ns: HashMap<String, Namespace>,
}

#[derive(Debug, Default)]
struct Namespace {
    services: HashMap<ServicePort, ServiceRoutes>,
}

#[derive(Debug, PartialEq, Eq, Hash)]
struct ServicePort {
    service: String,
    port: NonZeroU16,
}

#[derive(Debug)]
struct ServiceRoutes {
    routes: HashMap<String, OutboundHttpRoute>,
    watch: watch::Sender<OutboundPolicy>,
}

impl kubert::index::IndexNamespacedResource<HttpRoute> for Index {
    fn apply(&mut self, route: HttpRoute) {
        tracing::debug!(name=route.name_unchecked(), "indexing route");
        let ns = route.namespace().expect("HttpRoute must have a namespace");
        self.namespaces.by_ns.entry(ns).or_default().apply(route);
    }

    fn delete(&mut self, namespace: String, name: String) {
        if let Some(ns_index) = self.namespaces.by_ns.get_mut(&namespace) {
            ns_index.delete(name);
        }
    }
}

impl kubert::index::IndexNamespacedResource<Service> for Index {
    fn apply(&mut self, service: Service) {
        let name = service.name_unchecked();
        let ns = service.namespace().expect("Service must have a namespace");
        if let Some(cluster_ip) = service
            .spec
            .and_then(|spec| spec.cluster_ip)
            .filter(|ip| !ip.is_empty() && ip != "None")
        {
            match cluster_ip.parse() {
                Ok(addr) => {
                    let service_ref = ServiceRef {
                        name,
                        namespace: ns,
                    };
                    self.services.insert(addr, service_ref);
                }
                Err(error) => {
                    tracing::error!(%error, service=name, "invalid cluster ip");
                }
            }
        }
    }

    fn delete(&mut self, namespace: String, name: String) {
        let service_ref = ServiceRef { name, namespace };
        self.services.retain(|_, v| *v != service_ref);
    }
}

impl Index {
    pub fn shared() -> SharedIndex {
        Arc::new(RwLock::new(Self {
            namespaces: NamespaceIndex {
                by_ns: HashMap::default(),
            },
            services: HashMap::default(),
        }))
    }

    pub fn outbound_policy_rx(
        &mut self,
        namespace: &str,
        service: &str,
        port: NonZeroU16,
    ) -> Result<watch::Receiver<OutboundPolicy>> {
        let ns = self
            .namespaces
            .by_ns
            .entry(namespace.to_string())
            .or_default();
        let key = ServicePort {
            service: service.to_string(),
            port,
        };
        tracing::debug!(?key, "subscribing to service port");
        let routes = ns.service_routes_or_default(key);
        Ok(routes.watch.subscribe())
    }

    pub fn lookup_service(&self, addr: IpAddr) -> Option<ServiceRef> {
        self.services.get(&addr).cloned()
    }
}

impl Namespace {
    fn apply(&mut self, route: HttpRoute) {
        tracing::debug!(?route);
        for parent_ref in route.spec.inner.parent_refs.iter().flatten() {
            if parent_ref.kind.as_deref() == Some("Service") {
                if let Some(port) = parent_ref.port {
                    if let Some(port) = NonZeroU16::new(port) {
                        let service_port = ServicePort {
                            port,
                            service: parent_ref.name.clone(),
                        };
                        tracing::debug!(?service_port, route=route.name_unchecked(), "inserting route for service");
                        let service_routes = self.service_routes_or_default(service_port);
                        service_routes.apply(route.clone());
                    } else {
                        tracing::warn!(?parent_ref, "ignoring parent_ref with port 0");
                    }
                } else {
                    tracing::warn!(?parent_ref, "ignoring parent_ref without port");
                }
            } else {
                tracing::warn!(parent_kind=parent_ref.kind.as_deref(), "ignoring parent_ref with non-Service kind");
            }
        }
    }

    fn delete(&mut self, name: String) {
        for service in self.services.values_mut() {
            service.delete(&name);
        }
    }

    fn service_routes_or_default(&mut self, service_port: ServicePort) -> &mut ServiceRoutes {
        self.services.entry(service_port).or_insert_with(|| {
            let (sender, _) = watch::channel(OutboundPolicy {
                http_routes: Default::default(),
            });
            ServiceRoutes {
                routes: Default::default(),
                watch: sender,
            }
        })
    }
}

impl ServiceRoutes {
    fn apply(&mut self, route: HttpRoute) {
        let name = route.name_unchecked();
        match convert_route(route) {
            Ok(route) => {
                self.routes.insert(name, route);
                self.send_if_modified();
            }
            Err(error) => tracing::error!(%error, "failed to convert HttpRoute"),
        }
    }

    fn delete(&mut self, name: &String) {
        self.routes.remove(name);
        self.send_if_modified();
    }

    fn send_if_modified(&mut self) {
        self.watch.send_if_modified(|policy| {
            if self.routes == policy.http_routes {
                false
            } else {
                policy.http_routes = self.routes.clone();
                true
            }
        });
    }
}

fn convert_route(route: HttpRoute) -> Result<OutboundHttpRoute> {
    let hostnames = route
        .spec
        .hostnames
        .into_iter()
        .flatten()
        .map(convert::host_match)
        .collect();

    let rules = route
        .spec
        .rules
        .into_iter()
        .flatten()
        .map(convert_rule)
        .collect::<Result<_>>()?;
    Ok(OutboundHttpRoute { hostnames, rules })
}

fn convert_rule(rule: HttpRouteRule) -> Result<OutboundHttpRouteRule> {
    let matches = rule
        .matches
        .into_iter()
        .flatten()
        .map(InboundRouteBinding::try_match)
        .collect::<Result<_>>()?;

    let backends = rule
        .backend_refs
        .into_iter()
        .flatten()
        .filter_map(convert_backend)
        .collect();
    Ok(OutboundHttpRouteRule { matches, backends })
}

fn convert_backend(backend: HttpBackendRef) -> Option<Backend> {
    backend.backend_ref.map(|backend| {
        Backend::Dst(WeightedDst {
            weight: backend.weight.unwrap_or(1).into(),
            authority: backend.name,
        })
    })
}
