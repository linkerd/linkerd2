use std::collections::{HashSet, VecDeque};
use std::collections::hash_map::{Entry, HashMap};
use std::net::SocketAddr;

use futures::{Async, Future, Poll, Stream};
use futures::sync::mpsc;
use tower::Service;
use tower_discover::{Change, Discover};
use tower_grpc;

use fully_qualified_authority::FullyQualifiedAuthority;

use super::codec::Protobuf;
use super::pb::common::{Destination, TcpAddress};
use super::pb::proxy::destination::Update as PbUpdate;
use super::pb::proxy::destination::client::Destination as DestinationSvc;
use super::pb::proxy::destination::client::destination_methods::Get as GetRpc;
use super::pb::proxy::destination::update::Update as PbUpdate2;

pub type ClientBody = ::tower_grpc::client::codec::EncodingBody<
    Protobuf<Destination, PbUpdate>,
    ::tower_grpc::client::codec::Unary<Destination>,
>;

/// A handle to start watching a destination for address changes.
#[derive(Clone, Debug)]
pub struct Discovery {
    tx: mpsc::UnboundedSender<(FullyQualifiedAuthority, mpsc::UnboundedSender<Update>)>,
}

/// A `tower_discover::Discover`, given to a `tower_balance::Balance`.
#[derive(Debug)]
pub struct Watch<B> {
    rx: mpsc::UnboundedReceiver<Update>,
    bind: B,
}

/// A background handle to eventually bind on the controller thread.
#[derive(Debug)]
pub struct Background {
    rx: mpsc::UnboundedReceiver<(FullyQualifiedAuthority, mpsc::UnboundedSender<Update>)>,
}

type DiscoveryWatch<F> = DestinationSet<
    tower_grpc::client::Streaming<
        tower_grpc::client::ResponseFuture<Protobuf<Destination, PbUpdate>, F>,
        tower_grpc::client::codec::DecodingBody<Protobuf<Destination, PbUpdate>>,
    >,
>;

/// A future returned from `Background::work()`, doing the work of talking to
/// the controller destination API.
#[derive(Debug)]
pub struct DiscoveryWork<F> {
    destinations: HashMap<FullyQualifiedAuthority, DiscoveryWatch<F>>,
    /// A queue of authorities that need to be reconnected.
    reconnects: VecDeque<FullyQualifiedAuthority>,
    /// The Destination.Get RPC client service.
    /// Each poll, records whether the rpc service was till ready.
    rpc_ready: bool,
    /// A receiver of new watch requests.
    rx: mpsc::UnboundedReceiver<(FullyQualifiedAuthority, mpsc::UnboundedSender<Update>)>,
}

#[derive(Debug)]
struct DestinationSet<R> {
    addrs: HashSet<SocketAddr>,
    needs_reconnect: bool,
    rx: R,
    tx: mpsc::UnboundedSender<Update>,
}

#[derive(Debug)]
enum Update {
    Insert(SocketAddr),
    Remove(SocketAddr),
}

/// Bind a `SocketAddr` with a protocol.
pub trait Bind {
    /// Requests handled by the discovered services
    type Request;

    /// Responses given by the discovered services
    type Response;

    /// Errors produced by the discovered services
    type Error;

    type BindError;

    /// The discovered `Service` instance.
    type Service: Service<Request = Self::Request, Response = Self::Response, Error = Self::Error>;

    /// Bind a socket address with a service.
    fn bind(&self, addr: &SocketAddr) -> Result<Self::Service, Self::BindError>;
}

/// Creates a "channel" of `Discovery` to `Background` handles.
///
/// The `Discovery` is used by a listener, the `Background` is consumed
/// on the controller thread.
pub fn new() -> (Discovery, Background) {
    let (tx, rx) = mpsc::unbounded();
    (
        Discovery {
            tx,
        },
        Background {
            rx,
        },
    )
}

// ==== impl Discovery =====

impl Discovery {
    /// Start watching for address changes for a certain authority.
    pub fn resolve<B>(&self, authority: &FullyQualifiedAuthority, bind: B) -> Watch<B> {
        trace!("resolve; authority={:?}", authority);
        let (tx, rx) = mpsc::unbounded();
        self.tx
            .unbounded_send((authority.clone(), tx))
            .expect("unbounded can't fail");

        Watch {
            rx,
            bind,
        }
    }
}

// ==== impl Watch =====

