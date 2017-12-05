use std::collections::HashMap;
use std::sync::Arc;

use support::*;

pub fn new() -> Server {
    Server::new()
}

#[derive(Debug)]
pub struct Server {
    routes: HashMap<String, String>,
}

#[derive(Debug)]
pub struct Listening {
    pub addr: SocketAddr,
    shutdown: Shutdown,
}

impl Server {
    pub fn new() -> Self {
        Server {
            routes: HashMap::new(),
        }
    }

    pub fn route(mut self, path: &str, resp: &str) -> Self {
        self.routes.insert(path.into(), resp.into());
        self
    }

    pub fn run(self) -> Listening {
        let (tx, rx) = shutdown_signal();
        let (addr_tx, addr_rx) = oneshot::channel();
        ::std::thread::Builder::new()
            .name("support server".into())
            .spawn(move || {
                let mut core = Core::new().unwrap();
                let reactor = core.handle();

                let h2 = tower_h2::Server::new(
                    NewSvc(Arc::new(self.routes)),
                    Default::default(),
                    reactor.clone(),
                );

                let addr = ([127, 0, 0, 1], 0).into();
                let bind = TcpListener::bind(&addr, &reactor).expect("bind");

                let local_addr = bind.local_addr().expect("local_addr");
                info!("bound listener, sending addr: {}", local_addr);
                let _ = addr_tx.send(local_addr);

                let serve = bind.incoming()
                    .fold((h2, reactor), |(h2, reactor), (sock, _)| {
                        if let Err(e) = sock.set_nodelay(true) {
                            return Err(e);
                        }

                        let serve = h2.serve(sock);
                        reactor.spawn(serve.map_err(|e| println!("server error: {:?}", e)));

                        Ok((h2, reactor))
                    });

                core.handle().spawn(
                    serve
                        .map(|_| ())
                        .map_err(|e| println!("server error: {}", e)),
                );

                info!("running");
                core.run(rx).unwrap();
            })
            .unwrap();

        info!("awaiting listening addr");
        let addr = addr_rx.wait().expect("addr");

        Listening {
            addr,
            shutdown: tx,
        }
    }
}

type Response = http::Response<RspBody>;

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

#[derive(Debug)]
struct Svc(Arc<HashMap<String, String>>);

impl Service for Svc {
    type Request = Request<RecvBody>;
    type Response = Response;
    type Error = h2::Error;
    type Future = future::FutureResult<Response, Self::Error>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        Ok(Async::Ready(()))
    }

    fn call(&mut self, req: Self::Request) -> Self::Future {
        let mut rsp = http::Response::builder();
        rsp.version(http::Version::HTTP_2);

        let path = req.uri().path();
        let rsp = match self.0.get(path) {
            Some(body) => {
                let body = RspBody::new(body.as_bytes().into());
                rsp.status(200).body(body).unwrap()
            }
            None => {
                println!("server 404: {:?}", path);
                let body = RspBody::empty();
                rsp.status(404).body(body).unwrap()
            }
        };
        future::ok(rsp)
    }
}

#[derive(Debug)]
struct NewSvc(Arc<HashMap<String, String>>);
impl NewService for NewSvc {
    type Request = Request<RecvBody>;
    type Response = Response;
    type Error = h2::Error;
    type InitError = ::std::io::Error;
    type Service = Svc;
    type Future = future::FutureResult<Svc, Self::InitError>;

    fn new_service(&self) -> Self::Future {
        future::ok(Svc(Arc::clone(&self.0)))
    }
}
