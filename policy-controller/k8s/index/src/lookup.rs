use crate::{node::KubeletIps, PodServerRx};
use anyhow::{anyhow, Result};
use dashmap::{mapref::entry::Entry, DashMap};
use linkerd_policy_controller_core::{
    ClientAuthentication, ClientAuthorization, DiscoverInboundServer, InboundServer,
    InboundServerStream, NetworkMatch,
};
use std::{collections::HashMap, net::IpAddr, sync::Arc};

#[derive(Debug, Default)]
pub(crate) struct Writer(ByNs);

#[derive(Clone, Debug)]
pub struct Reader(ByNs);

type ByNs = Arc<DashMap<String, ByPod>>;
type ByPod = DashMap<String, ByPort>;

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
    kubelet: KubeletIps,

    /// A watch of server watches.
    rx: PodServerRx,
}

// === impl Writer ===

impl Writer {
    pub(crate) fn contains(&self, ns: impl AsRef<str>, pod: impl AsRef<str>) -> bool {
        self.0
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

    pub(crate) fn unset(&mut self, ns: impl AsRef<str>, pod: impl AsRef<str>) -> Result<ByPort> {
        let pods = self
            .0
            .get_mut(ns.as_ref())
            .ok_or_else(|| anyhow!("missing namespace {}", ns.as_ref()))?;

        let (_, ports) = pods
            .remove(pod.as_ref())
            .ok_or_else(|| anyhow!("missing pod {} in namespace {}", pod.as_ref(), ns.as_ref()))?;

        if (*pods).is_empty() {
            drop(pods);
            self.0.remove(ns.as_ref()).expect("namespace must exist");
        }

        Ok(ports)
    }
}

// === impl Reader ===

impl Reader {
    #[inline]
    pub(crate) fn lookup(&self, ns: &str, pod: &str, port: u16) -> Option<Rx> {
        self.0.get(ns)?.get(pod)?.get(&port).cloned()
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
    pub(crate) fn new(kubelet: KubeletIps, rx: PodServerRx) -> Self {
        Self { kubelet, rx }
    }

    #[inline]
    fn mk_server(kubelet: &[IpAddr], mut inner: InboundServer) -> InboundServer {
        let networks = kubelet.iter().copied().map(NetworkMatch::from).collect();
        let authz = ClientAuthorization {
            networks,
            authentication: ClientAuthentication::Unauthenticated,
        };

        inner.authorizations.insert("_health_check".into(), authz);
        inner
    }

    pub(crate) fn get(&self) -> InboundServer {
        Self::mk_server(&*self.kubelet, (*(*self.rx.borrow()).borrow()).clone())
    }

    /// Streams server configuration updates.
    pub(crate) fn into_stream(self) -> InboundServerStream {
        // The kubelet IPs for a pod cannot change at runtime.
        let kubelet = self.kubelet;

        // Watches server watches. This watch is updated as `Server` resources are created/deleted
        // (or modified to select/deselect a pod-port).
        let mut pod_port_rx = self.rx;

        // Watches an individual server resource. The inner watch is updated as a `Server` resource
        // is updated or as authorizations are modified.
        let mut server_rx = (*pod_port_rx.borrow_and_update()).clone();

        Box::pin(async_stream::stream! {
            // Get the initial server state and publish it.
            let mut server = (*server_rx.borrow_and_update()).clone();
            yield Self::mk_server(&*kubelet, server.clone());

            loop {
                tokio::select! {
                    // As the server is updated, publish the new state. Skip publishing updates hen
                    // the server is unchanged.
                    res = server_rx.changed() => {
                        if res.is_ok() {
                            let s = (*server_rx.borrow()).clone();
                            if s != server {
                                yield Self::mk_server(&*kubelet, s.clone());
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
                            yield Self::mk_server(&*kubelet, s.clone());
                            server = s;
                        }
                    },
                }
            }
        })
    }
}
