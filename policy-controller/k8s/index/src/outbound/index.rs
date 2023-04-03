use crate::{
    http_route,
    ports::{ports_annotation, PortSet},
    ClusterInfo,
};
use ahash::AHashMap as HashMap;
use anyhow::{bail, Result};
use k8s_gateway_api::{BackendObjectReference, HttpBackendRef, ParentReference};
use linkerd_policy_controller_core::outbound::{
    Backend, Backoff, FailureAccrual, HttpRoute, HttpRouteRule, OutboundPolicy, WeightedService,
};
use linkerd_policy_controller_k8s_api::{policy as api, ResourceExt, Service, Time};
use parking_lot::RwLock;
use std::{net::IpAddr, num::NonZeroU16, sync::Arc, time};
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
    accrual: Option<FailureAccrual>,
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
    accrual: Option<FailureAccrual>,
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
        let accrual = parse_accrual_config(service.annotations())
            .map_err(|error| tracing::error!(%error, service=name, namespace=ns, "failed to parse accrual config"))
            .unwrap_or_default();
        let opaque_ports =
            ports_annotation(service.annotations(), "config.linkerd.io/opaque-ports")
                .unwrap_or_else(|| self.namespaces.cluster_info.default_opaque_ports.clone());

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

        let service_info = ServiceInfo {
            opaque_ports,
            accrual,
        };

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
                tracing::error!(%error, "failed to convert HttpRoute");
                return;
            }
        };
        tracing::debug!(?outbound_route);

        for parent_ref in route.spec.inner.parent_refs.iter().flatten() {
            if !is_parent_service(parent_ref) {
                continue;
            }
            if !route_accepted_by_service(&route, &parent_ref.name) {
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
            if svc_port.service != name {
                continue;
            }
            let opaque = service.opaque_ports.contains(&svc_port.port);

            svc_routes.update_service(opaque, service.accrual);
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
            let (opaque, accrual) = match self.services.get(&sp.service) {
                Some(svc) => (svc.opaque_ports.contains(&sp.port), svc.accrual),
                None => (false, None),
            };

            let (sender, _) = watch::channel(OutboundPolicy {
                http_routes: Default::default(),
                authority,
                namespace: self.namespace.to_string(),
                opaque,
                accrual,
            });
            ServiceRoutes {
                routes: Default::default(),
                watch: sender,
                opaque,
                accrual,
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
fn route_accepted_by_service(route: &api::HttpRoute, service: &str) -> bool {
    route
        .status
        .as_ref()
        .map(|status| status.inner.parents.as_slice())
        .unwrap_or_default()
        .iter()
        .any(|parent_status| {
            parent_status.parent_ref.name == service
                && parent_status
                    .conditions
                    .iter()
                    .any(|condition| condition.type_ == "Accepted" && condition.status == "True")
        })
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

    fn update_service(&mut self, opaque: bool, accrual: Option<FailureAccrual>) {
        self.opaque = opaque;
        self.accrual = accrual;
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
            if self.accrual != policy.accrual {
                policy.accrual = self.accrual;
                modified = true;
            }
            modified
        });
    }
}

fn parse_accrual_config(
    annotations: &std::collections::BTreeMap<String, String>,
) -> Result<Option<FailureAccrual>> {
    annotations
        .get("balancer.linkerd.io/failure-accrual")
        .map(|mode| {
            if mode == "consecutive" {
                let max_failures = annotations
                    .get("balancer.linkerd.io/failure-accrual-consecutive-max-failures")
                    .map(|s| s.parse::<u32>())
                    .transpose()?
                    .unwrap_or(7);

                let max_penalty = annotations
                    .get("balancer.linkerd.io/failure-accrual-consecutive-max-penalty")
                    .map(|s| parse_duration(s))
                    .transpose()?
                    .unwrap_or_else(|| time::Duration::from_secs(60));

                let min_penalty = annotations
                    .get("balancer.linkerd.io/failure-accrual-consecutive-min-penalty")
                    .map(|s| parse_duration(s))
                    .transpose()?
                    .unwrap_or_else(|| time::Duration::from_secs(1));
                let jitter = annotations
                    .get("balancer.linkerd.io/failure-accrual-consecutive-jitter-ratio")
                    .map(|s| s.parse::<f32>())
                    .transpose()?
                    .unwrap_or(0.5);
                if min_penalty > max_penalty {
                    bail!(
                        "min_penalty ({min_penalty:?}) cannot exceed max_penalty ({max_penalty:?})",
                    );
                }

                Ok(FailureAccrual::Consecutive {
                    max_failures,
                    backoff: Backoff {
                        min_penalty,
                        max_penalty,
                        jitter,
                    },
                })
            } else {
                bail!("unsupported failure accrual mode: {mode}");
            }
        })
        .transpose()
}

//TODO: check what we do in proxy for this.
fn parse_duration(s: &str) -> Result<time::Duration> {
    let s = s.trim();
    let offset = s
        .rfind(|c: char| c.is_ascii_digit())
        .ok_or_else(|| anyhow::anyhow!("{} does not contain a timeout duration value", s))?;
    let (magnitude, unit) = s.split_at(offset + 1);
    let magnitude = magnitude.parse::<u64>()?;

    let mul = match unit {
        "" if magnitude == 0 => 0,
        "ms" => 1,
        "s" => 1000,
        "m" => 1000 * 60,
        "h" => 1000 * 60 * 60,
        "d" => 1000 * 60 * 60 * 24,
        _ => anyhow::bail!(
            "invalid duration unit {} (expected one of 'ms', 's', 'm', 'h', or 'd')",
            unit
        ),
    };

    let ms = magnitude
        .checked_mul(mul)
        .ok_or_else(|| anyhow::anyhow!("Timeout value {} overflows when converted to 'ms'", s))?;
    Ok(time::Duration::from_millis(ms))
}
