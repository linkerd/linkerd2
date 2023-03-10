use crate::{
    AuthenticationNsIndex, ClusterInfo, DefaultPolicy, Entry, HashMap, InboundServer, PolicyIndex,
};
use anyhow::{bail, Context, Result};
use linkerd_policy_controller_core::{ProxyProtocol, ServerRef};
use linkerd_policy_controller_k8s_api::{self as k8s, policy::server::Port, ResourceExt};
use std::{collections::BTreeSet, num::NonZeroU16};
use tokio::sync::watch;
use tracing::info_span;

/// Holds pod metadata/config that can change.
#[derive(Debug, PartialEq)]
pub(crate) struct Meta {
    /// The pod's labels. Used by `Server` pod selectors.
    pub labels: k8s::Labels,

    // Pod-specific settings (i.e., derived from annotations).
    pub settings: Settings,
}

/// Per-pod settings, as configured by the pod's annotations.
#[derive(Debug, Default, PartialEq)]
pub(crate) struct Settings {
    pub require_id_ports: PortSet,
    pub opaque_ports: PortSet,
    pub default_policy: Option<DefaultPolicy>,
}

/// Holds all pod data for a single namespace.
#[derive(Debug)]
pub(crate) struct PodIndex {
    pub(crate) namespace: String,
    pub(crate) by_name: HashMap<String, Pod>,
}

/// Holds a single pod's data with the server watches for all known ports.
///
/// The set of ports/servers is updated as clients discover server configuration
/// or as `Server` resources select a port.
#[derive(Debug)]
pub(crate) struct Pod {
    pub(crate) meta: Meta,

    /// The pod's named container ports. Used by `Server` port selectors.
    ///
    /// A pod may have multiple ports with the same name. E.g., each container
    /// may have its own `admin-http` port.
    pub(crate) port_names: HashMap<String, PortSet>,

    /// All known TCP server ports. This may be updated by
    /// `Namespace::reindex`--when a port is selected by a `Server`--or by
    /// `Namespace::get_pod_server` when a client discovers a port that has no
    /// configured server (and i.e. uses the default policy).
    pub(crate) port_servers: PortMap<PodPortServer>,

    /// The pod's probe ports and their respective paths.
    ///
    /// In order for the policy controller to authorize probes, it must be
    /// aware of the probe ports and the expected paths on which probes are
    /// expected.
    pub(crate) probes: PortMap<BTreeSet<String>>,
}

/// Holds the state of a single port on a pod.
#[derive(Debug)]
pub(crate) struct PodPortServer {
    /// The name of the server resource that matches this port. Unset when no
    /// server resources match this pod/port (and, i.e., the default policy is
    /// used).
    name: Option<String>,

    /// A sender used to broadcast pod port server updates.
    pub(crate) watch: watch::Sender<InboundServer>,
}

/// A `HashSet` specialized for ports.
///
/// Because ports are `u16` values, this type avoids the overhead of actually
/// hashing ports.
pub type PortSet = std::collections::HashSet<NonZeroU16, std::hash::BuildHasherDefault<PortHasher>>;

/// A `HashMap` specialized for ports.
///
/// Because ports are `NonZeroU16` values, this type avoids the overhead of
/// actually hashing ports.
pub(crate) type PortMap<V> =
    std::collections::HashMap<NonZeroU16, V, std::hash::BuildHasherDefault<PortHasher>>;

/// A hasher for ports.
///
/// Because ports are single `NonZeroU16` values, we don't have to hash them; we can just use
/// the integer values as hashes directly.
///
/// Borrowed from the proxy.
#[derive(Debug, Default)]
pub struct PortHasher(u16);

