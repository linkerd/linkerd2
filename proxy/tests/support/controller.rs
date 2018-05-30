#![cfg_attr(feature = "cargo-clippy", allow(clone_on_ref_ptr))]

use support::*;
use support::futures::future::Executor;
// use support::tokio::executor::Executor as _TokioExecutor;

use std::collections::{HashMap, VecDeque};
use std::io;
use std::net::IpAddr;
use std::sync::{Arc, Mutex};

use conduit_proxy_controller_grpc::common::{self, Destination};
use conduit_proxy_controller_grpc::destination as pb;


pub fn new() -> Controller {
    Controller::new()
}

pub type Labels = HashMap<String, String>;

#[derive(Debug)]
pub struct DstReceiver(sync::mpsc::UnboundedReceiver<pb::Update>);

#[derive(Clone, Debug)]
pub struct DstSender(sync::mpsc::UnboundedSender<pb::Update>);

#[derive(Clone, Debug, Default)]
pub struct Controller {
    expect_dst_calls: Arc<Mutex<VecDeque<(Destination, DstReceiver)>>>,
}

pub struct Listening {
    pub addr: SocketAddr,
    shutdown: Shutdown,
}

impl Controller {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn destination_tx(&self, dest: &str) -> DstSender {
        let (tx, rx) = sync::mpsc::unbounded();
        let dst = common::Destination {
            scheme: "k8s".into(),
            path: dest.into(),
        };
        self.expect_dst_calls
            .lock()
            .unwrap()
            .push_back((dst, DstReceiver(rx)));
        DstSender(tx)
    }

    pub fn destination_and_close(self, dest: &str, addr: SocketAddr) -> Self {
        self.destination_tx(dest).send_addr(addr);
        self
    }

    pub fn destination_close(self, dest: &str) -> Self {
        drop(self.destination_tx(dest));
        self
    }

    pub fn run(self) -> Listening {
        run(self)
    }
}

impl Stream for DstReceiver {
    type Item = pb::Update;
    type Error = grpc::Error;
    fn poll(&mut self) -> Poll<Option<Self::Item>, Self::Error> {
        self.0.poll().map_err(|_| grpc::Error::Grpc(grpc::Status::INTERNAL, HeaderMap::new()))
    }
}

impl DstSender {
    pub fn send(&self, up: pb::Update) {
        self.0.unbounded_send(up).expect("send dst update")
    }

    pub fn send_addr(&self, addr: SocketAddr) {
        self.send(destination_add(addr))
    }

    pub fn send_labeled(&self, addr: SocketAddr, addr_labels: Labels, parent_labels: Labels) {
        self.send(destination_add_labeled(addr, addr_labels, parent_labels));
    }
}

impl pb::server::Destination for Controller {
    type GetStream = DstReceiver;
    type GetFuture = future::FutureResult<grpc::Response<Self::GetStream>, grpc::Error>;

    fn get(&mut self, req: grpc::Request<Destination>) -> Self::GetFuture {
        if let Ok(mut calls) = self.expect_dst_calls.lock() {
            if let Some((dst, updates)) = calls.pop_front() {
                if &dst == req.get_ref() {
                    return future::ok(grpc::Response::new(updates));
                }

                calls.push_front((dst, updates));
            }
        }

        future::err(grpc::Error::Grpc(grpc::Status::INTERNAL, HeaderMap::new()))
    }
}

