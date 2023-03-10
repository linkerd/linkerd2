use crate::pod::ports_annotation;
use crate::{http_route::InboundRouteBinding, pod::PortSet};
use ahash::AHashMap as HashMap;
use anyhow::Result;
use k8s_gateway_api::HttpBackendRef;
use linkerd_policy_controller_core::{
    http_route::{Backend, OutboundHttpRoute, OutboundHttpRouteRule, WeightedDst},
    OutboundPolicy,
};
use linkerd_policy_controller_k8s_api::{
    policy::{httproute::HttpRouteRule, HttpRoute},
    ResourceExt, Service, Time,
};
use parking_lot::RwLock;
use std::{fmt, net::IpAddr, num::NonZeroU16, sync::Arc};
use tokio::sync::watch;

use super::http_route::convert;

pub type SharedIndex = Arc<RwLock<Index>>;

#[derive(Debug)]
pub struct Index {
    namespaces: NamespaceIndex,
    services: HashMap<IpAddr, ServiceRef>,
    default_opaque_ports: PortSet,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ServiceRef {
    pub name: String,
    pub namespace: String,
}

#[derive(Debug)]
pub struct NamespaceIndex {
    by_ns: HashMap<String, Namespace>,
    cluster_domain: Arc<String>,
}

#[derive(Debug, Default)]
struct Namespace {
    service_routes: HashMap<ServicePort, ServiceRoutes>,
    namespace: Arc<String>,
    cluster_domain: Arc<String>,
    services: HashMap<String, ServiceInfo>,
}

#[derive(Debug, Default)]
struct ServiceInfo {
    opaque_ports: PortSet,
}

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
struct ServicePort {
    service: String,
    port: NonZeroU16,
}

#[derive(Debug)]
struct ServiceRoutes {
    routes: HashMap<String, OutboundHttpRoute>,
    watch: watch::Sender<OutboundPolicy>,
    opaque: bool,
}

impl kubert::index::IndexNamespacedResource<HttpRoute> for Index {
    fn apply(&mut self, route: HttpRoute) {
        tracing::debug!(name = route.name_unchecked(), "indexing route");
        let ns = route.namespace().expect("HttpRoute must have a namespace");
        self.namespaces
            .by_ns
            .entry(ns.clone())
            .or_insert_with(|| Namespace {
                service_routes: Default::default(),
                namespace: Arc::new(ns),
                cluster_domain: self.namespaces.cluster_domain.clone(),
                services: Default::default(),
            })
            .apply(route);
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
            .as_ref()
            .and_then(|spec| spec.cluster_ip.as_deref())
            .filter(|ip| !ip.is_empty() && *ip != "None")
        {
            match cluster_ip.parse() {
                Ok(addr) => {
                    let service_ref = ServiceRef {
                        name,
                        namespace: ns.clone(),
                    };
                    self.services.insert(addr, service_ref);
                }
                Err(error) => {
                    tracing::error!(%error, service=name, "invalid cluster ip");
                }
            }
        }

        let opaque_ports =
            ports_annotation(service.annotations(), "config.linkerd.io/opaque-ports")
                .unwrap_or_else(|| self.default_opaque_ports.clone());
        let service_info = ServiceInfo { opaque_ports };

        self.namespaces
            .by_ns
            .entry(ns.clone())
            .or_insert_with(|| Namespace {
                service_routes: Default::default(),
                namespace: Arc::new(ns),
                cluster_domain: self.namespaces.cluster_domain.clone(),
                services: Default::default(),
            })
            .update_service(service.name_unchecked(), service_info);
    }

