use std::collections::{HashSet, VecDeque};
use std::collections::hash_map::{Entry, HashMap};
use std::net::SocketAddr;
use std::fmt;
use std::mem;

use futures::{Async, Future, Poll, Stream};
use futures::sync::mpsc;
use tower::Service;
use tower_h2::{HttpService, BoxBody, RecvBody};
use tower_discover::{Change, Discover};
use tower_grpc as grpc;

use fully_qualified_authority::FullyQualifiedAuthority;

use conduit_proxy_controller_grpc::common::{Destination, TcpAddress};
use conduit_proxy_controller_grpc::destination::Update as PbUpdate;
use conduit_proxy_controller_grpc::destination::update::Update as PbUpdate2;
use conduit_proxy_controller_grpc::destination::client::{Destination as DestinationSvc};

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

/// A future returned from `Background::work()`, doing the work of talking to
/// the controller destination API.
// TODO: debug impl
pub struct DiscoveryWork<T: HttpService<ResponseBody = RecvBody>> {
    destinations: HashMap<
        FullyQualifiedAuthority,
        DestinationSet<T>
    >,
    /// A queue of authorities that need to be reconnected.
    reconnects: VecDeque<FullyQualifiedAuthority>,
    /// The Destination.Get RPC client service.
    /// Each poll, records whether the rpc service was till ready.
    rpc_ready: bool,
    /// A receiver of new watch requests.
    rx: mpsc::UnboundedReceiver<(FullyQualifiedAuthority, mpsc::UnboundedSender<Update>)>,
}

struct DestinationSet<T: HttpService<ResponseBody = RecvBody>> {
    addrs: Exists<HashSet<SocketAddr>>,
    needs_reconnect: bool,
    rx: UpdateRx<T>,
    txs: Vec<mpsc::UnboundedSender<Update>>,
}

enum Exists<T> {
    Unknown, // Unknown if the item exists or not
    Yes(T),
    No, // Affirmatively known to not exist.
}

impl<T> Exists<T> {
    fn take(&mut self) -> Exists<T> {
        mem::replace(self, Exists::Unknown)
    }
}

/// Receiver for destination set updates.
///
/// The destination RPC returns a `ResponseFuture` whose item is a
/// `Response<Stream>`, so this type holds the state of that RPC call ---
/// either we're waiting for the future, or we have a stream --- and allows
/// us to implement `Stream` regardless of whether the RPC has returned yet
/// or not.
///
/// Polling an `UpdateRx` polls the wrapped future while we are
/// `Waiting`, and the `Stream` if we are `Streaming`. If the future is `Ready`,
/// then we switch states to `Streaming`.
enum UpdateRx<T: HttpService<ResponseBody = RecvBody>> {
    Waiting(UpdateRsp<T::Future>),
    Streaming(grpc::Streaming<PbUpdate, T::ResponseBody>),
}

type UpdateRsp<F> =
    grpc::client::server_streaming::ResponseFuture<PbUpdate, F>;

/// Wraps the error types returned by `UpdateRx` polls.
///
/// An `UpdateRx` error is either the error type of the `Future` in the
/// `UpdateRx::Waiting` state, or the `Stream` in the `UpdateRx::Streaming`
/// state.
// TODO: impl Error?
#[derive(Debug)]
enum RxError<T> {
    Future(grpc::Error<T>),
    Stream(grpc::Error),
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
    pub fn work<T>(self) -> DiscoveryWork<T>
    where T: HttpService<RequestBody = BoxBody, ResponseBody = RecvBody>,
          T::Error: fmt::Debug,
    {
        DiscoveryWork {
            destinations: HashMap::new(),
            reconnects: VecDeque::new(),
            rpc_ready: false,
            rx: self.rx,
        }
    }
}

// ==== impl DiscoveryWork =====

