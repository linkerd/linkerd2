use crate::{
    defaults::PortDefaults, lookup, DefaultPolicy, DefaultPolicyWatches, Errors, Index, Namespace,
    PodServerTx, ServerRx, SrvIndex,
};
use anyhow::{anyhow, bail, Context, Result};
use linkerd_policy_controller_k8s_api::{self as k8s, policy, ResourceExt};
use std::collections::{hash_map::Entry as HashEntry, HashMap, HashSet};
use tokio::sync::watch;
use tracing::{debug, instrument, trace};

/// Indexes pod state (within a namespace).
#[derive(Debug, Default)]
pub(crate) struct PodIndex {
    index: HashMap<String, Pod>,
}

/// Holds the state of an individual pod.
#[derive(Debug)]
struct Pod {
    /// An index of all ports in the pod spec.
    ports: PodPorts,

    /// The pod's labels.
    labels: k8s::Labels,

    /// The workload's default allow behavior (to apply when no `Server` references a port).
    default_policy: DefaultPolicy,
}

/// An index of all ports in a pod spec to the
#[derive(Debug, Default)]
struct PodPorts {
    /// All ports in the pod spec, by number.
    by_port: HashMap<u16, Port>,

    /// Enumerates named ports to their numeric equivalents to support by-name lookups.
    ///
    /// A name _usually_ maps to a single container port, however Kubernetes permits multiple
    /// container ports to use the same name. This may be useful to have multiple ports named, e.g.
    /// "admin-http", that have uniform policy requirements.
    by_name: HashMap<String, Vec<u16>>,
}

#[derive(Debug, Default)]
struct PodAnnotations {
    require_id: HashSet<u16>,
    opaque: HashSet<u16>,
}

/// A single pod-port's state.
#[derive(Debug)]
struct Port {
    default_policy_rx: ServerRx,

    /// Set with the name of the `Server` resource that currently selects this port, if one exists.
    ///
    /// When this is `None`, a default policy currently applies.
    server_name: Option<String>,

    /// Updated with a server update receiver as servers select/deselect this port.
    server_tx: PodServerTx,
}

// === impl Index ===

impl Index {
    /// Creates or updates a `Pod`, linking it with servers.
    #[instrument(
        skip(self, pod),
        fields(
            ns = ?pod.metadata.namespace,
            name = %pod.name(),
        )
    )]
    pub(crate) fn apply_pod(&mut self, pod: k8s::Pod) -> Result<()> {
        let Namespace {
            default_policy,
            ref mut pods,
            ref mut servers,
            ..
        } = self
            .namespaces
            .get_or_default(pod.namespace().expect("namespace must be set"));

        pods.apply(
            pod,
            servers,
            &mut self.lookups,
            *default_policy,
            &mut self.default_policy_watches,
        )
    }

    #[instrument(
        skip(self, pod),
        fields(
            ns = ?pod.metadata.namespace,
            name = %pod.name(),
        )
    )]
    pub(crate) fn delete_pod(&mut self, pod: k8s::Pod) -> Result<()> {
        let ns_name = pod.namespace().expect("namespace must be set");
        let pod_name = pod.name();
        self.rm_pod(ns_name.as_str(), pod_name.as_str())
    }

    #[instrument(skip(self, pods))]
    pub(crate) fn reset_pods(&mut self, pods: Vec<k8s::Pod>) -> Result<()> {
        let mut prior_pods = self
            .namespaces
            .iter()
            .map(|(name, ns)| {
                let pods = ns.pods.index.keys().cloned().collect::<HashSet<_>>();
                (name.clone(), pods)
            })
            .collect::<HashMap<_, _>>();

        let mut errors = vec![];
        for pod in pods.into_iter() {
            let ns_name = pod.namespace().unwrap();
            if let Some(ns) = prior_pods.get_mut(ns_name.as_str()) {
                ns.remove(pod.name().as_str());
            }

            if let Err(error) = self.apply_pod(pod) {
                errors.push(error);
            }
        }

        for (ns, pods) in prior_pods.into_iter() {
            for pod in pods.into_iter() {
                if let Err(error) = self.rm_pod(ns.as_str(), pod.as_str()) {
                    errors.push(error);
                }
            }
        }

        Errors::ok_if_empty(errors)
    }

    fn rm_pod(&mut self, ns: &str, pod: &str) -> Result<()> {
        self.namespaces
            .index
            .get_mut(ns)
            .ok_or_else(|| anyhow!("namespace {} doesn't exist", ns))?
            .pods
            .index
            .remove(pod)
            .ok_or_else(|| anyhow!("pod {} doesn't exist", pod))?;

        self.lookups.unset(ns, pod)?;

        debug!("Removed pod");

        Ok(())
    }
}

// === impl PodIndex ===

impl PodIndex {
    pub(crate) fn link_servers(&mut self, servers: &SrvIndex) {
        for pod in self.index.values_mut() {
            pod.link_servers(servers)
        }
    }

