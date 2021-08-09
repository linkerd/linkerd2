use crate::{
    lookup, node::KubeletIps, DefaultAllow, Errors, Index, Namespace, NodeIndex, PodServerTx,
    ServerRx, SrvIndex,
};
use anyhow::{anyhow, Result};
use linkerd_policy_controller_k8s_api::{self as k8s, policy, ResourceExt};
use std::collections::{hash_map::Entry as HashEntry, HashMap, HashSet};
use tokio::sync::watch;
use tracing::{debug, instrument, trace, warn};

#[derive(Debug, Default)]
pub(crate) struct PodIndex {
    index: HashMap<String, Pod>,
}

#[derive(Debug)]
struct Pod {
    ports: PodPorts,
    labels: k8s::Labels,
    default_allow_rx: ServerRx,
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

/// A single pod-port's state.
#[derive(Debug)]
struct Port {
    /// Set with the name of the `Server` resource that currently selects this port, if one exists.
    ///
    /// When this is `None`, a default policy currently applies.
    server_name: Option<String>,

    /// Updated with a server update receiver as servers select/delelect this port.
    server_tx: PodServerTx,
}

// === impl Index ===

impl Index {
    /// Builds a `Pod`, linking it with servers and nodes.
    #[instrument(
        skip(self, pod),
        fields(
            ns = ?pod.metadata.namespace,
            name = %pod.name(),
        )
    )]
    pub(crate) fn apply_pod(&mut self, pod: k8s::Pod) -> Result<()> {
        let Namespace {
            default_allow,
            ref mut pods,
            ref mut servers,
            ..
        } = self
            .namespaces
            .get_or_default(pod.namespace().expect("namespace must be set"));

        let default_allow = *default_allow;
        let allows = self.default_allows.clone();
        let mk_default_allow =
            move |da: Option<DefaultAllow>| allows.get(da.unwrap_or(default_allow));

        pods.apply(
            pod,
            &mut self.nodes,
            servers,
            &mut self.lookups,
            mk_default_allow,
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
        self.nodes.clear_pending_pods();

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
        if self.nodes.clear_pending_pod(ns, pod) {
            // If the pod was pending that it can't be in the main index.
            debug!("Cleared pending pod");
            return Ok(());
        }

        self.namespaces
            .index
            .get_mut(ns)
            .ok_or_else(|| anyhow!("namespace {} doesn't exist", ns))?
            .pods
            .index
            .remove(pod)
            .ok_or_else(|| anyhow!("pod {} doesn't exist", pod))?;

        self.lookups.unset(&ns, &pod)?;

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
            let rx = pod.default_allow_rx.clone();
            for (p, port) in pod.ports.by_port.iter_mut() {
                if port.server_name.as_deref() == Some(name) {
                    debug!(pod = %pod_name, port = %p, "Removing server from pod");
                    port.server_name = None;
                    port.server_tx
                        .send(rx.clone())
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
        nodes: &mut NodeIndex,
        servers: &SrvIndex,
        lookups: &mut lookup::Writer,
        get_default_allow_rx: impl Fn(Option<DefaultAllow>) -> ServerRx,
    ) -> Result<()> {
        let ns_name = pod.namespace().expect("pod must have a namespace");
        let pod_name = pod.name();
        match self.index.entry(pod_name) {
            HashEntry::Vacant(pod_entry) => {
                // Lookup the pod's node's kubelet IP or stop processing the update. If the pod does
                // not yet have a node, it will be ignored. If the node isn't yet in the index, the
                // pod is saved to be processed later.
                let (pod, kubelet) = match nodes.get_kubelet_ips_or_push_pending(pod) {
                    Some((pod, ips)) => (pod, ips),
                    None => {
                        debug!("Pod cannot yet assigned to a Node");
                        return Ok(());
                    }
                };

                let spec = pod.spec.ok_or_else(|| anyhow!("pod missing spec"))?;

                // Check the pod for a default-allow annotation. If it's set, use it; otherwise use
                // the default policy from the namespace or cluster. We retain this value (and not
                // only the policy) so that we can more conveniently de-duplicate changes
                let default_allow_rx = match DefaultAllow::from_annotation(&pod.metadata) {
                    Ok(allow) => get_default_allow_rx(allow),
                    Err(error) => {
                        warn!(%error, "Ignoring invalid default-allow annotation");
                        get_default_allow_rx(None)
                    }
                };

                // Read the pod's ports and extract:
                // - `ServerTx`s to be linkerd against the server index; and
                // - lookup receivers to be returned to API clients.
                let (ports, pod_lookups) =
                    Self::extract_ports(spec, default_allow_rx.clone(), kubelet);

                // Start tracking the pod's metadata so it can be linked against servers as they are
                // created. Immediately link the pod against the server index.
                let mut pod = Pod {
                    default_allow_rx,
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
                if p.labels.as_ref() != &pod.metadata.labels {
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
        server_rx: ServerRx,
        kubelet: KubeletIps,
    ) -> (PodPorts, HashMap<u16, lookup::Rx>) {
        let mut ports = PodPorts::default();
        let mut lookups = HashMap::new();

        for container in spec.containers.into_iter() {
            for p in container.ports.into_iter() {
                if p.protocol.map(|p| p == "TCP").unwrap_or(true) {
                    let port = p.container_port as u16;
                    if ports.by_port.contains_key(&port) {
                        debug!(port, "Port duplicated");
                        continue;
                    }

                    let (server_tx, rx) = watch::channel(server_rx.clone());
                    let pod_port = Port {
                        server_name: None,
                        server_tx,
                    };

                    trace!(%port, name = ?p.name, "Adding port");
                    if let Some(name) = p.name {
                        ports.by_name.entry(name).or_default().push(port);
                    }

                    ports.by_port.insert(port, pod_port);
                    lookups.insert(port, lookup::Rx::new(kubelet.clone(), rx));
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
                .send(self.default_allow_rx.clone())
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
