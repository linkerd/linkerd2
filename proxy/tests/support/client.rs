use support::*;

use std::io;
use std::cell::RefCell;

use self::futures::{
    future::Executor,
    sync::{mpsc, oneshot},
};
use self::tokio::{
    net::TcpStream,
    io::{AsyncRead, AsyncWrite},
};

type Request = http::Request<()>;
type Response = http::Response<BodyStream>;
type BodyStream = Box<Stream<Item=Bytes, Error=String> + Send>;
type Sender = mpsc::UnboundedSender<(Request, oneshot::Sender<Result<Response, String>>)>;

pub fn new<T: Into<String>>(addr: SocketAddr, auth: T) -> Client {
    http2(addr, auth.into())
}

pub fn http1<T: Into<String>>(addr: SocketAddr, auth: T) -> Client {
    Client::new(addr, auth.into(), Run::Http1 {
        absolute_uris: false,
    })
}

/// This sends `GET http://foo.com/ HTTP/1.1` instead of just `GET / HTTP/1.1`.
pub fn http1_absolute_uris<T: Into<String>>(addr: SocketAddr, auth: T) -> Client {
    Client::new(addr, auth.into(), Run::Http1 {
        absolute_uris: true,
    })
}

pub fn http2<T: Into<String>>(addr: SocketAddr, auth: T) -> Client {
    Client::new(addr, auth.into(), Run::Http2)
}

pub fn tcp(addr: SocketAddr) -> tcp::TcpClient {
    tcp::client(addr)
}

pub struct Client {
    authority: String,
    /// This is a future that completes when the associated connection for
    /// this Client has been dropped.
    running: Running,
    tx: Sender,
    version: http::Version,
}

impl Client {
    fn new(addr: SocketAddr, authority: String, r: Run) -> Client {
        let v = match r {
            Run::Http1 { .. } => http::Version::HTTP_11,
            Run::Http2 => http::Version::HTTP_2,
        };
        let (tx, running) = run(addr, r);
        Client {
            authority,
            running,
            tx,
            version: v,
        }
    }

    pub fn get(&self, path: &str) -> String {
        let mut req = self.request_builder(path);
        let res = self.request(req.method("GET"));
        assert_eq!(
            res.status(),
            StatusCode::OK,
            "client.get({:?}) expects 200 OK, got \"{}\"",
            path,
            res.status(),
        );
        let stream = res.into_parts().1;
        stream.concat2()
            .map(|body| ::std::str::from_utf8(&body).unwrap().to_string())
            .wait()
            .expect("get() wait body")
    }

    pub fn request_async(&self, builder: &mut http::request::Builder) -> Box<Future<Item=Response, Error=String> + Send> {
        let (tx, rx) = oneshot::channel();
        let _ = self.tx.unbounded_send((builder.body(()).unwrap(), tx));
        Box::new(rx.then(|oneshot_result| oneshot_result.expect("request canceled")))
    }

    pub fn request(&self, builder: &mut http::request::Builder) -> Response {
        self.request_async(builder)
            .wait()
            .expect("response")
    }

    pub fn request_builder(&self, path: &str) -> http::request::Builder {
        let mut b = Request::builder();
        b.uri(format!("http://{}{}", self.authority, path).as_str())
            .version(self.version);
        b
    }

    pub fn wait_for_closed(self) {
        self.running
            .wait()
            .expect("wait_for_closed");
    }
}

enum Run {
    Http1 {
        absolute_uris: bool,
    },
    Http2,
}