impl<B> Discover for Watch<B>
where
    B: Bind,
{
    type Key = SocketAddr;
    type Request = B::Request;
    type Response = B::Response;
    type Error = B::Error;
    type Service = B::Service;
    type DiscoverError = ();

    fn poll(&mut self) -> Poll<Change<Self::Key, Self::Service>, Self::DiscoverError> {
        let up = self.rx.poll();
        trace!("watch: {:?}", up);
        let update = match up {
            Ok(Async::Ready(Some(update))) => update,
            Ok(Async::Ready(None)) => unreachable!(),
            Ok(Async::NotReady) => return Ok(Async::NotReady),
            Err(_) => return Err(()),
        };

        match update {
            Update::Insert(addr) => {
                let service = self.bind.bind(&addr).map_err(|_| ())?;

                Ok(Async::Ready(Change::Insert(addr, service)))
            }
            Update::Remove(addr) => Ok(Async::Ready(Change::Remove(addr))),
        }
    }
}

// ==== impl Background =====

impl Background {
    /// Bind this handle to start talking to the controller API.
    pub fn work<F>(self) -> DiscoveryWork<F> {
        DiscoveryWork {
            destinations: HashMap::new(),
            reconnects: VecDeque::new(),
            rpc_ready: false,
            rx: self.rx,
        }
    }
}

// ==== impl DiscoveryWork =====

impl<F> DiscoveryWork<F>
where
    F: Future<Item = ::http::Response<::tower_h2::RecvBody>>,
    F::Error: ::std::fmt::Debug,
{
    pub fn poll_rpc<S>(&mut self, client: &mut S)
    where
        S: Service<
            Request = ::http::Request<ClientBody>,
            Response = F::Item,
            Error = F::Error,
            Future = F,
        >,
    {
        // This loop is make sure any streams that were found disconnected
        // in `poll_destinations` while the `rpc` service is ready should
        // be reconnected now, otherwise the task would just sleep...
        loop {
            trace!("poll_rpc");
            self.poll_new_watches(client);
            self.poll_destinations();

            if self.reconnects.is_empty() || !self.rpc_ready {
                break;
            }
        }
    }

    fn poll_new_watches<S>(&mut self, mut client: &mut S)
    where
        S: Service<
            Request = ::http::Request<ClientBody>,
            Response = F::Item,
            Error = F::Error,
            Future = F,
        >,
    {
        loop {
            // if rpc service isn't ready, not much we can do...
            match client.poll_ready() {
                Ok(Async::Ready(())) => {
                    self.rpc_ready = true;
                }
                Ok(Async::NotReady) => {
                    self.rpc_ready = false;
                    break;
                }
                Err(err) => {
                    warn!("Destination.Get poll_ready error: {:?}", err);
                    self.rpc_ready = false;
                    break;
                }
            }

            // handle any pending reconnects first
            if self.poll_reconnect(client) {
                continue;
            }

            let grpc = tower_grpc::Client::new(Protobuf::new(), &mut client);
            let mut rpc = GetRpc::new(grpc);
            // check for any new watches
            match self.rx.poll() {
                Ok(Async::Ready(Some((auth, tx)))) => {
                    trace!("Destination.Get {:?}", auth);
                    match self.destinations.entry(auth) {
                        Entry::Occupied(mut occ) => {
                            occ.get_mut().tx = tx;
                        }
                        Entry::Vacant(vac) => {
                            let req = Destination {
                                scheme: "k8s".into(),
                                path: vac.key().without_trailing_dot().into(),
                            };
                            let stream = DestinationSvc::new(&mut rpc).get(req);
                            vac.insert(DestinationSet {
                                addrs: HashSet::new(),
                                needs_reconnect: false,
                                rx: stream,
                                tx,
                            });
                        }
                    }
                }
                Ok(Async::Ready(None)) => {
                    trace!("Discover tx is dropped, shutdown?");
                    return;
                }
                Ok(Async::NotReady) => break,
                Err(_) => unreachable!("unbounded receiver doesn't error"),
            }
        }
    }

    /// Tries to reconnect next watch stream. Returns true if reconnection started.
    fn poll_reconnect<S>(&mut self, client: &mut S) -> bool
    where
        S: Service<
            Request = ::http::Request<ClientBody>,
            Response = F::Item,
            Error = F::Error,
            Future = F,
        >,
    {
        debug_assert!(self.rpc_ready);
        let grpc = tower_grpc::Client::new(Protobuf::new(), client);
        let mut rpc = GetRpc::new(grpc);

        while let Some(auth) = self.reconnects.pop_front() {
            if let Some(set) = self.destinations.get_mut(&auth) {
                trace!("Destination.Get reconnect {:?}", auth);
                let req = Destination {
                    scheme: "k8s".into(),
                    path: auth.without_trailing_dot().into(),
                };
                set.rx = DestinationSvc::new(&mut rpc).get(req);
                set.needs_reconnect = false;
                return true;
            } else {
                trace!("reconnect no longer needed: {:?}", auth);
            }
        }
        false
    }

    fn poll_destinations(&mut self) {
        for (auth, set) in &mut self.destinations {
            if set.needs_reconnect {
                continue;
            }
            let needs_reconnect = 'set: loop {
                match set.rx.poll() {
                    Ok(Async::Ready(Some(update))) => match update.update {
                        Some(PbUpdate2::Add(a_set)) => for addr in a_set.addrs {
                            if let Some(addr) = addr.addr.and_then(pb_to_sock_addr) {
                                if set.addrs.insert(addr) {
                                    trace!("update {:?} for {:?}", addr, auth);
                                    let _ = set.tx.unbounded_send(Update::Insert(addr));
                                }
                            }
                        },
                        Some(PbUpdate2::Remove(r_set)) => for addr in r_set.addrs {
                            if let Some(addr) = pb_to_sock_addr(addr) {
                                if set.addrs.remove(&addr) {
                                    trace!("remove {:?} for {:?}", addr, auth);
                                    let _ = set.tx.unbounded_send(Update::Remove(addr));
                                }
                            }
                        },
                        None => (),
                    },
                    Ok(Async::Ready(None)) => {
                        trace!(
                            "Destination.Get stream ended for {:?}, must reconnect",
                            auth
                        );
                        break 'set true;
                    }
                    Ok(Async::NotReady) => break 'set false,
                    Err(err) => {
                        warn!("Destination.Get stream errored for {:?}: {:?}", auth, err);
                        break 'set true;
                    }
                }
            };
            if needs_reconnect {
                set.needs_reconnect = true;
                self.reconnects.push_back(FullyQualifiedAuthority::clone(auth));
            }
        }
    }
}