impl kubert::index::IndexNamespacedResource<k8s::Pod> for crate::Index {
    fn apply(&mut self, pod: k8s::Pod) {
        let namespace = pod.namespace().unwrap();
        let name = pod.name_unchecked();
        let _span = info_span!("apply", ns = %namespace, %name).entered();

        let port_names = pod.spec.as_ref().map(tcp_ports_by_name).unwrap_or_default();
        let probes = pod.spec.as_ref().map(pod_http_probes).unwrap_or_default();

        let meta = Meta::from_metadata(pod.metadata);

        // Add or update the pod. If the pod was not already present in the
        // index with the same metadata, index it against the policy resources,
        // updating its watches.
        let ns = self.namespaces.get_or_default(namespace);
        match ns.pods.update(name, meta, port_names, probes) {
            Ok(None) => {}
            Ok(Some(pod)) => pod.reindex_servers(&ns.policy, &self.authentications),
            Err(error) => {
                tracing::error!(%error, "Illegal pod update");
            }
        }
    }

    fn delete(&mut self, ns: String, name: String) {
        tracing::debug!(%ns, %name, "delete");
        if let Entry::Occupied(mut ns) = self.namespaces.by_ns.entry(ns) {
            // Once the pod is removed, there's nothing else to update. Any open
            // watches will complete.  No other parts of the index need to be
            // updated.
            if ns.get_mut().pods.by_name.remove(&name).is_some() && ns.get().is_empty() {
                ns.remove();
            }
        }
    }

    // Since apply only reindexes a single pod at a time, there's no need to
    // handle resets specially.
}

// === impl PodIndex ===

impl PodIndex {
    #[inline]
    pub fn is_empty(&self) -> bool {
        self.by_name.is_empty()
    }

    pub(crate) fn update(
        &mut self,
        name: String,
        meta: Meta,
        port_names: HashMap<String, PortSet>,
        probes: PortMap<BTreeSet<String>>,
    ) -> Result<Option<&mut Pod>> {
        let pod = match self.by_name.entry(name.clone()) {
            Entry::Vacant(entry) => entry.insert(Pod {
                meta,
                port_names,
                port_servers: PortMap::default(),
                probes,
            }),

            Entry::Occupied(entry) => {
                let pod = entry.into_mut();

                // Pod labels and annotations may change at runtime, but the
                // port list may not
                if pod.port_names != port_names {
                    bail!("pod {} port names must not change", name);
                }

                // If there aren't meaningful changes, then don't bother doing
                // any more work.
                if pod.meta == meta {
                    tracing::debug!(pod = %name, "No changes");
                    return Ok(None);
                }
                tracing::debug!(pod = %name, "Updating");
                pod.meta = meta;
                pod
            }
        };
        Ok(Some(pod))
    }

    pub(crate) fn reindex(&mut self, policy: &PolicyIndex, authns: &AuthenticationNsIndex) {
        let _span = info_span!("reindex", ns = %self.namespace).entered();
        for (name, pod) in self.by_name.iter_mut() {
            let _span = info_span!("pod", pod = %name).entered();
            pod.reindex_servers(policy, authns);
        }
    }
}

// === impl Pod ===

impl Pod {
    /// Determines the policies for ports on this pod.
    fn reindex_servers(&mut self, policy: &PolicyIndex, authentications: &AuthenticationNsIndex) {
        // Keep track of the ports that are already known in the pod so that, after applying server
        // matches, we can ensure remaining ports are set to the default policy.
        let mut unmatched_ports = self.port_servers.keys().copied().collect::<PortSet>();

        // Keep track of which ports have been matched to servers to that we can detect when
        // multiple servers match a single port.
        //
        // We start with capacity for the known ports on the pod; but this can grow if servers
        // select additional ports.
        let mut matched_ports = PortMap::with_capacity_and_hasher(
            unmatched_ports.len(),
            std::hash::BuildHasherDefault::<PortHasher>::default(),
        );

        for (srvname, server) in policy.servers.iter() {
            if server.pod_selector.matches(&self.meta.labels) {
                for port in self.select_ports(&server.port_ref).into_iter() {
                    // If the port is already matched to a server, then log a warning and skip
                    // updating it so it doesn't flap between servers.
                    if let Some(prior) = matched_ports.get(&port) {
                        tracing::warn!(
                            port = %port,
                            server = %prior,
                            conflict = %srvname,
                            "Port already matched by another server; skipping"
                        );
                        continue;
                    }

                    let s = policy.inbound_server(
                        srvname.clone(),
                        server,
                        authentications,
                        self.probes
                            .get(&port)
                            .into_iter()
                            .flatten()
                            .map(|p| p.as_str()),
                    );
                    self.update_server(port, srvname, s);

                    matched_ports.insert(port, srvname.clone());
                    unmatched_ports.remove(&port);
                }
            }
        }

        // Reset all remaining ports to the default policy.
        for port in unmatched_ports.into_iter() {
            self.set_default_server(port, &policy.cluster_info);
        }
    }

