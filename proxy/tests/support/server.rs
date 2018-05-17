use std::collections::HashMap;
use std::io;
use std::sync::Arc;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::thread;

use support::*;

pub fn new() -> Server {
    http2()
}

pub fn http1() -> Server {
    Server::http1()
}

pub fn http2() -> Server {
    Server::http2()
}

pub fn tcp() -> tcp::TcpServer {
    tcp::server()
}

pub struct Server {
    routes: HashMap<String, Route>,
    version: Run,
}

pub struct Listening {
    pub addr: SocketAddr,
    pub(super) shutdown: Shutdown,
    pub(super) conn_count: Arc<AtomicUsize>,
}

impl Listening {
    pub fn connections(&self) -> usize {
        self.conn_count.load(Ordering::Acquire)
    }
}

impl Drop for Listening {
    fn drop(&mut self) {
        println!("server Listening dropped; addr={}", self.addr);
    }
}

impl Server {
    fn new(run: Run) -> Self {
        Server {
            routes: HashMap::new(),
            version: run,
        }
    }
    fn http1() -> Self {
        Server::new(Run::Http1)
    }

    fn http2() -> Self {
        Server::new(Run::Http2)
    }

    /// Return a string body as a 200 OK response, with the string as
    /// the response body.
    pub fn route(mut self, path: &str, resp: &str) -> Self {
        self.routes.insert(path.into(), Route::string(resp));
        self
    }

    /// Return a 200 OK response with no body when the path matches.
    pub fn route_empty_ok(self, path: &str) -> Self {
        self.route_fn(path, |_| {
            Response::builder()
                .header("content-length", "0")
                .body(Default::default())
                .unwrap()
        })
    }

    /// Call a closure when the request matches, returning a response
    /// to send back.
    pub fn route_fn<F>(mut self, path: &str, cb: F) -> Self
    where
        F: Fn(Request<()>) -> Response<Bytes> + Send + 'static,
    {
        self.routes.insert(path.into(), Route(Box::new(cb)));
        self
    }

    pub fn route_with_latency(
        mut self,
        path: &str,
        resp: &str,
        latency: Duration
    ) -> Self {
        let resp = Bytes::from(resp);
        let route = Route(Box::new(move |_| {
            thread::sleep(latency);
            http::Response::builder()
                .status(200)
                .body(resp.clone())
                .unwrap()
        }));
        self.routes.insert(path.into(), route);
        self
    }

    pub fn delay_listen<F>(self, f: F) -> Listening
    where
        F: Future<Item=(), Error=()> + Send + 'static,
    {
        self.run_inner(Some(Box::new(f.then(|_| Ok(())))))
    }

    pub fn run(self) -> Listening {
        self.run_inner(None)
    }

    fn run_inner(self, delay: Option<Box<Future<Item=(), Error=()> + Send>>) -> Listening {
        let (tx, rx) = shutdown_signal();
        let (listening_tx, listening_rx) = oneshot::channel();
        let mut listening_tx = Some(listening_tx);
        let conn_count = Arc::new(AtomicUsize::from(0));
        let srv_conn_count = Arc::clone(&conn_count);
        let version = self.version;
        let tname = format!(
            "support {:?} server (test={})",
            version,
            thread_name(),
        );


        let addr = SocketAddr::from(([127, 0, 0, 1], 0));
        let listener = net2::TcpBuilder::new_v4().expect("Tcp::new_v4");
        listener.bind(addr).expect("Tcp::bind");
        let addr = listener.local_addr().expect("Tcp::local_addr");

        ::std::thread::Builder::new()
            .name(tname)
            .spawn(move || {
                if let Some(delay) = delay {
                    let _ = listening_tx.take().unwrap().send(());
                    delay.wait().expect("support server delay wait");
                }
                let listener = listener.listen(1024).expect("Tcp::listen");

                let mut runtime = runtime::current_thread::Runtime::new()
                    .expect("initialize support server runtime");

                let new_svc = NewSvc(Arc::new(self.routes));

                let srv: Box<Fn(TcpStream) -> Box<Future<Item=(), Error=()>>> = match self.version {
                    Run::Http1 => {
                        let h1 = hyper::server::conn::Http::new();

                        Box::new(move |sock| {
                            let h1_clone = h1.clone();
                            let srv_conn_count = Arc::clone(&srv_conn_count);
                            let conn = new_svc.new_service()
                                .inspect(move |_| {
                                    srv_conn_count.fetch_add(1, Ordering::Release);
                                })
                                .map_err(|e| println!("server new_service error: {}", e))
                                .and_then(move |svc|
                                    h1_clone.serve_connection(sock, svc)
                                        .map_err(|e| println!("server h1 error: {}", e))
                                )
                                .map(|_| ());
                            Box::new(conn)
                        })
                    },
                    Run::Http2 => {
                        let h2 = tower_h2::Server::new(
                            new_svc,
                            Default::default(),
                            LazyExecutor,
                        );
                        Box::new(move |sock| {
                            let srv_conn_count = Arc::clone(&srv_conn_count);
                            let conn = h2.serve(sock)
                                .map_err(|e| println!("server h2 error: {:?}", e))
                                .inspect(move |_| {
                                    srv_conn_count.fetch_add(1, Ordering::Release);
                                });
                            Box::new(conn)
                        })
                    },
                };

                let bind = TcpListener::from_std(
                    listener,
                    &reactor::Handle::current()
                ).expect("from_std");

                if let Some(listening_tx) = listening_tx {
                    let _ = listening_tx.send(());
                }

                let serve = bind.incoming()
                    .fold(srv, move |srv, sock| {
                        if let Err(e) = sock.set_nodelay(true) {
                            return Err(e);
                        }
                        current_thread::TaskExecutor::current()
                            .execute(srv(sock))
                            .map_err(|e| {
                                println!("server execute error: {:?}", e);
                                io::Error::from(io::ErrorKind::Other)
                            })
                            .map(|_| srv)
                    });

                runtime.spawn(
                    Box::new(serve
                        .map(|_| ())
                        .map_err(|e| println!("server error: {}", e))
                    )
                );

                runtime.block_on(rx).expect("block on");
            }).unwrap();

        listening_rx.wait().expect("listening_rx");

        // printlns will show if the test fails...
        println!(
            "{:?} server running; addr={}",
            version,
            addr,
        );

        Listening {
            addr,
            shutdown: tx,
            conn_count,
        }
    }
}

