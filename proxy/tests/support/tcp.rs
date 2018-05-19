use support::*;

use std::collections::VecDeque;
use std::io;
use std::net::TcpListener as StdTcpListener;
use std::sync::Arc;
use std::sync::atomic::{AtomicUsize, Ordering};

use self::futures::sync::{mpsc, oneshot};
use self::tokio::net::TcpStream;

type TcpSender = mpsc::UnboundedSender<oneshot::Sender<TcpConnSender>>;
type TcpConnSender = mpsc::UnboundedSender<(Option<Vec<u8>>, oneshot::Sender<io::Result<Option<Vec<u8>>>>)>;

pub fn client(addr: SocketAddr) -> TcpClient {
    let tx = run_client(addr);
    TcpClient {
        addr,
        tx,
    }
}

pub fn server() -> TcpServer {
    TcpServer {
        accepts: VecDeque::new(),
    }
}

pub struct TcpClient {
    addr: SocketAddr,
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
    addr: SocketAddr,
    tx: TcpConnSender,
}

impl TcpClient {
    pub fn connect(&self) -> TcpConn {
        let (tx, rx) = oneshot::channel();
        let _ = self.tx.unbounded_send(tx);
        let tx = rx.map_err(|_| panic!("tcp connect dropped"))
            .wait()
            .unwrap();
        println!("tcp client (addr={}): connected", self.addr);
        TcpConn {
            addr: self.addr,
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
        self
            .try_read()
            .unwrap_or_else(|e| {
                panic!("TcpConn(addr={}) read() error: {:?}", self.addr, e)
            })
    }

    pub fn try_read(&self) -> io::Result<Vec<u8>> {
        println!("tcp client (addr={}): read", self.addr);
        let (tx, rx) = oneshot::channel();
        let _ = self.tx.unbounded_send((None, tx));
        rx.map_err(|_| panic!("tcp read dropped"))
            .map(|res| res.map(|opt| opt.unwrap()))
            .wait()
            .unwrap()
    }

    pub fn write<T: Into<Vec<u8>>>(&self, buf: T) {
        println!("tcp client (addr={}): write", self.addr);
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
    let tname = format!("support tcp client (addr={})", addr);
    ::std::thread::Builder::new().name(tname).spawn(move || {
        let mut core = runtime::current_thread::Runtime::new()
            .expect("support tcp client runtime");

        let work = rx.for_each(|cb: oneshot::Sender<_>| {
            let fut = TcpStream::connect(&addr)
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

            current_thread::TaskExecutor::current()
                .execute(fut)
                .map_err(|e| {
                    println!("tcp client execute error: {:?}", e);
                })
                .map(|_| ())

        }).map_err(|e| println!("client error: {:?}", e));
        core.block_on(work).unwrap();
    }).unwrap();

    println!("tcp client (addr={}) thread running", addr);
    tx
}

fn run_server(tcp: TcpServer) -> server::Listening {
    let (tx, rx) = shutdown_signal();
    let (started_tx, started_rx) = oneshot::channel();
    let conn_count = Arc::new(AtomicUsize::from(0));
    let srv_conn_count = Arc::clone(&conn_count);
    let any_port = SocketAddr::from(([127, 0, 0, 1], 0));
    let std_listener = StdTcpListener::bind(&any_port).expect("bind");
    let addr = std_listener.local_addr().expect("local_addr");
    let tname = format!("support tcp server (addr={})", addr);
    ::std::thread::Builder::new().name(tname).spawn(move || {
        let mut core = runtime::current_thread::Runtime::new()
            .expect("support tcp server Runtime::new");

        let bind = TcpListener::from_std(
            std_listener,
            &reactor::Handle::current(),
        ).expect("TcpListener::from_std");


        let mut accepts = tcp.accepts;

        let listen = bind
            .incoming()
            .for_each(move |sock| {
                let cb = accepts.pop_front().expect("no more accepts");
                srv_conn_count.fetch_add(1, Ordering::Release);

                let fut = cb.call_box(sock);

                current_thread::TaskExecutor::current()
                    .execute(fut)
                    .map_err(|e| {
                        println!("tcp execute error: {:?}", e);
                        io::Error::from(io::ErrorKind::Other)
                    })
                    .map(|_| ())
            })
            .map_err(|e| panic!("tcp accept error: {}", e));

        core.spawn(listen);

        let _ = started_tx.send(());
        core.block_on(rx).unwrap();
    }).unwrap();

    started_rx.wait().expect("support tcp server started");

    // printlns will show if the test fails...
    println!("tcp server (addr={}): running", addr);

    server::Listening {
        addr,
        shutdown: tx,
        conn_count,
    }
}