fn run(addr: SocketAddr, version: Run) -> (Sender, Running) {
    let (tx, rx) = mpsc::unbounded::<(Request, oneshot::Sender<Result<Response, String>>)>();
    let (running_tx, running_rx) = running();

    ::std::thread::Builder::new()
        .name("support client".into())
        .spawn(move || {
        let mut runtime = runtime::current_thread::Runtime::new()
            .expect("initialize support client runtime");

        let absolute_uris = if let Run::Http1 { absolute_uris } = version {
            absolute_uris
        } else {
            false
        };
        let conn = Conn {
            addr,
            running: RefCell::new(Some(running_tx)),
            absolute_uris,
        };

        let work: Box<Future<Item=(), Error=()>> = match version {
            Run::Http1 { .. } => {
                let client = hyper::Client::builder()
                    .build::<Conn, hyper::Body>(conn);
                Box::new(rx.for_each(move |(req, cb)| {
                    let req = req.map(|_| hyper::Body::empty());
                    if req.headers().get(http::header::CONTENT_LENGTH).is_none() {
                        // assert!(req.body_mut().take().unwrap().is_empty());
                    }
                    let fut = client.request(req).then(move |result| {
                        let result = result
                            .map(|res| {
                                let res = http::Response::from(res);
                                res.map(|body| -> BodyStream {
                                    Box::new(body.map(|chunk| chunk.into())
                                        .map_err(|e| e.to_string()))
                                })
                            })
                            .map_err(|e| e.to_string());
                        let _ = cb.send(result);
                        Ok(())
                    });
                    current_thread::TaskExecutor::current().execute(fut)
                        .map_err(|e| println!("client spawn error: {:?}", e))
                })
                    .map_err(|e| println!("client error: {:?}", e)))
            },
            Run::Http2 => {
                let connect = tower_h2::client::Connect::new(
                    conn,
                    Default::default(),
                    LazyExecutor,
                );

                Box::new(connect.new_service()
                    .map_err(move |err| println!("connect error ({:?}): {:?}", addr, err))
                    .and_then(move |mut h2| {
                        rx.for_each(move |(req, cb)| {
                            let fut = h2.call(req).then(|result| {
                                let result = result
                                    .map(|res| {
                                        res.map(|body| -> BodyStream {
                                            Box::new(RecvBodyStream(body).map_err(|e| format!("{:?}", e)))
                                        })
                                    })
                                    .map_err(|e| format!("{:?}", e));
                                let _ = cb.send(result);
                                Ok(())
                            });
                            current_thread::TaskExecutor::current().execute(fut)
                                .map_err(|e| println!("client spawn error: {:?}", e))
                        })
                    })
                    .map(|_| ())
                    .map_err(|e| println!("client error: {:?}", e)))
            }
        };

        runtime.block_on(work).expect("support client runtime");
    }).unwrap();
    (tx, running_rx)
}

/// The "connector". Clones `running` into new connections, so we can signal
/// when all connections are finally closed.
struct Conn {
    addr: SocketAddr,
    /// When this Sender drops, that should mean the connection is closed.
    running: RefCell<Option<oneshot::Sender<()>>>,
    absolute_uris: bool,
}

impl Conn {
    fn connect_(&self) -> Box<Future<Item = RunningIo, Error = ::std::io::Error> + Send> {
        let running = self.running
            .borrow_mut()
            .take()
            .expect("connected more than once");
        let c = TcpStream::connect(&self.addr)
            .and_then(|tcp| tcp.set_nodelay(true).map(move |_| tcp))
            .map(move |tcp| RunningIo {
                inner: tcp,
                running: running,
            });
        Box::new(c)
    }
}

impl Connect for Conn {
    type Connected = RunningIo;
    type Error = ::std::io::Error;
    type Future = Box<Future<Item = Self::Connected, Error = ::std::io::Error>>;

    fn connect(&self) -> Self::Future {
        self.connect_()
    }
}

impl hyper::client::connect::Connect for Conn {
    type Transport = RunningIo;
    type Future = Box<Future<
        Item = (Self::Transport, hyper::client::connect::Connected),
        Error = ::std::io::Error,
    > + Send>;
    type Error = ::std::io::Error;
    fn connect(&self, _: hyper::client::connect::Destination) -> Self::Future {
        let connected = hyper::client::connect::Connected::new()
            .proxy(self.absolute_uris);
        Box::new(self.connect_().map(|t| (t, connected)))
    }
}

// Hyper requires that implementors of `Connect` be `Sync`; and the `RefCell`
// in `Conn` makes it `!Sync`. However, since we're using the current thread
// executor, we know this type will never be sent between threads.
// TODO: I would really prefer to not have to do this.
unsafe impl Sync for Conn {}

/// A wrapper around a TcpStream, allowing us to signal when the connection
/// is dropped.
struct RunningIo {
    inner: TcpStream,
    /// When this drops, the related Receiver is notified that the connection
    /// is closed.
    running: oneshot::Sender<()>,
}

impl io::Read for RunningIo {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        self.inner.read(buf)
    }
}

impl io::Write for RunningIo {
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        self.inner.write(buf)
    }

    fn flush(&mut self) -> io::Result<()> {
        self.inner.flush()
    }
}

impl AsyncRead for RunningIo {}

impl AsyncWrite for RunningIo {
    fn shutdown(&mut self) -> Poll<(), io::Error> {
        AsyncWrite::shutdown(&mut self.inner)
    }
}