#[derive(Clone, Copy, Debug)]
enum Run {
    Http1,
    Http2,
}

struct RspBody(Option<Bytes>);

impl RspBody {
    fn new(body: Bytes) -> Self {
        RspBody(Some(body))
    }

    fn empty() -> Self {
        RspBody(None)
    }
}


impl Body for RspBody {
    type Data = Bytes;

    fn is_end_stream(&self) -> bool {
        self.0.as_ref().map(|b| b.is_empty()).unwrap_or(false)
    }

    fn poll_data(&mut self) -> Poll<Option<Bytes>, h2::Error> {
        let data = self.0
            .take()
            .and_then(|b| if b.is_empty() { None } else { Some(b) });
        Ok(Async::Ready(data))
    }
}

struct Route(Box<Fn(Request<()>) -> Response<Bytes> + Send>);

impl Route {
    fn string(body: &str) -> Route {
        let body = Bytes::from(body);
        Route(Box::new(move |_| {
            http::Response::builder()
                .status(200)
                .body(body.clone())
                .unwrap()
        }))
    }
}

impl ::std::fmt::Debug for Route {
    fn fmt(&self, f: &mut ::std::fmt::Formatter) -> ::std::fmt::Result {
        f.write_str("Route")
    }
}

#[derive(Debug)]
struct Svc(Arc<HashMap<String, Route>>);

impl Service for Svc {
    type Request = Request<RecvBody>;
    type Response = Response<RspBody>;
    type Error = h2::Error;
    type Future = future::FutureResult<Self::Response, Self::Error>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        Ok(Async::Ready(()))
    }

    fn call(&mut self, req: Self::Request) -> Self::Future {
        let rsp = match self.0.get(req.uri().path()) {
            Some(route) => {
                (route.0)(req.map(|_| ()))
                    .map(|s| RspBody::new(s))
            }
            None => {
                println!("server 404: {:?}", req.uri().path());
                let mut rsp = http::Response::builder();
                rsp.version(http::Version::HTTP_2);
                let body = RspBody::empty();
                rsp.status(404).body(body).unwrap()
            }
        };
        future::ok(rsp)
    }
}

impl hyper::service::Service for Svc {
    type ReqBody = hyper::Body;
    type ResBody = hyper::Body;
    type Error = http::Error;
    type Future = future::FutureResult<hyper::Response<hyper::Body>, Self::Error>;

    fn call(&mut self, req: hyper::Request<hyper::Body>) -> Self::Future {

        let rsp = match self.0.get(req.uri().path()) {
            Some(route) => {
                let rsp = (route.0)(Request::from(req).map(|_| ()))
                    .map(|s| hyper::Body::from(s));
                Ok(rsp)
            }
            None => {
                println!("server 404: {:?}", req.uri().path());
                let mut rsp = hyper::Response::builder();
                let body = hyper::Body::empty();
                rsp.status(StatusCode::NOT_FOUND)
                    .body(body)
            }
        };

        future::result(rsp)
    }
}

#[derive(Debug)]
struct NewSvc(Arc<HashMap<String, Route>>);
impl NewService for NewSvc {
    type Request = Request<RecvBody>;
    type Response = Response<RspBody>;
    type Error = h2::Error;
    type InitError = ::std::io::Error;
    type Service = Svc;
    type Future = future::FutureResult<Svc, Self::InitError>;

    fn new_service(&self) -> Self::Future {
        future::ok(Svc(Arc::clone(&self.0)))
    }
}
