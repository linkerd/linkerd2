use support::*;

use self::futures::sync::{mpsc, oneshot};
use self::tokio_core::net::TcpStream;
use self::tower_h2::client::Error;

type Request = http::Request<()>;
type Response = http::Response<RecvBody>;
type Sender = mpsc::UnboundedSender<(Request, oneshot::Sender<Result<Response, Error>>)>;

pub fn new<T: Into<String>>(addr: SocketAddr, auth: T) -> Client {
    Client::new(addr, auth.into())
}

#[derive(Debug)]
pub struct Client {
    authority: String,
    tx: Sender,
}

impl Client {
    pub fn new(addr: SocketAddr, authority: String) -> Client {
        Client {
            authority,
            tx: run(addr),
        }
    }

    pub fn get(&self, path: &str) -> String {
        let (tx, rx) = oneshot::channel();
        let req = Request::builder()
            .method("GET")
            .uri(format!("http://{}{}", self.authority, path).as_str())
            .version(http::Version::HTTP_2)
            .body(())
            .unwrap();
        let _ = self.tx.unbounded_send((req, tx));
        rx.map_err(|_| panic!("client request dropped"))
            .and_then(|res| {
                let stream = RecvBodyStream(res.unwrap().into_parts().1);
                stream.concat2()
            })
            .map(|body| ::std::str::from_utf8(&body).unwrap().to_string())
            .wait()
            .unwrap()
    }
}

fn run(addr: SocketAddr) -> Sender {
    let (tx, rx) = mpsc::unbounded::<(Request, oneshot::Sender<Result<Response, Error>>)>();

    ::std::thread::Builder::new()
        .name("support client".into())
        .spawn(move || {
            let mut core = Core::new().unwrap();
            let reactor = core.handle();

            let conn = Conn(addr, reactor.clone());
            let h2 = tower_h2::Client::<Conn, Handle, ()>::new(
                conn,
                Default::default(),
                reactor.clone(),
            );

            let done = h2.new_service()
                .map_err(move |err| println!("connect error ({:?}): {:?}", addr, err))
                .and_then(move |mut h2| {
                    rx.for_each(move |(req, cb)| {
                        let fut = h2.call(req).then(|result| {
                            let _ = cb.send(result);
                            Ok(())
                        });
                        reactor.spawn(fut);
                        Ok(())
                    })
                })
                .map(|_| ())
                .map_err(|e| println!("client error: {:?}", e));

            core.run(done).unwrap();
        })
        .unwrap();
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
