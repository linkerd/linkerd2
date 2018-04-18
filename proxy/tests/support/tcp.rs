use support::*;

use std::collections::VecDeque;
use std::io;
use std::sync::Arc;
use std::sync::atomic::{AtomicUsize, Ordering};

use self::futures::sync::{mpsc, oneshot};
use self::tokio_core::net::TcpStream;

type TcpSender = mpsc::UnboundedSender<oneshot::Sender<TcpConnSender>>;
type TcpConnSender = mpsc::UnboundedSender<(Option<Vec<u8>>, oneshot::Sender<io::Result<Option<Vec<u8>>>>)>;

pub fn client(addr: SocketAddr) -> TcpClient {
    let tx = run_client(addr);
    TcpClient {
        tx,
    }
}

pub fn server() -> TcpServer {
    TcpServer {
        accepts: VecDeque::new(),
    }
}

pub struct TcpClient {
    tx: TcpSender,
}

type Handler = Box<CallBox + Send>;

trait CallBox: 'static {
    fn call_box(self: Box<Self>, sock: TcpStream) -> Box<Future<Item=(), Error=()>>;
}

impl<F: FnOnce(TcpStream) -> Box<Future<Item=(), Error=()>> + Send + 'static> CallBox for F {
    fn call_box(self: Box<Self>, sock: TcpStream) -> Box<Future<Item=(), Error=()>> {
        (*self)(sock)
    }
}

pub struct TcpServer {
    accepts: VecDeque<Handler>,
}

pub struct TcpConn {
    tx: TcpConnSender,
}

impl TcpClient {
    pub fn connect(&self) -> TcpConn {
        let (tx, rx) = oneshot::channel();
        let _ = self.tx.unbounded_send(tx);
        let tx = rx.map_err(|_| panic!("tcp connect dropped"))
            .wait()
            .unwrap();
        TcpConn {
            tx,
        }
    }
}

impl TcpServer {
    pub fn accept<F, U>(self, cb: F) -> Self
    where
        F: FnOnce(Vec<u8>) -> U + Send + 'static,
        U: Into<Vec<u8>>,
    {
        self.accept_fut(move |sock| {
            tokio_io::io::read(sock, vec![0; 1024])
                .and_then(move |(sock, mut vec, n)| {
                    vec.truncate(n);
                    let write = cb(vec).into();
                    tokio_io::io::write_all(sock, write)
                })
                .map(|_| ())
                .map_err(|e| panic!("tcp server error: {}", e))
        })
    }

    pub fn accept_fut<F, U>(mut self, cb: F) -> Self
    where
        F: FnOnce(TcpStream) -> U + Send + 'static,
        U: IntoFuture<Item=(), Error=()> + 'static,
    {
        self.accepts.push_back(Box::new(move |tcp| -> Box<Future<Item=(), Error=()>> {
            Box::new(cb(tcp).into_future())
        }));
        self
    }

    pub fn run(self) -> server::Listening {
        run_server(self)
    }
}

impl TcpConn {
    pub fn read(&self) -> Vec<u8> {
        self.try_read().expect("read")
    }

    pub fn try_read(&self) -> io::Result<Vec<u8>> {
        let (tx, rx) = oneshot::channel();
        let _ = self.tx.unbounded_send((None, tx));
        rx.map_err(|_| panic!("tcp read dropped"))
            .map(|res| res.map(|opt| opt.unwrap()))
            .wait()
            .unwrap()
    }

    pub fn write<T: Into<Vec<u8>>>(&self, buf: T) {
        let (tx, rx) = oneshot::channel();
        let _ = self.tx.unbounded_send((Some(buf.into()), tx));
        rx.map_err(|_| panic!("tcp write dropped"))
            .map(|rsp| assert!(rsp.unwrap().is_none()))
            .wait()
            .unwrap()
    }
}


fn run_client(addr: SocketAddr) -> TcpSender {
    let (tx, rx) = mpsc::unbounded();
    ::std::thread::Builder::new().name("support tcp client".into()).spawn(move || {
        let mut core = Core::new().unwrap();
        let handle = core.handle();

        let work = rx.for_each(|cb: oneshot::Sender<_>| {
            let fut = TcpStream::connect(&addr, &handle)
                .map_err(|e| panic!("connect error: {}", e))
                .and_then(move |tcp| {
                    let (tx, rx) = mpsc::unbounded();
                    cb.send(tx).unwrap();
                    rx.fold(tcp, |tcp, (action, cb): (Option<Vec<u8>>, oneshot::Sender<io::Result<Option<Vec<u8>>>>)| {
                        let f: Box<Future<Item=TcpStream, Error=()>> = match action {
                            None => {
                                Box::new(tokio_io::io::read(tcp, vec![0; 1024])
                                    .then(move |res| {
                                        match res {
                                            Ok((tcp, mut vec, n)) => {
                                                vec.truncate(n);
                                                cb.send(Ok(Some(vec))).unwrap();
                                                Ok(tcp)
                                            }
                                            Err(e) => {
                                                cb.send(Err(e)).unwrap();
                                                Err(())
                                            }
                                        }
                                    }))
                            },
                            Some(vec) => {
                                Box::new(tokio_io::io::write_all(tcp, vec)
                                    .then(move |res| {
                                        match res {
                                            Ok((tcp, _)) => {
                                                cb.send(Ok(None)).unwrap();
                                                Ok(tcp)
                                            },
                                            Err(e) => {
                                                cb.send(Err(e)).unwrap();
                                                Err(())
                                            }
                                        }
                                    }))
                            }
                        };
                        f
                    })
                        .map(|_| ())
                        .map_err(|_| ())
                });

            handle.spawn(fut);
            Ok(())

        }).map_err(|e| println!("client error: {:?}", e));
        core.run(work).unwrap();
    }).unwrap();
    tx
}

fn run_server(tcp: TcpServer) -> server::Listening {
    let (tx, rx) = shutdown_signal();
    let (addr_tx, addr_rx) = oneshot::channel();
    let conn_count = Arc::new(AtomicUsize::from(0));
    let srv_conn_count = Arc::clone(&conn_count);
    ::std::thread::Builder::new().name("support tcp server".into()).spawn(move || {
        let mut core = Core::new().unwrap();
        let reactor = core.handle();

        let addr = ([127, 0, 0, 1], 0).into();
        let bind = TcpListener::bind(&addr, &reactor).expect("bind");

        let local_addr = bind.local_addr().expect("local_addr");
        let _ = addr_tx.send(local_addr);

        let mut accepts = tcp.accepts;

        let listen = bind
            .incoming()
            .for_each(move |(sock, _)| {
                let cb = accepts.pop_front().expect("no more accepts");
                srv_conn_count.fetch_add(1, Ordering::Release);

                let fut = cb.call_box(sock);

                reactor.spawn(fut);
                Ok(())
            })
            .map_err(|e| panic!("tcp accept error: {}", e));

        core.handle().spawn(listen);
        core.run(rx).unwrap();
    }).unwrap();

    let addr = addr_rx.wait().expect("addr");
    server::Listening {
        addr,
        shutdown: tx,
        conn_count,
    }
}
