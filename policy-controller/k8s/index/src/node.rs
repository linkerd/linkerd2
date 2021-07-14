//! Node->Kubelet IP

use crate::Index;
use anyhow::{anyhow, Context, Result};
use linkerd_policy_controller_core::IpNet;
use linkerd_policy_controller_k8s_api::{self as k8s, ResourceExt};
use std::{
    collections::{hash_map::Entry as HashEntry, HashMap, HashSet},
    net::IpAddr,
    sync::Arc,
};
use tracing::{debug, instrument, trace, warn};

#[derive(Debug, Default)]
pub(crate) struct NodeIndex {
    index: HashMap<String, State>,
}

#[derive(Debug)]
enum State {
    Pending(HashMap<String, HashMap<String, k8s::Pod>>),
    Known(KubeletIps),
}

#[derive(Clone, Debug, Hash, PartialEq, Eq)]
pub(crate) struct KubeletIps(Arc<[IpAddr]>);

// === impl NodeIndex ===

impl NodeIndex {
    pub fn get_or_push_pending(&mut self, pod: k8s::Pod) -> Option<(k8s::Pod, KubeletIps)> {
        let node_name = pod.spec.as_ref()?.node_name.clone()?;
        match self.index.entry(node_name) {
            HashEntry::Occupied(mut entry) => match entry.get_mut() {
                State::Known(ips) => Some((pod, ips.clone())),
                State::Pending(pods) => {
                    pods.entry(pod.namespace()?)
                        .or_default()
                        .insert(pod.name(), pod);
                    None
                }
            },
            HashEntry::Vacant(entry) => {
                let ns = pod.namespace()?;
                let name = pod.name();
                entry.insert(State::Pending(
                    Some((ns, Some((name, pod)).into_iter().collect()))
                        .into_iter()
                        .collect(),
                ));
                None
            }
        }
    }

    pub fn clear_pending_pod(&mut self, ns: &str, pod: &str) -> bool {
        for state in self.index.values_mut() {
            if let State::Pending(by_ns) = state {
                if let Some(pods) = by_ns.get_mut(ns) {
                    if pods.remove(pod).is_some() {
                        return true;
                    }
                }
            }
        }

        false
    }

    pub fn clear_pending_pods(&mut self) {
        let pending_nodes = self
            .index
            .iter()
            .filter_map(|(node, state)| match state {
                State::Known(_) => None,
                State::Pending(_) => Some(node.clone()),
            })
            .collect::<Vec<_>>();

        for node in pending_nodes {
            self.index.remove(&node);
        }
    }
}

// === impl Index ===

impl Index {
    /// Tracks the kubelet IP for each node.
    ///
    /// As pods are we created, we refer to the node->kubelet index to automatically allow traffic
    /// from the kubelet.
    #[instrument(
        skip(self, node),
        fields(name = ?node.metadata.name)
    )]
    pub fn apply_node(&mut self, node: k8s::Node) -> Result<()> {
        match self.nodes.index.entry(node.name()) {
            HashEntry::Vacant(entry) => {
                let ips = KubeletIps::try_from_node(node)
                    .with_context(|| format!("failed to load kubelet IPs for {}", entry.key()))?;
                debug!(?ips, "Adding");
                entry.insert(State::Known(ips));
                Ok(())
            }

            HashEntry::Occupied(mut entry) => {
                // If the node is already configured, ignore the update.
                if let State::Known(_) = entry.get() {
                    trace!("Already existed");
                    return Ok(());
                }

                // Otherwise, the update is replacing a set of pending pods. Update the state to the
                // known set of IPs and then apply all of the pending pods.
                let ips = KubeletIps::try_from_node(node)
                    .with_context(|| format!("failed to load kubelet IPs for {}", entry.key()))?;
                debug!(?ips, "Adding");
                let pods = match std::mem::replace(entry.get_mut(), State::Known(ips)) {
                    State::Pending(pods) => pods,
                    State::Known(_) => unreachable!("the node state must have been pending"),
                };

                let mut result = Ok(());
                for (_, by_ns) in pods.into_iter() {
                    for (_, pod) in by_ns.into_iter() {
                        if let Err(e) = self.apply_pod(pod) {
                            result = Err(e);
                        }
                    }
                }
                result
            }
        }
    }

    #[instrument(skip(self))]
    pub fn delete_node(&mut self, name: &str) -> Result<()> {
        self.nodes
            .index
            .remove(name)
            .ok_or_else(|| anyhow!("node {} does not exist", name))?;
        debug!("Deleted");
        Ok(())
    }

    #[instrument(skip(self, nodes))]
    pub fn reset_nodes(&mut self, nodes: Vec<k8s::Node>) -> Result<()> {
        // Avoid rebuilding data for nodes that have not changed.
        let mut prior = self
            .nodes
            .index
            .iter()
            .filter_map(|(name, state)| match state {
                State::Known(_) => Some(name.clone()),
                State::Pending(_) => None,
            })
            .collect::<HashSet<_>>();

        let mut result = Ok(());
        for node in nodes.into_iter() {
            let name = node.name();
            if prior.remove(&name) {
                trace!(%name, "Already existed");
            } else if let Err(error) = self.apply_node(node) {
                warn!(%name, %error, "Failed to apply node");
                result = Err(error);
            }
        }

        for name in prior.into_iter() {
            debug!(?name, "Removing defunct node");
            let removed = self.nodes.index.remove(&name).is_some();
            debug_assert!(removed, "node must be removable");
            if !removed {
                result = Err(anyhow!("node {} already removed", name));
            }
        }

        result
    }
}

// === impl KubeletIps ===

impl std::ops::Deref for KubeletIps {
    type Target = [IpAddr];

    fn deref(&self) -> &[IpAddr] {
        &*self.0
    }
}

impl KubeletIps {
    fn try_from_cidr(cidr: String) -> Result<IpAddr> {
        cidr.parse::<IpNet>()
            .with_context(|| format!("invalid CIDR {}", cidr))?
            .hosts()
            .next()
            .ok_or_else(|| anyhow!("pod CIDR network is empty"))
    }

    fn try_from_node(node: k8s::Node) -> Result<Self> {
        let spec = node.spec.ok_or_else(|| anyhow!("node missing spec"))?;

        let addrs = if spec.pod_cidrs.is_empty() {
            let cidr = spec
                .pod_cidr
                .ok_or_else(|| anyhow!("node missing pod_cidr"))?;
            let ip = Self::try_from_cidr(cidr)?;
            vec![ip]
        } else {
            spec.pod_cidrs
                .into_iter()
                .map(Self::try_from_cidr)
                .collect::<Result<Vec<_>>>()?
        };

        Ok(Self(addrs.into()))
    }
}