    pub(crate) fn reset_server(&mut self, name: &str) {
        for (pod_name, pod) in self.index.iter_mut() {
            for (p, port) in pod.ports.by_port.iter_mut() {
                if port.server_name.as_deref() == Some(name) {
                    debug!(pod = %pod_name, port = %p, "Removing server from pod");
                    port.server_name = None;
                    port.server_tx
                        .send(port.default_policy_rx.clone())
                        .expect("pod config receiver must still be held");
                } else {
                    trace!(pod = %pod_name, port = %p, server = ?port.server_name, "Server does not match");
                }
            }
        }
    }

    /// Processes a pod update.
    fn apply(
        &mut self,
        pod: k8s::Pod,
        servers: &SrvIndex,
        lookups: &mut lookup::Writer,
        default_policy: DefaultPolicy,
        default_policy_watches: &mut DefaultPolicyWatches,
    ) -> Result<()> {
        let ns_name = pod.namespace().expect("pod must have a namespace");
        let pod_name = pod.name();
        match self.index.entry(pod_name) {
            HashEntry::Vacant(pod_entry) => {
                let spec = pod.spec.ok_or_else(|| anyhow!("pod missing spec"))?;

                // Check the pod for a default-allow annotation. If it's set, use it; otherwise use
                // the default policy from the namespace or cluster. We retain this value (and not
                // only the policy) so that we can more conveniently de-duplicate changes
                let default_policy = match DefaultPolicy::from_annotation(&pod.metadata) {
                    Ok(allow) => allow.unwrap_or(default_policy),
                    Err(error) => {
                        tracing::info!(%error, "failed to parse default-allow annotation");
                        default_policy
                    }
                };

                let pod_annotations = PodAnnotations::from_annotations(&pod.metadata);

                // Read the pod's ports and extract:
                // - `ServerTx`s to be linkerd against the server index; and
                // - lookup receivers to be returned to API clients.
                let (ports, pod_lookups) = Self::extract_ports(
                    spec,
                    &pod_annotations,
                    default_policy,
                    default_policy_watches,
                );

                // Start tracking the pod's metadata so it can be linked against servers as they are
                // created. Immediately link the pod's ports to the server index.
                let mut pod = Pod {
                    default_policy,
                    labels: pod.metadata.labels.into(),
                    ports,
                };
                pod.link_servers(servers);

                // The pod has been linked against servers and is registered for subsequent updates,
                // so make it discoverable to API clients.
                lookups
                    .set(ns_name, pod_entry.key(), pod_lookups)
                    .expect("pod must not already exist");

                pod_entry.insert(pod);

                Ok(())
            }

            HashEntry::Occupied(mut entry) => {
                debug_assert!(
                    lookups.contains(&ns_name, entry.key()),
                    "pod must exist in lookups"
                );

                // Labels can be updated at runtime (even though that's kind of weird). If the
                // labels have changed, then we relink servers to pods in case label selections have
                // changed.
                let p = entry.get_mut();
                if p.labels != pod.metadata.labels {
                    p.labels = pod.metadata.labels.into();
                    p.link_servers(servers);
                }

                // Note that the default-allow annotation may not be changed at runtime.
                Ok(())
            }
        }
    }

    /// Extracts port information from a pod spec.
    fn extract_ports(
        spec: k8s::PodSpec,
        annotations: &PodAnnotations,
        default_policy: DefaultPolicy,
        default_policy_watches: &mut DefaultPolicyWatches,
    ) -> (PodPorts, HashMap<u16, lookup::Rx>) {
        let mut ports = PodPorts::default();
        let mut lookups = HashMap::new();

        for container in spec.containers.into_iter() {
            for p in container.ports.into_iter().flatten() {
                if p.protocol.map(|p| p == "TCP").unwrap_or(true) {
                    let port = p.container_port as u16;
                    if ports.by_port.contains_key(&port) {
                        debug!(port, "Port duplicated");
                        continue;
                    }

                    let config = annotations.get_config(port);
                    let default_policy_rx = default_policy_watches.watch(default_policy, config);
                    let (server_tx, rx) = watch::channel(default_policy_rx.clone());
                    let pod_port = Port {
                        default_policy_rx,
                        server_name: None,
                        server_tx,
                    };

                    trace!(%port, name = ?p.name, "Adding port");
                    if let Some(name) = p.name {
                        ports.by_name.entry(name).or_default().push(port);
                    }

                    ports.by_port.insert(port, pod_port);
                    lookups.insert(port, lookup::Rx::new(rx));
                }
            }
        }

        (ports, lookups)
    }
}

// === impl Pod ===

impl Pod {
    /// Links this pod to servers (by label selector).
    ///
    ///
    fn link_servers(&mut self, servers: &SrvIndex) {
        let mut remaining_ports = self.ports.by_port.keys().copied().collect::<HashSet<u16>>();

        // Get all servers that match this pod.
        let matching = servers.iter_matching_pod(self.labels.clone());
        for (name, port_match, rx) in matching {
            // Get all pod ports that match this server.
            for p in self.ports.collect_port(port_match).into_iter() {
                self.link_server_port(p, name, rx);
                remaining_ports.remove(&p);
            }
        }

        // Iterate through the ports that have not been matched to clear them.
        for p in remaining_ports.into_iter() {
            let port = self.ports.by_port.get_mut(&p).unwrap();
            port.server_name = None;
            port.server_tx
                .send(port.default_policy_rx.clone())
                .expect("pod config receiver must still be held");
        }
    }