fn run(controller: Controller) -> Listening {
    let (tx, rx) = shutdown_signal();
    let (addr_tx, addr_rx) = oneshot::channel();
    ::std::thread::Builder::new()
        .name("support controller".into())
        .spawn(move || {
            let new = pb::server::DestinationServer::new(controller);
            let mut runtime = runtime::current_thread::Runtime::new()
                .expect("support controller runtime");
            let h2 = tower_h2::Server::new(new,
                Default::default(),
                LazyExecutor,
            );

            let addr = ([127, 0, 0, 1], 0).into();
            let bind = TcpListener::bind(&addr).expect("bind");

            let _ = addr_tx.send(bind.local_addr().expect("addr"));

            let serve = bind.incoming()
                .fold(h2, |h2, sock| {
                    if let Err(e) = sock.set_nodelay(true) {
                        return Err(e);
                    }

                    let serve = h2.serve(sock);
                    current_thread::TaskExecutor::current()
                        .execute(serve.map_err(|e| println!("controller error: {:?}", e)))
                        .map_err(|e| {
                            println!("controller execute error: {:?}", e);
                            io::Error::from(io::ErrorKind::Other)
                        })
                        .map(|_| h2)
                });


            runtime.spawn(Box::new(
                serve
                    .map(|_| ())
                    .map_err(|e| println!("controller error: {}", e)),
            ));
            runtime.block_on(rx).expect("support controller run");
        }).unwrap();

    let addr = addr_rx.wait().expect("addr");
    Listening {
        addr,
        shutdown: tx,
    }
}

pub fn destination_add(addr: SocketAddr) -> pb::Update {
    destination_add_labeled(addr, HashMap::new(), HashMap::new())
}

pub fn destination_add_labeled(
    addr: SocketAddr,
    set_labels: HashMap<String, String>,
    addr_labels: HashMap<String, String>)
    -> pb::Update
{
    pb::Update {
        update: Some(pb::update::Update::Add(
            pb::WeightedAddrSet {
                addrs: vec![
                    pb::WeightedAddr {
                        addr: Some(common::TcpAddress {
                            ip: Some(ip_conv(addr.ip())),
                            port: u32::from(addr.port()),
                        }),
                        weight: 0,
                        metric_labels: addr_labels,
                        ..Default::default()
                    },
                ],
                metric_labels: set_labels,
            },
        )),
    }
}

pub fn destination_add_none() -> pb::Update {
    pb::Update {
        update: Some(pb::update::Update::Add(
            pb::WeightedAddrSet {
                addrs: Vec::new(),
                ..Default::default()
            },
        )),
    }
}

pub fn destination_remove_none() -> pb::Update {
    pb::Update {
        update: Some(pb::update::Update::Remove(
            pb::AddrSet {
                addrs: Vec::new(),
                ..Default::default()
            },
        )),
    }
}

pub fn destination_exists_with_no_endpoints() -> pb::Update {
    pb::Update {
        update: Some(pb::update::Update::NoEndpoints(
            pb::NoEndpoints { exists: true }
        )),
    }
}

fn ip_conv(ip: IpAddr) -> common::IpAddress {
    match ip {
        IpAddr::V4(v4) => common::IpAddress {
            ip: Some(common::ip_address::Ip::Ipv4(v4.into())),
        },
        IpAddr::V6(v6) => {
            let (first, last) = octets_to_u64s(v6.octets());
            common::IpAddress {
                ip: Some(common::ip_address::Ip::Ipv6(common::IPv6 {
                    first,
                    last,
                })),
            }
        }
    }
}

fn octets_to_u64s(octets: [u8; 16]) -> (u64, u64) {
    let first = (u64::from(octets[0]) << 56) + (u64::from(octets[1]) << 48)
        + (u64::from(octets[2]) << 40) + (u64::from(octets[3]) << 32)
        + (u64::from(octets[4]) << 24) + (u64::from(octets[5]) << 16)
        + (u64::from(octets[6]) << 8) + u64::from(octets[7]);
    let last = (u64::from(octets[8]) << 56) + (u64::from(octets[9]) << 48)
        + (u64::from(octets[10]) << 40) + (u64::from(octets[11]) << 32)
        + (u64::from(octets[12]) << 24) + (u64::from(octets[13]) << 16)
        + (u64::from(octets[14]) << 8) + u64::from(octets[15]);
    (first, last)
}
