use crate::PodServerRx;
use ahash::AHashMap as HashMap;
use anyhow::{anyhow, Result};
use linkerd_policy_controller_core::{DiscoverInboundServer, InboundServer, InboundServerStream};
use parking_lot::RwLock;
use std::{collections::hash_map::Entry, sync::Arc};

#[derive(Clone, Debug, Default)]
pub(crate) struct Writer(ByNs);

// Supports lookups in a shared map of pod-ports.
#[derive(Clone, Debug)]
pub struct Reader(ByNs);

type ByNs = Arc<RwLock<HashMap<String, ByPod>>>;
type ByPod = HashMap<String, ByPort>;

// Boxed to enforce immutability.
type ByPort = Box<HashMap<u16, Rx>>;

pub(crate) fn pair() -> (Writer, Reader) {
    let by_ns = ByNs::default();
    let w = Writer(by_ns.clone());
    let r = Reader(by_ns);
    (w, r)
}

/// Represents a pod server's configuration.
#[derive(Clone, Debug)]
pub struct Rx {
    /// A watch of server watches.
    rx: PodServerRx,
}

// === impl Writer ===

impl Writer {
    pub(crate) fn contains(&self, ns: impl AsRef<str>, pod: impl AsRef<str>) -> bool {
        self.0
            .read()
            .get(ns.as_ref())
            .map(|ns| ns.contains_key(pod.as_ref()))
            .unwrap_or(false)
    }

    pub(crate) fn set(
        &mut self,
        ns: impl ToString,
        pod: impl ToString,
        ports: impl IntoIterator<Item = (u16, Rx)>,
    ) -> Result<()> {
        match self
            .0
            .write()
            .entry(ns.to_string())
            .or_default()
            .entry(pod.to_string())
        {
            Entry::Vacant(entry) => {
                entry.insert(ports.into_iter().collect::<HashMap<_, _>>().into());
                Ok(())
            }
            Entry::Occupied(_) => Err(anyhow!(
                "pod {} already exists in namespace {}",
                pod.to_string(),
                ns.to_string()
            )),
        }
    }

    pub(crate) fn unset(&mut self, ns: &str, pod: &str) -> Result<ByPort> {
        let mut nses = self.0.write();
        let mut ns_entry = match nses.entry(ns.to_string()) {
            Entry::Occupied(entry) => entry,
            Entry::Vacant(_) => return Err(anyhow!("missing namespace {}", ns)),
        };

        let ports = ns_entry
            .get_mut()
            .remove(pod)
            .ok_or_else(|| anyhow!("missing pod {} in namespace {}", pod, ns))?;

        if ns_entry.get().is_empty() {
            ns_entry.remove_entry();
        }

        Ok(ports)
    }
}

// === impl Reader ===

impl Reader {
    #[inline]
    pub(crate) fn lookup(&self, ns: &str, pod: &str, port: u16) -> Option<Rx> {
        self.0.read().get(ns)?.get(pod)?.get(&port).cloned()
    }
}

#[async_trait::async_trait]
impl DiscoverInboundServer<(String, String, u16)> for Reader {
    async fn get_inbound_server(
        &self,
        (ns, pod, port): (String, String, u16),
    ) -> Result<Option<InboundServer>> {
        Ok(self.lookup(&*ns, &*pod, port).map(|rx| rx.get()))
    }

    async fn watch_inbound_server(
        &self,
        (ns, pod, port): (String, String, u16),
    ) -> Result<Option<InboundServerStream>> {
        Ok(self.lookup(&*ns, &*pod, port).map(|rx| rx.into_stream()))
    }
}

// === impl Rx ===

impl Rx {
    pub(crate) fn new(rx: PodServerRx) -> Self {
        Self { rx }
    }

    pub(crate) fn get(&self) -> InboundServer {
        (*self.rx.borrow().borrow()).clone()
    }

    /// Streams server configuration updates.
    pub(crate) fn into_stream(self) -> InboundServerStream {
        // Watches server watches. This watch is updated as `Server` resources are created/deleted
        // (or modified to select/deselect a pod-port).
        let mut pod_port_rx = self.rx;

        // Watches an individual server resource. The inner watch is updated as a `Server` resource
        // is updated or as authorizations are modified.
        let mut server_rx = (*pod_port_rx.borrow_and_update()).clone();

        Box::pin(async_stream::stream! {
            // Get the initial server state and publish it.
            let mut server = (*server_rx.borrow_and_update()).clone();
            yield server.clone();

            loop {
                tokio::select! {
                    // As the server is updated, publish the new state. Skip publishing updates when
                    // the server is unchanged.
                    res = server_rx.changed() => {
                        if res.is_ok() {
                            let s = (*server_rx.borrow()).clone();
                            if s != server {
                                yield s.clone();
                                server = s;
                            }
                        }
                    },

                    // As the pod-port is updated with a new server watch, update our state and
                    // publish the server if it differs from the prior state.
                    res = pod_port_rx.changed() => {
                        if res.is_err() {
                            return;
                        }

                        server_rx = (*pod_port_rx.borrow()).clone();
                        let s = (*server_rx.borrow_and_update()).clone();
                        if s != server {
                            yield s.clone();
                            server = s;
                        }
                    },
                }
            }
        })
    }
}