    fn link_server_port(&mut self, port: u16, name: &str, rx: &ServerRx) {
        let port = match self.ports.by_port.get_mut(&port) {
            Some(p) => p,
            None => return,
        };

        // Either this port is using a default allow policy, and the server name is unset, or
        // multiple servers select this pod. If there's a conflict, we panic if the controller is
        // running in debug mode. In release mode, we log a warning and ignore the conflicting
        // server.
        //
        // TODO these cases should be prevented with a validating admission controller.
        if let Some(sn) = port.server_name.as_ref() {
            if sn != name {
                debug_assert!(false, "Pod port must not match multiple servers");
                tracing::warn!("Pod port matches multiple servers: {} and {}", sn, name);
            }
            // If the name matched there's no use in proceeding with a redundant update
            return;
        }
        port.server_name = Some(name.to_string());

        port.server_tx
            .send(rx.clone())
            .expect("pod config receiver must be set");
        debug!(server = %name, "Pod server updated");
    }
}

// === impl PodAnnotations ===

impl PodAnnotations {
    const OPAQUE_PORTS_ANNOTATION: &'static str = "config.linkerd.io/opaque-ports";
    const REQUIRE_IDENTITY_PORTS_ANNOTATION: &'static str =
        "config.linkerd.io/proxy-require-identity-inbound-ports";

    fn from_annotations(meta: &k8s::ObjectMeta) -> Self {
        let anns = match meta.annotations.as_ref() {
            None => return Self::default(),
            Some(anns) => anns,
        };

        let opaque = Self::ports_annotation(anns, Self::OPAQUE_PORTS_ANNOTATION);
        let require_id = Self::ports_annotation(anns, Self::REQUIRE_IDENTITY_PORTS_ANNOTATION);

        Self { opaque, require_id }
    }

    /// Reads `annotation` from the provided set of annotations, parsing it as a port set.  If the
    /// annotation is not set or is invalid, the empty set is returned.
    fn ports_annotation(
        annotations: &std::collections::BTreeMap<String, String>,
        annotation: &str,
    ) -> HashSet<u16> {
        annotations
            .get(annotation)
            .map(|spec| {
                Self::parse_portset(spec).unwrap_or_else(|error| {
                    tracing::info!(%spec, %error, %annotation, "Invalid ports list");
                    Default::default()
                })
            })
            .unwrap_or_default()
    }

    /// Read a comma-separated of ports or port ranges from the given string.
    fn parse_portset(s: &str) -> Result<HashSet<u16>> {
        let mut ports = HashSet::new();
        for spec in s.split(',') {
            match spec.split_once('-') {
                None => {
                    if !spec.trim().is_empty() {
                        let port = spec.trim().parse().context("parsing port")?;
                        if port == 0 {
                            bail!("port must not be 0")
                        }
                        ports.insert(port);
                    }
                }
                Some((floor, ceil)) => {
                    let floor = floor.trim().parse::<u16>().context("parsing port")?;
                    let ceil = ceil.trim().parse::<u16>().context("parsing port")?;
                    if floor == 0 {
                        bail!("port must not be 0")
                    }
                    if floor > ceil {
                        bail!("Port range must be increasing");
                    }
                    ports.extend(floor..=ceil);
                }
            }
        }
        Ok(ports)
    }

    fn get_config(&self, port: u16) -> PortDefaults {
        PortDefaults {
            authenticated: self.require_id.contains(&port),
            opaque: self.opaque.contains(&port),
        }
    }
}

// === impl PodPorts ===

impl PodPorts {
    /// Finds all ports on this pod that match a server's port reference.
    ///
    /// Numeric port matches will only return a single server, generally, while named port
    /// references may select an arbitrary number of server ports.
    fn collect_port(&self, port_match: &policy::server::Port) -> Vec<u16> {
        match port_match {
            policy::server::Port::Number(ref port) => vec![*port],
            policy::server::Port::Name(ref name) => {
                self.by_name.get(name).cloned().unwrap_or_default()
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::PodAnnotations;

    #[test]
    fn parse_portset() {
        assert!(
            PodAnnotations::parse_portset("").unwrap().is_empty(),
            "empty"
        );
        assert!(PodAnnotations::parse_portset("0").is_err(), "0");
        assert_eq!(
            PodAnnotations::parse_portset("1").unwrap(),
            vec![1].into_iter().collect(),
            "1"
        );
        assert_eq!(
            PodAnnotations::parse_portset("1-2").unwrap(),
            vec![1, 2].into_iter().collect(),
            "1-2"
        );
        assert_eq!(
            PodAnnotations::parse_portset("4,1-2").unwrap(),
            vec![1, 2, 4].into_iter().collect(),
            "4,1-2"
        );
        assert!(PodAnnotations::parse_portset("2-1").is_err(), "2-1");
        assert!(PodAnnotations::parse_portset("2-").is_err(), "2-");
        assert!(PodAnnotations::parse_portset("65537").is_err(), "65537");
    }
}
