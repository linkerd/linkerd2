use futures::future;
use hyper::{Body, Request, Response};
use std::net::SocketAddr;
use tokio::sync::watch;
use tracing::{info, instrument};

#[instrument(skip(ready))]
pub async fn serve(addr: SocketAddr, ready: watch::Receiver<bool>) -> Result<(), hyper::Error> {
    let server =
        hyper::server::Server::bind(&addr).serve(hyper::service::make_service_fn(move |_conn| {
            let ready = ready.clone();
            future::ok::<_, hyper::Error>(hyper::service::service_fn(
                move |req: hyper::Request<hyper::Body>| match req.uri().path() {
                    "/ready" => future::ok(handle_ready(&ready, req)),
                    _ => future::ok::<_, hyper::Error>(
                        hyper::Response::builder()
                            .status(hyper::StatusCode::NOT_FOUND)
                            .body(hyper::Body::default())
                            .unwrap(),
                    ),
                },
            ))
        }));
    let addr = server.local_addr();
    info!(%addr, "HTTP admin server listening");
    server.await
}

fn handle_ready(ready: &watch::Receiver<bool>, req: Request<Body>) -> Response<Body> {
    match *req.method() {
        hyper::Method::GET | hyper::Method::HEAD => {
            if *ready.borrow() {
                Response::builder()
                    .status(hyper::StatusCode::OK)
                    .header(hyper::header::CONTENT_TYPE, "text/plain")
                    .body("ready\n".into())
                    .unwrap()
            } else {
                Response::builder()
                    .status(hyper::StatusCode::INTERNAL_SERVER_ERROR)
                    .header(hyper::header::CONTENT_TYPE, "text/plain")
                    .body("not ready\n".into())
                    .unwrap()
            }
        }
        _ => Response::builder()
            .status(hyper::StatusCode::METHOD_NOT_ALLOWED)
            .body(Body::default())
            .unwrap(),
    }
}
