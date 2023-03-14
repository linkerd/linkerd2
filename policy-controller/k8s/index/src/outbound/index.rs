use crate::{
    http_route,
    pod::{ports_annotation, PortSet},
    ClusterInfo,
};
use ahash::AHashMap as HashMap;
use anyhow::Result;
use k8s_gateway_api::{BackendObjectReference, HttpBackendRef, ParentReference};
use linkerd_policy_controller_core::outbound::{
    Backend, HttpRoute, HttpRouteRule, OutboundPolicy, WeightedService,
};
use linkerd_policy_controller_k8s_api::{policy as api, ResourceExt, Service, Time};
use parking_lot::RwLock;
use std::{net::IpAddr, num::NonZeroU16, sync::Arc};
use tokio::sync::watch;

#[derive(Debug)]
pub struct Index {
    namespaces: NamespaceIndex,
    services: HashMap<IpAddr, ServiceRef>,
}

pub type SharedIndex = Arc<RwLock<Index>>;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ServiceRef {
    pub name: String,
    pub namespace: String,
}

/// Holds all `Pod`, `Server`, and `ServerAuthorization` indices by-namespace.
#[derive(Debug)]
struct NamespaceIndex {
    cluster_info: Arc<ClusterInfo>,
    by_ns: HashMap<String, Namespace>,
}

#[derive(Debug)]
struct Namespace {
    service_routes: HashMap<ServicePort, ServiceRoutes>,
    namespace: Arc<String>,
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
    routes: HashMap<String, HttpRoute>,
    watch: watch::Sender<OutboundPolicy>,
    opaque: bool,
}

