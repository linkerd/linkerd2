use support::*;

use self::futures::sync::{mpsc, oneshot};
use self::tokio_core::net::TcpStream;

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

// This sends `GET http://foo.com/ HTTP/1.1` instead of just `GET / HTTP/1.1`.
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
    tx: Sender,
    version: http::Version,
}

impl Client {
    fn new(addr: SocketAddr, authority: String, r: Run) -> Client {
        let v = match r {
            Run::Http1 { .. } => http::Version::HTTP_11,
            Run::Http2 => http::Version::HTTP_2,
        };
        Client {
            authority,
            tx: run(addr, r),
            version: v,
        }
    }

    pub fn get(&self, path: &str) -> String {
        let mut req = self.request_builder(path);
        let res = self.request(req.method("GET"));
        let stream = res.into_parts().1;
        stream.concat2()
            .map(|body| ::std::str::from_utf8(&body).unwrap().to_string())
            .wait()
            .unwrap()
    }

    pub fn request(&self, builder: &mut http::request::Builder) -> Response {
        let (tx, rx) = oneshot::channel();
        let _ = self.tx.unbounded_send((builder.body(()).unwrap(), tx));
        rx.map_err(|_| panic!("client request dropped"))
            .wait()
            .map(|result| result.unwrap())
            .unwrap()
    }

    pub fn request_builder(&self, path: &str) -> http::request::Builder {
        let mut b = Request::builder();
        b.uri(format!("http://{}{}", self.authority, path).as_str())
            .version(self.version);
        b
    }
}

enum Run {
    Http1 {
        absolute_uris: bool,
    },
    Http2,
}

fn run(addr: SocketAddr, version: Run) -> Sender {
    let (tx, rx) = mpsc::unbounded::<(Request, oneshot::Sender<Result<Response, String>>)>();

    ::std::thread::Builder::new().name("support client".into()).spawn(move || {
        let mut core = Core::new().unwrap();
        let reactor = core.handle();

        let conn = Conn(addr, reactor.clone());

        let work: Box<Future<Item=(), Error=()>> = match version {
            Run::Http1 { absolute_uris } => {
                let client = hyper::Client::configure()
                    .connector(conn)
                    .build(&reactor);
                Box::new(rx.for_each(move |(req, cb)| {
                    let mut req = hyper::Request::from(req.map(|()| hyper::Body::empty()));
                    if absolute_uris {
                        req.set_proxy(true);
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
                    reactor.spawn(fut);
                    Ok(())
                })
                    .map_err(|e| println!("client error: {:?}", e)))
            },
            Run::Http2 => {
                let h2 = tower_h2::client::Connect::<Conn, Handle, ()>::new(
                    conn,
                    Default::default(),
                    reactor.clone(),
                );

                Box::new(h2.new_service()
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
                            reactor.spawn(fut);
                            Ok(())
                        })
                    })
                    .map(|_| ())
                    .map_err(|e| println!("client error: {:?}", e)))
            }
        };

        core.run(work).unwrap();
    }).unwrap();
    tx
}

struct Conn(SocketAddr, Handle);

impl Connect for Conn {
    type Connected = TcpStream;
    type Error = ::std::io::Error;
    type Future = Box<Future<Item = TcpStream, Error = ::std::io::Error>>;

    fn connect(&self) -> Self::Future {
        let c = TcpStream::connect(&self.0, &self.1)
            .and_then(|tcp| tcp.set_nodelay(true).map(move |_| tcp));
        Box::new(c)
    }
}


impl hyper::client::Service for Conn {
    type Request = hyper::Uri;
    type Response = TcpStream;
    type Future = Box<Future<Item = TcpStream, Error = ::std::io::Error>>;
    type Error = ::std::io::Error;
    fn call(&self, _: hyper::Uri) -> <Self as hyper::client::Service>::Future {
        let c = TcpStream::connect(&self.0, &self.1)
            .and_then(|tcp| tcp.set_nodelay(true).map(move |_| tcp));
        Box::new(c)
    }
}