// ===== impl Bind =====

impl<F, S, E> Bind for F
where
    F: Fn(&SocketAddr) -> Result<S, E>,
    S: Service,
{
    type Request = S::Request;
    type Response = S::Response;
    type Error = S::Error;
    type Service = S;
    type BindError = E;

    fn bind(&self, addr: &SocketAddr) -> Result<Self::Service, Self::BindError> {
        (*self)(addr)
    }
}

fn pb_to_sock_addr(pb: TcpAddress) -> Option<SocketAddr> {
    use super::pb::common::ip_address::Ip;
    use std::net::{Ipv4Addr, Ipv6Addr};
    /*
    current structure is:
    TcpAddress {
        ip: Option<IpAddress {
            ip: Option<enum Ip {
                Ipv4(u32),
                Ipv6(IPv6 {
                    first: u64,
                    last: u64,
                }),
            }>,
        }>,
        port: u32,
    }
    */
    // oh gawd i wish ? worked with Options already...
    match pb.ip {
        Some(ip) => match ip.ip {
            Some(Ip::Ipv4(octets)) => {
                let ipv4 = Ipv4Addr::from(octets);
                Some(SocketAddr::from((ipv4, pb.port as u16)))
            }
            Some(Ip::Ipv6(v6)) => {
                let octets = [
                    (v6.first >> 56) as u8,
                    (v6.first >> 48) as u8,
                    (v6.first >> 40) as u8,
                    (v6.first >> 32) as u8,
                    (v6.first >> 24) as u8,
                    (v6.first >> 16) as u8,
                    (v6.first >> 8) as u8,
                    v6.first as u8,
                    (v6.last >> 56) as u8,
                    (v6.last >> 48) as u8,
                    (v6.last >> 40) as u8,
                    (v6.last >> 32) as u8,
                    (v6.last >> 24) as u8,
                    (v6.last >> 16) as u8,
                    (v6.last >> 8) as u8,
                    v6.last as u8,
                ];
                let ipv6 = Ipv6Addr::from(octets);
                Some(SocketAddr::from((ipv6, pb.port as u16)))
            }
            None => None,
        },
        None => None,
    }
}
