use crate::{
    http_route::{self, gkn_for_gateway_http_route, gkn_for_linkerd_http_route, HttpRouteResource},
    ports::{ports_annotation, PortSet},
    ClusterInfo,
};
use ahash::AHashMap as HashMap;
use anyhow::{bail, ensure, Result};
use k8s_gateway_api::{BackendObjectReference, HttpBackendRef, ParentReference};
use linkerd_policy_controller_core::{
    http_route::GroupKindName,
    outbound::{
        Backend, Backoff, FailureAccrual, Filter, HttpRoute, HttpRouteRule, OutboundPolicy,
        WeightedService,
    },
};
use linkerd_policy_controller_k8s_api::{policy as api, ResourceExt, Service, Time};
use parking_lot::RwLock;
use std::{hash::Hash, net::IpAddr, num::NonZeroU16, sync::Arc, time};
use tokio::sync::watch;

#[derive(Debug)]
pub struct Index {
    namespaces: NamespaceIndex,
    services_by_ip: HashMap<IpAddr, ServiceRef>,
    service_info: HashMap<ServiceRef, ServiceInfo>,
}

pub type SharedIndex = Arc<RwLock<Index>>;

#[derive(Debug, Clone, Hash, PartialEq, Eq)]
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
    routes: HashMap<GroupKindName, HttpRoute>,
    watch: watch::Sender<OutboundPolicy>,
    opaque: bool,
    accrual: Option<FailureAccrual>,
}

impl kubert::index::IndexNamespacedResource<api::HttpRoute> for Index {
    fn apply(&mut self, route: api::HttpRoute) {
        self.apply(HttpRouteResource::Linkerd(route))
    }

    fn delete(&mut self, namespace: String, name: String) {
        if let Some(ns_index) = self.namespaces.by_ns.get_mut(&namespace) {
            let gkn = gkn_for_linkerd_http_route(name);
            ns_index.delete(gkn);
        }
    }
}

impl kubert::index::IndexNamespacedResource<k8s_gateway_api::HttpRoute> for Index {
    fn apply(&mut self, route: k8s_gateway_api::HttpRoute) {
        self.apply(HttpRouteResource::Gateway(route))
    }

    fn delete(&mut self, namespace: String, name: String) {
        if let Some(ns_index) = self.namespaces.by_ns.get_mut(&namespace) {
            let gkn = gkn_for_gateway_http_route(name);
            ns_index.delete(gkn);
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
                    self.services_by_ip.insert(addr, service_ref);
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
            })
            .update_service(service.name_unchecked(), &service_info);

        self.service_info.insert(
            ServiceRef {
                name: service.name_unchecked(),
                namespace: service.namespace().expect("Service must have Namespace"),
            },
            service_info,
        );
    }

    fn delete(&mut self, namespace: String, name: String) {
        let service_ref = ServiceRef { name, namespace };
        self.service_info.remove(&service_ref);
        self.services_by_ip.retain(|_, v| *v != service_ref);
    }
}