    /// Updates a pod-port to use the given named server.
    ///
    /// The name is used explicity (and not derived from the `server` itself) to
    /// ensure that we're not handling a default server.
    fn update_server(&mut self, port: NonZeroU16, name: &str, server: InboundServer) {
        match self.port_servers.entry(port) {
            Entry::Vacant(entry) => {
                tracing::trace!(port = %port, server = %name, "Creating server");
                let (watch, _) = watch::channel(server);
                entry.insert(PodPortServer {
                    name: Some(name.to_string()),
                    watch,
                });
            }

            Entry::Occupied(mut entry) => {
                let ps = entry.get_mut();

                ps.watch.send_if_modified(|current| {
                    if ps.name.as_deref() == Some(name) && *current == server {
                        tracing::trace!(port = %port, server = %name, "Skipped redundant server update");
                        tracing::trace!(?server);
                        return false;
                    }

                    // If the port's server previously matched a different server,
                    // this can either mean that multiple servers currently match
                    // the pod:port, or that we're in the middle of an update. We
                    // make the opportunistic choice to assume the cluster is
                    // configured coherently so we take the update. The admission
                    // controller should prevent conflicts.
                    tracing::trace!(port = %port, server = %name, "Updating server");
                    if ps.name.as_deref() != Some(name) {
                        ps.name = Some(name.to_string());
                    }

                    *current = server;
                    true
                });
            }
        }

        tracing::debug!(port = %port, server = %name, "Updated server");
    }

    /// Updates a pod-port to use the given named server.
    fn set_default_server(&mut self, port: NonZeroU16, config: &ClusterInfo) {
        let server = Self::default_inbound_server(
            port,
            &self.meta.settings,
            self.probes
                .get(&port)
                .into_iter()
                .flatten()
                .map(|p| p.as_str()),
            config,
        );
        match self.port_servers.entry(port) {
            Entry::Vacant(entry) => {
                tracing::debug!(%port, server = %config.default_policy, "Creating default server");
                let (watch, _) = watch::channel(server);
                entry.insert(PodPortServer { name: None, watch });
            }

            Entry::Occupied(mut entry) => {
                let ps = entry.get_mut();
                ps.watch.send_if_modified(|current| {
                    // Avoid sending redundant updates.
                    if *current == server {
                        tracing::trace!(%port, server = %config.default_policy, "Default server already set");
                        return false;
                    }

                    tracing::debug!(%port, server = %config.default_policy, "Setting default server");
                    ps.name = None;
                    *current = server;
                    true
                });
            }
        }
    }

    /// Enumerates ports.
    ///
    /// A named port may refer to an arbitrary number of port numbers.
    fn select_ports(&mut self, port_ref: &Port) -> Vec<NonZeroU16> {
        match port_ref {
            Port::Number(p) => Some(*p).into_iter().collect(),
            Port::Name(name) => self
                .port_names
                .get(name)
                .into_iter()
                .flatten()
                .cloned()
                .collect(),
        }
    }

    pub(crate) fn port_server_or_default(
        &mut self,
        port: NonZeroU16,
        config: &ClusterInfo,
    ) -> &mut PodPortServer {
        match self.port_servers.entry(port) {
            Entry::Occupied(entry) => entry.into_mut(),
            Entry::Vacant(entry) => {
                let (watch, _) = watch::channel(Self::default_inbound_server(
                    port,
                    &self.meta.settings,
                    self.probes
                        .get(&port)
                        .into_iter()
                        .flatten()
                        .map(|p| p.as_str()),
                    config,
                ));
                entry.insert(PodPortServer { name: None, watch })
            }
        }
    }