impl kubert::index::IndexNamespacedResource<api::HttpRoute> for Index {
    fn apply(&mut self, route: api::HttpRoute) {
        tracing::debug!(name = route.name_unchecked(), "indexing route");
        let ns = route.namespace().expect("HttpRoute must have a namespace");
        self.namespaces
            .by_ns
            .entry(ns.clone())
            .or_insert_with(|| Namespace {
                service_routes: Default::default(),
                namespace: Arc::new(ns),
                services: Default::default(),
            })
            .apply(route, &self.namespaces.cluster_info);
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
                    tracing::error!(%error, service=name, cluster_ip, "invalid cluster ip");
                }
            }
        }

        let opaque_ports =
            ports_annotation(service.annotations(), "config.linkerd.io/opaque-ports")
                .unwrap_or_else(|| self.namespaces.cluster_info.default_opaque_ports.clone());
        let service_info = ServiceInfo { opaque_ports };

        self.namespaces
            .by_ns
            .entry(ns.clone())
            .or_insert_with(|| Namespace {
                service_routes: Default::default(),
                namespace: Arc::new(ns),
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
    pub fn shared(cluster_info: Arc<ClusterInfo>) -> SharedIndex {
        Arc::new(RwLock::new(Self {
            namespaces: NamespaceIndex {
                by_ns: HashMap::default(),
                cluster_info,
            },
            services: HashMap::default(),
        }))
    }

    pub fn outbound_policy_rx(
        &mut self,
        namespace: String,
        service: String,
        port: NonZeroU16,
    ) -> Result<watch::Receiver<OutboundPolicy>> {
        let ns = self
            .namespaces
            .by_ns
            .entry(namespace.clone())
            .or_insert_with(|| Namespace {
                service_routes: Default::default(),
                namespace: Arc::new(namespace.to_string()),
                services: Default::default(),
            });
        let key = ServicePort { service, port };
        tracing::debug!(?key, "subscribing to service port");
        let routes = ns.service_routes_or_default(key, &self.namespaces.cluster_info);
        Ok(routes.watch.subscribe())
    }

    pub fn lookup_service(&self, addr: IpAddr) -> Option<ServiceRef> {
        self.services.get(&addr).cloned()
    }
}

impl Namespace {
    fn apply(&mut self, route: api::HttpRoute, cluster_info: &ClusterInfo) {
        tracing::debug!(?route);
        let name = route.name_unchecked();
        let outbound_route = match self.convert_route(route.clone(), cluster_info) {
            Ok(route) => route,
            Err(error) => {
                // XXX(ver) This is likely to fire whenever we process routes
                // that target servers, for instance. Ultimately, we should
                // unify the handling. Either that or we should reduce the log
                // level to avoid user-facing noise.
                tracing::error!(%error, "failed to convert HttpRoute");
                return;
            }
        };
        tracing::debug!(?outbound_route);

        for parent_ref in route.spec.inner.parent_refs.iter().flatten() {
            if !is_parent_service(parent_ref) {
                // XXX(ver) This is likely to fire whenever we process routes
                // that only target inbound resources.
                tracing::warn!(
                    parent_kind = parent_ref.kind.as_deref(),
                    "ignoring parent_ref with non-Service kind"
                );
                continue;
            }

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
                    let service_routes = self.service_routes_or_default(service_port, cluster_info);
                    service_routes.apply(name.clone(), outbound_route.clone());
                } else {
                    tracing::warn!(?parent_ref, "ignoring parent_ref with port 0");
                }
            } else {
                tracing::warn!(?parent_ref, "ignoring parent_ref without port");
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

    fn service_routes_or_default(
        &mut self,
        sp: ServicePort,
        cluster: &ClusterInfo,
    ) -> &mut ServiceRoutes {
        self.service_routes.entry(sp.clone()).or_insert_with(|| {
            let authority = cluster.service_dns_authority(&self.namespace, &sp.service, sp.port);
            let opaque = match self.services.get(&sp.service) {
                Some(svc) => svc.opaque_ports.contains(&sp.port),
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

    fn convert_route(&self, route: api::HttpRoute, cluster: &ClusterInfo) -> Result<HttpRoute> {
        let hostnames = route
            .spec
            .hostnames
            .into_iter()
            .flatten()
            .map(http_route::host_match)
            .collect();

        let rules = route
            .spec
            .rules
            .into_iter()
            .flatten()
            .map(|r| self.convert_rule(r, cluster))
            .collect::<Result<_>>()?;

        let creation_timestamp = route.metadata.creation_timestamp.map(|Time(t)| t);

        Ok(HttpRoute {
            hostnames,
            rules,
            creation_timestamp,
        })
    }

    fn convert_rule(
        &self,
        rule: api::httproute::HttpRouteRule,
        cluster: &ClusterInfo,
    ) -> Result<HttpRouteRule> {
        let matches = rule
            .matches
            .into_iter()
            .flatten()
            .map(http_route::try_match)
            .collect::<Result<_>>()?;

        let backends = rule
            .backend_refs
            .into_iter()
            .flatten()
            .filter_map(|b| convert_backend(&self.namespace, b, cluster, &self.services))
            .collect();
        Ok(HttpRouteRule { matches, backends })
    }
}

fn convert_backend(
    ns: &str,
    backend: HttpBackendRef,
    cluster: &ClusterInfo,
    services: &HashMap<String, ServiceInfo>,
) -> Option<Backend> {
    backend.backend_ref.map(|backend| {
        if !is_backend_service(&backend.inner) {
            return Backend::Invalid {
                weight: backend.weight.unwrap_or(1).into(),
                message: format!(
                    "unsupported backend type {group} {kind}",
                    group = backend.inner.group.as_deref().unwrap_or("core"),
                    kind = backend.inner.kind.as_deref().unwrap_or("<empty>"),
                ),
            };
        }

        let name = backend.inner.name;
        let weight = backend.weight.unwrap_or(1);

        // The gateway API dictates:
        //
        // Port is required when the referent is a Kubernetes Service.
        let port = match backend
            .inner
            .port
            .and_then(|p| NonZeroU16::try_from(p).ok())
        {
            Some(port) => port,
            None => {
                return Backend::Invalid {
                    weight: weight.into(),
                    message: format!("missing port for backend Service {name}"),
                }
            }
        };

        if !services.contains_key(&name) {
            return Backend::Invalid {
                weight: weight.into(),
                message: format!("Service not found {name}"),
            };
        }

        Backend::Service(WeightedService {
            weight: weight.into(),
            authority: cluster.service_dns_authority(ns, &name, port),
            name,
            namespace: ns.to_string(),
        })
    })
}

#[inline]
fn is_parent_service(parent: &ParentReference) -> bool {
    parent
        .kind
        .as_deref()
        .map(|k| is_service(parent.group.as_deref(), k))
        // Parent refs require a `kind`.
        .unwrap_or(false)
}

#[inline]
fn is_backend_service(backend: &BackendObjectReference) -> bool {
    is_service(
        backend.group.as_deref(),
        // Backends default to `Service` if no kind is specified.
        backend.kind.as_deref().unwrap_or("Service"),
    )
}

#[inline]
fn is_service(group: Option<&str>, kind: &str) -> bool {
    // If the group is not specified or empty, assume it's 'core'.
    group
        .map(|g| g.eq_ignore_ascii_case("core") || g.is_empty())
        .unwrap_or(true)
        && kind.eq_ignore_ascii_case("Service")
}

impl ServiceRoutes {
    fn apply(&mut self, name: String, route: HttpRoute) {
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