    fn delete(&mut self, namespace: String, name: String) {
        if let Some(ns) = self.namespaces.by_ns.get_mut(&namespace) {
            ns.services.remove(&name);
        }
        let service_ref = ServiceRef { name, namespace };
        self.services.retain(|_, v| *v != service_ref);
    }
}

impl Index {
    pub fn shared(cluster_domain: String, default_opaque_ports: PortSet) -> SharedIndex {
        Arc::new(RwLock::new(Self {
            namespaces: NamespaceIndex {
                by_ns: HashMap::default(),
                cluster_domain: Arc::new(cluster_domain),
            },
            services: HashMap::default(),
            default_opaque_ports,
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
            .or_insert_with(|| Namespace {
                service_routes: Default::default(),
                namespace: Arc::new(namespace.to_string()),
                cluster_domain: self.namespaces.cluster_domain.clone(),
                services: Default::default(),
            });
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
        let name = route.name_unchecked();
        let outbound_route = match self.convert_route(route.clone()) {
            Ok(route) => route,
            Err(error) => {
                tracing::error!(%error, "failed to convert HttpRoute");
                return;
            }
        };
        tracing::debug!(?outbound_route);

        for parent_ref in route.spec.inner.parent_refs.iter().flatten() {
            if parent_ref.kind.as_deref() == Some("Service") {
                if let Some(port) = parent_ref.port {
                    if let Some(port) = NonZeroU16::new(port) {
                        let service_port = ServicePort {
                            port,
                            service: parent_ref.name.clone(),
                        };
                        tracing::debug!(
                            ?service_port,
                            route = route.name_unchecked(),
                            "inserting route for service"
                        );
                        let service_routes = self.service_routes_or_default(service_port);
                        service_routes.apply(name.clone(), outbound_route.clone());
                    } else {
                        tracing::warn!(?parent_ref, "ignoring parent_ref with port 0");
                    }
                } else {
                    tracing::warn!(?parent_ref, "ignoring parent_ref without port");
                }
            } else {
                tracing::warn!(
                    parent_kind = parent_ref.kind.as_deref(),
                    "ignoring parent_ref with non-Service kind"
                );
            }
        }
    }

    fn update_service(&mut self, name: String, service: ServiceInfo) {
        tracing::debug!(?name, ?service, "updating service");
        for (svc_port, svc_routes) in self.service_routes.iter_mut() {
            let opaque = service.opaque_ports.contains(&svc_port.port);
            svc_routes.set_opaque(opaque);
        }
        self.services.insert(name, service);
    }

    fn delete(&mut self, name: String) {
        for service in self.service_routes.values_mut() {
            service.delete(&name);
        }
    }

    fn service_routes_or_default(&mut self, service_port: ServicePort) -> &mut ServiceRoutes {
        let authority = format!(
            "{}.{}.svc.{}:{}",
            service_port.service, self.namespace, self.cluster_domain, service_port.port
        );
        self.service_routes
            .entry(service_port.clone())
            .or_insert_with(|| {
                let opaque = match self.services.get(&service_port.service) {
                    Some(svc) => svc.opaque_ports.contains(&service_port.port),
                    None => false,
                };
                let (sender, _) = watch::channel(OutboundPolicy {
                    http_routes: Default::default(),
                    authority,
                    namespace: self.namespace.to_string(),
                    opaque,
                });
                ServiceRoutes {
                    routes: Default::default(),
                    watch: sender,
                    opaque,
                }
            })
    }

    fn convert_route(&self, route: HttpRoute) -> Result<OutboundHttpRoute> {
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
            .map(|r| self.convert_rule(r))
            .collect::<Result<_>>()?;

        let creation_timestamp = route.metadata.creation_timestamp.map(|Time(t)| t);

        Ok(OutboundHttpRoute {
            hostnames,
            rules,
            creation_timestamp,
        })
    }

    fn convert_rule(&self, rule: HttpRouteRule) -> Result<OutboundHttpRouteRule> {
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
            .filter_map(|b| self.convert_backend(b))
            .collect();
        Ok(OutboundHttpRouteRule { matches, backends })
    }

    fn convert_backend(&self, backend: HttpBackendRef) -> Option<Backend> {
        backend.backend_ref.map(|backend| {
            let port = backend.inner.port.unwrap_or_else(|| {
                tracing::warn!(?backend, "missing port in backend_ref");
                u16::default()
            });
            let dst = WeightedDst {
                weight: backend.weight.unwrap_or(1).into(),
                authority: format!(
                    "{}.{}.svc.{}:{port}",
                    backend.inner.name, self.namespace, self.cluster_domain,
                ),
            };
            if self.services.contains_key(&backend.inner.name) {
                Backend::Dst(dst)
            } else {
                Backend::InvalidDst(dst)
            }
        })
    }
}

impl ServiceRoutes {
    fn apply(&mut self, name: String, route: OutboundHttpRoute) {
        self.routes.insert(name, route);
        self.send_if_modified();
    }

    fn set_opaque(&mut self, opaque: bool) {
        self.opaque = opaque;
        self.send_if_modified();
    }

    fn delete(&mut self, name: &String) {
        self.routes.remove(name);
        self.send_if_modified();
    }

    fn send_if_modified(&mut self) {
        self.watch.send_if_modified(|policy| {
            let mut modified = false;
            if self.routes != policy.http_routes {
                policy.http_routes = self.routes.clone();
                modified = true;
            }
            if self.opaque != policy.opaque {
                policy.opaque = self.opaque;
                modified = true;
            }
            modified
        });
    }
}