    fn default_inbound_server<'p>(
        port: NonZeroU16,
        settings: &Settings,
        probe_paths: impl Iterator<Item = &'p str>,
        config: &ClusterInfo,
    ) -> InboundServer {
        let protocol = if settings.opaque_ports.contains(&port) {
            ProxyProtocol::Opaque
        } else {
            ProxyProtocol::Detect {
                timeout: config.default_detect_timeout,
            }
        };

        let mut policy = settings.default_policy.unwrap_or(config.default_policy);
        if settings.require_id_ports.contains(&port) {
            if let DefaultPolicy::Allow {
                ref mut authenticated_only,
                ..
            } = policy
            {
                *authenticated_only = true;
            }
        }

        let authorizations = policy.default_authzs(config);

        let http_routes = crate::default_inbound_http_routes(config, probe_paths);

        InboundServer {
            reference: ServerRef::Default(policy.as_str()),
            protocol,
            authorizations,
            http_routes,
        }
    }
}

/// Gets the set of named ports with `protocol: TCP` from a pod spec.
pub(crate) fn tcp_ports_by_name(spec: &k8s::PodSpec) -> HashMap<String, PortSet> {
    let mut ports = HashMap::<String, PortSet>::default();
    for (port, name) in spec
        .containers
        .iter()
        .flat_map(|c| c.ports.iter().flatten())
        .filter_map(named_tcp_port)
    {
        ports.entry(name.to_string()).or_default().insert(port);
    }
    ports
}

/// Gets the container probe ports for a Pod.
///
/// The result is a mapping for each probe port exposed by a container in the
/// Pod and the paths for which probes are expected.
pub(crate) fn pod_http_probes(pod: &k8s::PodSpec) -> PortMap<BTreeSet<String>> {
    let mut probes = PortMap::<BTreeSet<String>>::default();
    for (port, path) in pod.containers.iter().flat_map(container_http_probe_paths) {
        probes.entry(port).or_default().insert(path);
    }
    probes
}

fn container_http_probe_paths(
    container: &k8s::Container,
) -> impl Iterator<Item = (NonZeroU16, String)> + '_ {
    fn find_by_name(name: &str, ports: &[k8s::ContainerPort]) -> Option<NonZeroU16> {
        for (p, n) in ports.iter().filter_map(named_tcp_port) {
            if n.eq_ignore_ascii_case(name) {
                return Some(p);
            }
        }
        None
    }

    fn get_port(port: &k8s::IntOrString, container: &k8s::Container) -> Option<NonZeroU16> {
        match port {
            k8s::IntOrString::Int(p) => u16::try_from(*p).ok()?.try_into().ok(),
            k8s::IntOrString::String(n) => find_by_name(n, container.ports.as_ref()?),
        }
    }

    (container.liveness_probe.iter())
        .chain(container.readiness_probe.iter())
        .chain(container.startup_probe.iter())
        .filter_map(|p| {
            let probe = p.http_get.as_ref()?;
            let port = get_port(&probe.port, container)?;
            let path = probe.path.clone().unwrap_or_else(|| "/".to_string());
            Some((port, path))
        })
}

fn named_tcp_port(port: &k8s::ContainerPort) -> Option<(NonZeroU16, &str)> {
    if let Some(ref proto) = port.protocol {
        if !proto.eq_ignore_ascii_case("TCP") {
            return None;
        }
    }
    let p = u16::try_from(port.container_port)
        .and_then(NonZeroU16::try_from)
        .ok()?;
    let n = port.name.as_deref()?;
    Some((p, n))
}

// === impl Meta ===

impl Meta {
    pub(crate) fn from_metadata(meta: k8s::ObjectMeta) -> Self {
        let settings = Settings::from_metadata(&meta);
        tracing::trace!(?settings);
        Self {
            settings,
            labels: meta.labels.into(),
        }
    }
}

// === impl Settings ===