impl<T> DiscoveryWork<T>
where
    T: HttpService<RequestBody = BoxBody, ResponseBody = RecvBody>,
    T::Error: fmt::Debug,
{
    pub fn poll_rpc(&mut self, client: &mut T) {
        // This loop is make sure any streams that were found disconnected
        // in `poll_destinations` while the `rpc` service is ready should
        // be reconnected now, otherwise the task would just sleep...
        loop {
            self.poll_new_watches(client);
            self.poll_destinations();

            if self.reconnects.is_empty() || !self.rpc_ready {
                break;
            }
        }
    }

    fn poll_new_watches(&mut self, client: &mut T) {
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

            // check for any new watches
            match self.rx.poll() {
                Ok(Async::Ready(Some((auth, tx)))) => {
                    trace!("Destination.Get {:?}", auth);
                    match self.destinations.entry(auth) {
                        Entry::Occupied(mut occ) => {
                            let set = occ.get_mut();
                            // we may already know of some addresses here, so push
                            // them onto the new watch first
                            match set.addrs {
                                Exists::Yes(ref mut addrs) => {
                                    for &addr in addrs.iter() {
                                        tx.unbounded_send(Update::Insert(addr))
                                            .expect("unbounded_send does not fail");
                                    }
                                },
                                Exists::No | Exists::Unknown => (),
                            }
                            set.txs.push(tx);
                        }
                        Entry::Vacant(vac) => {
                            let req = Destination {
                                scheme: "k8s".into(),
                                path: vac.key().without_trailing_dot()
                                        .as_str().into(),
                            };
                            // TODO: Can grpc::Request::new be removed?
                            let mut svc = DestinationSvc::new(client.lift_ref());
                            let response = svc.get(grpc::Request::new(req));
                            let stream = UpdateRx::Waiting(response);

                            vac.insert(DestinationSet {
                                addrs: Exists::Unknown,
                                needs_reconnect: false,
                                rx: stream,
                                txs: vec![tx],
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
    fn poll_reconnect(&mut self, client: &mut T) -> bool {
        debug_assert!(self.rpc_ready);

        while let Some(auth) = self.reconnects.pop_front() {
            if let Some(set) = self.destinations.get_mut(&auth) {
                trace!("Destination.Get reconnect {:?}", auth);
                let req = Destination {
                    scheme: "k8s".into(),
                    path: auth.without_trailing_dot().as_str().into(),
                };
                let mut svc = DestinationSvc::new(client.lift_ref());
                let response = svc.get(grpc::Request::new(req));
                set.rx = UpdateRx::Waiting(response);
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
                        Some(PbUpdate2::Add(a_set)) =>
                            set.add(
                                auth,
                                a_set.addrs.iter().filter_map(
                                    |addr| addr.addr.clone().and_then(pb_to_sock_addr))),
                        Some(PbUpdate2::Remove(r_set)) =>
                            set.remove(
                                auth,
                                r_set.addrs.iter().filter_map(|addr| pb_to_sock_addr(addr.clone()))),
                        Some(PbUpdate2::NoEndpoints(no_endpoints)) =>
                            set.no_endpoints(auth, no_endpoints.exists),
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

// ===== impl DestinationSet =====

impl <T: HttpService<ResponseBody = RecvBody>> DestinationSet<T> {
    fn add<Addrs>(&mut self, authority_for_logging: &FullyQualifiedAuthority, addrs_to_add: Addrs)
        where Addrs: Iterator<Item = SocketAddr>
    {
        let mut addrs = match self.addrs.take() {
            Exists::Yes(addrs) => addrs,
            Exists::Unknown | Exists::No => {
                trace!("adding entries for {:?} that wasn't known to exist. Now assuming it does.",
                       authority_for_logging);
                HashSet::new()
            },
        };
        for addr in addrs_to_add {
            if addrs.insert(addr) {
                trace!("update {:?} for {:?}", addr, authority_for_logging);
                // retain is used to drop any senders that are dead
                self.txs.retain(|tx| {
                    tx.unbounded_send(Update::Insert(addr)).is_ok()
                });
            }
        }
        self.addrs = Exists::Yes(addrs);
    }

    fn remove<Addrs>(&mut self, authority_for_logging: &FullyQualifiedAuthority,
                     addrs_to_remove: Addrs)
        where Addrs: Iterator<Item = SocketAddr>
    {
        let addrs = match self.addrs.take() {
            Exists::Yes(mut addrs) => {
                for addr in addrs_to_remove {
                    if addrs.remove(&addr) {
                        self.notify_of_removal(addr, authority_for_logging)
                    }
                }
                addrs
            },
            Exists::Unknown | Exists::No => {
                trace!("remove addresses for {:?} that wasn't known to exist. Now assuming it does.",
                       authority_for_logging);
                HashSet::new()
            },
        };
        self.addrs = Exists::Yes(addrs)
    }

    fn no_endpoints(&mut self, authority_for_logging: &FullyQualifiedAuthority, exists: bool) {
        trace!("no endpoints for {:?} that is known to {}", authority_for_logging,
               if exists { "exist"} else { "not exist"});
        match self.addrs.take() {
            Exists::Yes(addrs) => {
                for addr in addrs {
                    self.notify_of_removal(addr, authority_for_logging)
                }
            },
            Exists::No | Exists::Unknown => {},
        }
        self.addrs = if exists {
            Exists::Yes(HashSet::new())
        } else {
            Exists::No
        };
    }

    fn notify_of_removal(&mut self, addr: SocketAddr,
                         authority_for_logging: &FullyQualifiedAuthority) {
        trace!("remove {:?} for {:?}", addr, authority_for_logging);
        // retain is used to drop any senders that are dead
        self.txs.retain(|tx| {
            tx.unbounded_send(Update::Remove(addr)).is_ok()
        });
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

// ===== impl UpdateRx =====

impl<T> Stream for UpdateRx<T>
where T: HttpService<RequestBody = BoxBody, ResponseBody = RecvBody>,
      T::Error: fmt::Debug,
{
    type Item = PbUpdate;
    type Error = RxError<T::Error>;

    fn poll(&mut self) -> Poll<Option<Self::Item>, Self::Error> {
        // this is not ideal.
        let stream = match *self {
            UpdateRx::Waiting(ref mut future) => match future.poll() {
                Ok(Async::Ready(response)) => response.into_inner(),
                Ok(Async::NotReady) => return Ok(Async::NotReady),
                Err(e) => return Err(RxError::Future(e)),
            },
            UpdateRx::Streaming(ref mut stream) =>
                return stream.poll().map_err(RxError::Stream),
        };
        *self = UpdateRx::Streaming(stream);
        self.poll()
    }
}

// ===== impl RxError =====

fn pb_to_sock_addr(pb: TcpAddress) -> Option<SocketAddr> {
    use conduit_proxy_controller_grpc::common::ip_address::Ip;
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