impl Index {
    pub fn shared(cluster_info: Arc<ClusterInfo>) -> SharedIndex {
        Arc::new(RwLock::new(Self {
            namespaces: NamespaceIndex {
                by_ns: HashMap::default(),
                cluster_info,
            },
            services_by_ip: HashMap::default(),
            service_info: HashMap::default(),
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
            });
        let key = ServicePort { service, port };
        tracing::debug!(?key, "subscribing to service port");
        let routes =
            ns.service_routes_or_default(key, &self.namespaces.cluster_info, &self.service_info);
        Ok(routes.watch.subscribe())
    }

    pub fn lookup_service(&self, addr: IpAddr) -> Option<ServiceRef> {
        self.services_by_ip.get(&addr).cloned()
    }

    fn apply(&mut self, route: HttpRouteResource) {
        tracing::debug!(name = route.name(), "indexing route");
        let ns = route.namespace();
        self.namespaces
            .by_ns
            .entry(ns.clone())
            .or_insert_with(|| Namespace {
                service_routes: Default::default(),
                namespace: Arc::new(ns),
            })
            .apply(route, &self.namespaces.cluster_info, &self.service_info);
    }
}

impl Namespace {
    fn apply(
        &mut self,
        route: HttpRouteResource,
        cluster_info: &ClusterInfo,
        service_info: &HashMap<ServiceRef, ServiceInfo>,
    ) {
        tracing::debug!(?route);
        let outbound_route = match self.convert_route(route.clone(), cluster_info, service_info) {
            Ok(route) => route,
            Err(error) => {
                tracing::error!(%error, "failed to convert HttpRoute");
                return;
            }
        };
        tracing::debug!(?outbound_route);

        for parent_ref in route.inner().parent_refs.iter().flatten() {
            if !is_parent_service(parent_ref) {
                continue;
            }
            if !route_accepted_by_service(route.status(), &parent_ref.name) {
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
                        route = route.name(),
                        "inserting route for service"
                    );
                    let service_routes =
                        self.service_routes_or_default(service_port, cluster_info, service_info);
                    service_routes.apply(route.gkn(), outbound_route.clone());
                } else {
                    tracing::warn!(?parent_ref, "ignoring parent_ref with port 0");
                }
            } else {
                tracing::warn!(?parent_ref, "ignoring parent_ref without port");
            }
        }
    }

    fn update_service(&mut self, name: String, service: &ServiceInfo) {
        tracing::debug!(?name, ?service, "updating service");
        for (svc_port, svc_routes) in self.service_routes.iter_mut() {
            if svc_port.service != name {
                continue;
            }
            let opaque = service.opaque_ports.contains(&svc_port.port);

            svc_routes.update_service(opaque, service.accrual);
        }
    }

    fn delete(&mut self, gkn: GroupKindName) {
        for service in self.service_routes.values_mut() {
            service.delete(&gkn);
        }
    }

    fn service_routes_or_default(
        &mut self,
        sp: ServicePort,
        cluster: &ClusterInfo,
        service_info: &HashMap<ServiceRef, ServiceInfo>,
    ) -> &mut ServiceRoutes {
        self.service_routes.entry(sp.clone()).or_insert_with(|| {
            let authority = cluster.service_dns_authority(&self.namespace, &sp.service, sp.port);
            let service_ref = ServiceRef {
                name: sp.service.clone(),
                namespace: self.namespace.to_string(),
            };
            let (opaque, accrual) = match service_info.get(&service_ref) {
                Some(svc) => (svc.opaque_ports.contains(&sp.port), svc.accrual),
                None => (false, None),
            };

            let (sender, _) = watch::channel(OutboundPolicy {
                http_routes: Default::default(),
                authority,
                name: sp.service.clone(),
                namespace: self.namespace.to_string(),
                port: sp.port,
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

    fn convert_route(
        &self,
        route: HttpRouteResource,
        cluster: &ClusterInfo,
        service_info: &HashMap<ServiceRef, ServiceInfo>,
    ) -> Result<HttpRoute> {
        match route {
            HttpRouteResource::Linkerd(route) => {
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
                    .map(|r| self.convert_linkerd_rule(r, cluster, service_info))
                    .collect::<Result<_>>()?;

                let creation_timestamp = route.metadata.creation_timestamp.map(|Time(t)| t);

                Ok(HttpRoute {
                    hostnames,
                    rules,
                    creation_timestamp,
                })
            }
            HttpRouteResource::Gateway(route) => {
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
                    .map(|r| self.convert_gateway_rule(r, cluster, service_info))
                    .collect::<Result<_>>()?;

                let creation_timestamp = route.metadata.creation_timestamp.map(|Time(t)| t);

                Ok(HttpRoute {
                    hostnames,
                    rules,
                    creation_timestamp,
                })
            }
        }
    }

    fn convert_linkerd_rule(
        &self,
        rule: api::httproute::HttpRouteRule,
        cluster: &ClusterInfo,
        service_info: &HashMap<ServiceRef, ServiceInfo>,
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
            .filter_map(|b| convert_backend(&self.namespace, b, cluster, service_info))
            .collect();

        let filters = rule
            .filters
            .into_iter()
            .flatten()
            .map(convert_linkerd_filter)
            .collect::<Result<_>>()?;

        let request_timeout = rule.timeouts.as_ref().and_then(|timeouts| {
            let timeout = time::Duration::from(timeouts.request?);

            // zero means "no timeout", per GEP-1742
            if timeout == time::Duration::from_nanos(0) {
                return None;
            }

            Some(timeout)
        });

        let backend_request_timeout =
            rule.timeouts
                .as_ref()
                .and_then(|timeouts: &api::httproute::HttpRouteTimeouts| {
                    let timeout = time::Duration::from(timeouts.backend_request?);

                    // zero means "no timeout", per GEP-1742
                    if timeout == time::Duration::from_nanos(0) {
                        return None;
                    }

                    Some(timeout)
                });

        Ok(HttpRouteRule {
            matches,
            backends,
            request_timeout,
            backend_request_timeout,
            filters,
        })
    }

    fn convert_gateway_rule(
        &self,
        rule: k8s_gateway_api::HttpRouteRule,
        cluster: &ClusterInfo,
        service_info: &HashMap<ServiceRef, ServiceInfo>,
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
            .filter_map(|b| convert_backend(&self.namespace, b, cluster, service_info))
            .collect();

        let filters = rule
            .filters
            .into_iter()
            .flatten()
            .map(convert_gateway_filter)
            .collect::<Result<_>>()?;

        Ok(HttpRouteRule {
            matches,
            backends,
            request_timeout: None,
            backend_request_timeout: None,
            filters,
        })
    }
}

fn convert_backend(
    ns: &str,
    backend: HttpBackendRef,
    cluster: &ClusterInfo,
    services: &HashMap<ServiceRef, ServiceInfo>,
) -> Option<Backend> {
    let filters = backend.filters;
    let backend = backend.backend_ref?;
    if !is_backend_service(&backend.inner) {
        return Some(Backend::Invalid {
            weight: backend.weight.unwrap_or(1).into(),
            message: format!(
                "unsupported backend type {group} {kind}",
                group = backend.inner.group.as_deref().unwrap_or("core"),
                kind = backend.inner.kind.as_deref().unwrap_or("<empty>"),
            ),
        });
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
            return Some(Backend::Invalid {
                weight: weight.into(),
                message: format!("missing port for backend Service {name}"),
            })
        }
    };
    let service_ref = ServiceRef {
        name: name.clone(),
        namespace: backend.inner.namespace.unwrap_or_else(|| ns.to_string()),
    };
    if !services.contains_key(&service_ref) {
        return Some(Backend::Invalid {
            weight: weight.into(),
            message: format!("Service not found {name}"),
        });
    }

    let filters = match filters
        .into_iter()
        .flatten()
        .map(convert_gateway_filter)
        .collect::<Result<_>>()
    {
        Ok(filters) => filters,
        Err(error) => {
            return Some(Backend::Invalid {
                weight: backend.weight.unwrap_or(1).into(),
                message: format!("unsupported backend filter: {error}", error = error),
            });
        }
    };

    Some(Backend::Service(WeightedService {
        weight: weight.into(),
        authority: cluster.service_dns_authority(&service_ref.namespace, &name, port),
        name,
        namespace: ns.to_string(),
        port,
        filters,
    }))
}

fn convert_linkerd_filter(filter: api::httproute::HttpRouteFilter) -> Result<Filter> {
    let filter = match filter {
        api::httproute::HttpRouteFilter::RequestHeaderModifier {
            request_header_modifier,
        } => {
            let filter = http_route::req_header_modifier(request_header_modifier)?;
            Filter::RequestHeaderModifier(filter)
        }

        api::httproute::HttpRouteFilter::RequestRedirect { request_redirect } => {
            let filter = http_route::req_redirect(request_redirect)?;
            Filter::RequestRedirect(filter)
        }
    };
    Ok(filter)
}

fn convert_gateway_filter(filter: k8s_gateway_api::HttpRouteFilter) -> Result<Filter> {
    let filter = match filter {
        k8s_gateway_api::HttpRouteFilter::RequestHeaderModifier {
            request_header_modifier,
        } => {
            let filter = http_route::req_header_modifier(request_header_modifier)?;
            Filter::RequestHeaderModifier(filter)
        }

        k8s_gateway_api::HttpRouteFilter::RequestRedirect { request_redirect } => {
            let filter = http_route::req_redirect(request_redirect)?;
            Filter::RequestRedirect(filter)
        }
        k8s_gateway_api::HttpRouteFilter::RequestMirror { .. } => {
            bail!("RequestMirror filter is not supported")
        }
        k8s_gateway_api::HttpRouteFilter::URLRewrite { .. } => {
            bail!("URLRewrite filter is not supported")
        }
        k8s_gateway_api::HttpRouteFilter::ExtensionRef { .. } => {
            bail!("ExtensionRef filter is not supported")
        }
    };
    Ok(filter)
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
fn route_accepted_by_service(
    route_status: Option<&k8s_gateway_api::RouteStatus>,
    service: &str,
) -> bool {
    route_status
        .as_ref()
        .map(|status| status.parents.as_slice())
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
    fn apply(&mut self, gkn: GroupKindName, route: HttpRoute) {
        self.routes.insert(gkn, route);
        self.send_if_modified();
    }

    fn update_service(&mut self, opaque: bool, accrual: Option<FailureAccrual>) {
        self.opaque = opaque;
        self.accrual = accrual;
        self.send_if_modified();
    }

    fn delete(&mut self, gkn: &GroupKindName) {
        self.routes.remove(gkn);
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
                ensure!(
                    min_penalty <= max_penalty,
                    "min_penalty ({min_penalty:?}) cannot exceed max_penalty ({max_penalty:?})"
                );
                ensure!(
                    max_penalty > time::Duration::from_millis(0),
                    "max_penalty cannot be zero"
                );
                ensure!(jitter >= 0.0, "jitter cannot be negative");
                ensure!(jitter <= 100.0, "jitter cannot be greater than 100");

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