impl Settings {
    /// Reads pod settings from the pod metadata including:
    ///
    /// - Opaque ports
    /// - Ports that require identity
    /// - The pod's default policy
    pub(crate) fn from_metadata(meta: &k8s::ObjectMeta) -> Self {
        let anns = match meta.annotations.as_ref() {
            None => return Self::default(),
            Some(anns) => anns,
        };

        let default_policy = default_policy(anns).unwrap_or_else(|error| {
            tracing::warn!(%error, "invalid default policy annotation value");
            None
        });

        let opaque_ports =
            ports_annotation(anns, "config.linkerd.io/opaque-ports").unwrap_or_default();
        let require_id_ports = ports_annotation(
            anns,
            "config.linkerd.io/proxy-require-identity-inbound-ports",
        )
        .unwrap_or_default();

        Self {
            default_policy,
            opaque_ports,
            require_id_ports,
        }
    }
}

/// Attempts to read a default policy override from an annotation map.
fn default_policy(
    ann: &std::collections::BTreeMap<String, String>,
) -> Result<Option<DefaultPolicy>> {
    if let Some(v) = ann.get("config.linkerd.io/default-inbound-policy") {
        let mode = v.parse()?;
        return Ok(Some(mode));
    }

    Ok(None)
}

/// Reads `annotation` from the provided set of annotations, parsing it as a port set.  If the
/// annotation is not set or is invalid, the empty set is returned.
pub(crate) fn ports_annotation(
    annotations: &std::collections::BTreeMap<String, String>,
    annotation: &str,
) -> Option<PortSet> {
    annotations.get(annotation).map(|spec| {
        parse_portset(spec).unwrap_or_else(|error| {
            tracing::info!(%spec, %error, %annotation, "Invalid ports list");
            Default::default()
        })
    })
}

/// Read a comma-separated of ports or port ranges from the given string.
pub fn parse_portset(s: &str) -> Result<PortSet> {
    let mut ports = PortSet::default();

    for spec in s.split(',') {
        match spec.split_once('-') {
            None => {
                if !spec.trim().is_empty() {
                    let port = spec.trim().parse().context("parsing port")?;
                    ports.insert(port);
                }
            }
            Some((floor, ceil)) => {
                let floor = floor.trim().parse::<NonZeroU16>().context("parsing port")?;
                let ceil = ceil.trim().parse::<NonZeroU16>().context("parsing port")?;
                if floor > ceil {
                    bail!("Port range must be increasing");
                }
                ports.extend(
                    (u16::from(floor)..=u16::from(ceil)).map(|p| NonZeroU16::try_from(p).unwrap()),
                );
            }
        }
    }

    Ok(ports)
}

// === impl PortHasher ===

impl std::hash::Hasher for PortHasher {
    fn write(&mut self, _: &[u8]) {
        unreachable!("hashing a `u16` calls `write_u16`");
    }

    #[inline]
    fn write_u16(&mut self, port: u16) {
        self.0 = port;
    }

    #[inline]
    fn finish(&self) -> u64 {
        self.0 as u64
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use linkerd_policy_controller_k8s_api as k8s;

    macro_rules! ports {
        ($($x:expr),+ $(,)?) => (
            vec![$($x),+]
                .into_iter()
                .map(NonZeroU16::try_from)
                .collect::<Result<PortSet, _>>()
                .unwrap()
        );
    }

    #[test]
    fn parse_portset() {
        use super::parse_portset;

        assert!(parse_portset("").unwrap().is_empty(), "empty");
        assert!(parse_portset("0").is_err(), "0");
        assert_eq!(parse_portset("1").unwrap(), ports![1], "1");
        assert_eq!(parse_portset("1-3").unwrap(), ports![1, 2, 3], "1-2");
        assert_eq!(parse_portset("4,1-2").unwrap(), ports![1, 2, 4], "4,1-2");
        assert!(parse_portset("2-1").is_err(), "2-1");
        assert!(parse_portset("2-").is_err(), "2-");
        assert!(parse_portset("65537").is_err(), "65537");
    }

    #[test]
    fn probe_multiple_paths() {
        let probes = pod_http_probes(&k8s::PodSpec {
            containers: vec![
                k8s::Container {
                    liveness_probe: Some(k8s::Probe {
                        http_get: Some(k8s::HTTPGetAction {
                            path: Some("/liveness-container-1".to_string()),
                            port: k8s::IntOrString::Int(5432),
                            ..Default::default()
                        }),
                        ..Default::default()
                    }),
                    readiness_probe: Some(k8s::Probe {
                        http_get: Some(k8s::HTTPGetAction {
                            path: Some("/ready-container-1".to_string()),
                            port: k8s::IntOrString::Int(5432),
                            ..Default::default()
                        }),
                        ..Default::default()
                    }),
                    ..Default::default()
                },
                k8s::Container {
                    ports: Some(vec![k8s::ContainerPort {
                        name: Some("named-1".to_string()),
                        container_port: 6543,
                        ..Default::default()
                    }]),
                    liveness_probe: Some(k8s::Probe {
                        http_get: Some(k8s::HTTPGetAction {
                            path: Some("/liveness-container-2".to_string()),
                            port: k8s::IntOrString::String("named-1".to_string()),
                            ..Default::default()
                        }),
                        ..Default::default()
                    }),
                    readiness_probe: Some(k8s::Probe {
                        http_get: Some(k8s::HTTPGetAction {
                            path: Some("/ready-container-2".to_string()),
                            port: k8s::IntOrString::Int(6543),
                            ..Default::default()
                        }),
                        ..Default::default()
                    }),
                    ..Default::default()
                },
            ],
            ..Default::default()
        });

        let port_5432 = u16::try_from(5432).and_then(NonZeroU16::try_from).unwrap();
        let mut expected_5432 = BTreeSet::new();
        expected_5432.insert("/liveness-container-1".to_string());
        expected_5432.insert("/ready-container-1".to_string());
        assert!(probes.get(&port_5432).is_some());
        assert_eq!(*probes.get(&port_5432).unwrap(), expected_5432);

        let port_6543 = u16::try_from(6543).and_then(NonZeroU16::try_from).unwrap();
        let mut expected_6543 = BTreeSet::new();
        expected_6543.insert("/liveness-container-2".to_string());
        expected_6543.insert("/ready-container-2".to_string());
        assert!(probes.get(&port_6543).is_some());
        assert_eq!(*probes.get(&port_6543).unwrap(), expected_6543);
    }

    #[test]
    fn probe_ignores_udp() {
        let probes = pod_http_probes(&k8s::PodSpec {
            containers: vec![k8s::Container {
                ports: Some(vec![k8s::ContainerPort {
                    container_port: 6543,
                    name: Some("named".to_string()),
                    protocol: Some("UDP".to_string()),
                    ..Default::default()
                }]),
                liveness_probe: Some(k8s::Probe {
                    http_get: Some(k8s::HTTPGetAction {
                        port: k8s::IntOrString::String("named".to_string()),
                        ..Default::default()
                    }),
                    ..Default::default()
                }),
                ..Default::default()
            }],
            ..Default::default()
        });

        assert!(probes.is_empty());
    }

    #[test]
    fn probe_only_references_within_container() {
        let probes = pod_http_probes(&k8s::PodSpec {
            containers: vec![
                k8s::Container {
                    liveness_probe: Some(k8s::Probe {
                        http_get: Some(k8s::HTTPGetAction {
                            port: k8s::IntOrString::String("named".to_string()),
                            ..Default::default()
                        }),
                        ..Default::default()
                    }),
                    ..Default::default()
                },
                k8s::Container {
                    ports: Some(vec![k8s::ContainerPort {
                        container_port: 6543,
                        name: Some("named".to_string()),
                        protocol: Some("TCP".to_string()),
                        ..Default::default()
                    }]),
                    ..Default::default()
                },
            ],
            ..Default::default()
        });

        assert!(probes.is_empty());
    }

    #[test]
    fn probe_ports_optional() {
        let probes = pod_http_probes(&k8s::PodSpec {
            containers: vec![k8s::Container {
                liveness_probe: Some(k8s::Probe {
                    http_get: Some(k8s::HTTPGetAction {
                        port: k8s::IntOrString::Int(8080),
                        ..Default::default()
                    }),
                    ..Default::default()
                }),
                ..Default::default()
            }],
            ..Default::default()
        });

        assert_eq!(probes.len(), 1);
        let paths = probes.get(&8080.try_into().unwrap()).unwrap();
        assert_eq!(paths.len(), 1);
        assert_eq!(paths.iter().next().unwrap(), "/");
    }
}
