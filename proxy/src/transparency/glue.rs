use std::cell::RefCell;
use std::fmt;
use std::io;
use std::sync::Arc;

use bytes::Bytes;
use futures::{future, Async, Future, Poll, Stream};
use futures::future::Either;
use h2;
use http;
use hyper;
use tokio_connect::Connect;
use tower::{Service, NewService};
use tower_h2;

use ctx::transport::{Server as ServerCtx};
use super::h1;

/// Glue between `hyper::Body` and `tower_h2::RecvBody`.
#[derive(Debug)]
pub enum HttpBody {
    Http1(hyper::Body),
    Http2(tower_h2::RecvBody),
}

/// Glue for `tower_h2::Body`s to be used in hyper.
#[derive(Debug)]
pub(super) struct BodyStream<B>(pub(super) B);

/// Glue for the `Data` part of a `tower_h2::Body` to be used as an `AsRef` in `BodyStream`.
#[derive(Debug)]
pub(super) struct BufAsRef<B>(B);

/// Glue for a `tower::Service` to used as a `hyper::server::Service`.
#[derive(Debug)]
pub(super) struct HyperServerSvc<S> {
    service: RefCell<S>,
    srv_ctx: Arc<ServerCtx>,
}

/// Future returned by `HyperServerSvc`.
pub(super) struct HyperServerSvcFuture<F> {
    inner: F,
}

/// Glue for any `Service` taking an h2 body to receive an `HttpBody`.
#[derive(Debug)]
pub(super) struct HttpBodySvc<S> {
    service: S,
}

/// Glue for any `NewService` taking an h2 body to receive an `HttpBody`.
#[derive(Clone)]
pub(super) struct HttpBodyNewSvc<N> {
    new_service: N,
}

/// Future returned by `HttpBodyNewSvc`.
pub(super) struct HttpBodyNewSvcFuture<F> {
    inner: F,
}

/// Glue for any `tokio_connect::Connect` to implement `hyper::client::Connect`.
#[derive(Debug, Clone)]
pub(super) struct HyperConnect<C> {
    connect: C,
}

/// Future returned by `HyperConnect`.
pub(super) struct HyperConnectFuture<F> {
    inner: F,
}

// ===== impl HttpBody =====

impl tower_h2::Body for HttpBody {
    type Data = Bytes;

    fn is_end_stream(&self) -> bool {
        match *self {
            HttpBody::Http1(_) => false,
            HttpBody::Http2(ref b) => b.is_end_stream(),
        }
    }

    fn poll_data(&mut self) -> Poll<Option<Self::Data>, h2::Error> {
        match *self {
            HttpBody::Http1(ref mut b) => {
                match b.poll() {
                    Ok(Async::Ready(Some(chunk))) => Ok(Async::Ready(Some(chunk.into()))),
                    Ok(Async::Ready(None)) => Ok(Async::Ready(None)),
                    Ok(Async::NotReady) => Ok(Async::NotReady),
                    Err(e) => {
                        debug!("http/1 body error: {}", e);
                        Err(h2::Reason::INTERNAL_ERROR.into())
                    }
                }
            },
            HttpBody::Http2(ref mut b) => b.poll_data().map(|async| async.map(|opt| opt.map(|data| data.into()))),
        }
    }

    fn poll_trailers(&mut self) -> Poll<Option<http::HeaderMap>, h2::Error> {
        match *self {
            HttpBody::Http1(_) => Ok(Async::Ready(None)),
            HttpBody::Http2(ref mut b) => b.poll_trailers(),
        }
    }
}

impl Default for HttpBody {
    fn default() -> HttpBody {
        HttpBody::Http2(Default::default())
    }
}

// ===== impl BodyStream =====

impl<B> Stream for BodyStream<B>
where
    B: tower_h2::Body,
{
    type Item = BufAsRef<<B::Data as ::bytes::IntoBuf>::Buf>;
    type Error = hyper::Error;

    fn poll(&mut self) -> Poll<Option<Self::Item>, Self::Error> {
        self.0.poll_data()
            .map(|async| async.map(|opt| opt.map(|buf| BufAsRef(::bytes::IntoBuf::into_buf(buf)))))
            .map_err(|e| {
                trace!("h2 body error: {:?}", e);
                hyper::Error::Io(io::ErrorKind::Other.into())
            })
    }
}

// ===== impl BufAsRef =====

impl<B: ::bytes::Buf> AsRef<[u8]> for BufAsRef<B> {
    fn as_ref(&self) -> &[u8] {
        ::bytes::Buf::bytes(&self.0)
    }
}

// ===== impl HyperServerSvc =====

impl<S> HyperServerSvc<S> {
    pub fn new(svc: S, ctx: Arc<ServerCtx>) -> Self {
        HyperServerSvc {
            service: RefCell::new(svc),
            srv_ctx: ctx,
        }
    }
}

impl<S, B> hyper::server::Service for HyperServerSvc<S>
where
    S: Service<
        Request=http::Request<HttpBody>,
        Response=http::Response<B>,
    >,
    S::Error: fmt::Debug,
    B: tower_h2::Body + 'static,
{
    type Request = hyper::server::Request;
    type Response = hyper::server::Response<BodyStream<B>>;
    type Error = hyper::Error;
    type Future = Either<
        HyperServerSvcFuture<S::Future>,
        future::FutureResult<Self::Response, Self::Error>,
    >;

    fn call(&self, req: Self::Request) -> Self::Future {
        if let &hyper::Method::Connect = req.method() {
            debug!("HTTP/1.1 CONNECT not supported");
            let res = hyper::Response::new()
                .with_status(hyper::StatusCode::BadGateway);
            return Either::B(future::ok(res));

        }

        let mut req: http::Request<hyper::Body> = req.into();
        req.extensions_mut().insert(self.srv_ctx.clone());

        if let Err(()) = h1::reconstruct_uri(&mut req) {
            let res = hyper::Response::new()
                .with_status(hyper::BadRequest);
            return Either::B(future::ok(res));
        }
        h1::strip_connection_headers(req.headers_mut());

        let req = req.map(|b| HttpBody::Http1(b));
        let f = HyperServerSvcFuture {
            inner: self.service.borrow_mut().call(req),
        };
        Either::A(f)
    }
}

impl<F, B> Future for HyperServerSvcFuture<F>
where
    F: Future<Item=http::Response<B>>,
    F::Error: fmt::Debug,
{
    type Item = hyper::server::Response<BodyStream<B>>;
    type Error = hyper::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let mut res = try_ready!(self.inner.poll().map_err(|e| {
            debug!("h2 error: {:?}", e);
            hyper::Error::Io(io::ErrorKind::Other.into())
        }));

        if res.status() == http::StatusCode::SWITCHING_PROTOCOLS {
            debug!("HTTP/1.1 101 upgrade not supported");
            let res = hyper::Response::new()
                .with_status(hyper::StatusCode::BadGateway);
            return Ok(Async::Ready(res));
        }
        h1::strip_connection_headers(res.headers_mut());
        Ok(Async::Ready(res.map(BodyStream).into()))
    }
}

// ==== impl HttpBodySvc ====


impl<S> Service for HttpBodySvc<S>
where
    S: Service<
        Request=http::Request<HttpBody>,
    >,
{
    type Request = http::Request<tower_h2::RecvBody>;
    type Response = S::Response;
    type Error = S::Error;
    type Future = S::Future;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        self.service.poll_ready()
    }

    fn call(&mut self, req: Self::Request) -> Self::Future {
        self.service.call(req.map(|b| HttpBody::Http2(b)))
    }
}

impl<N> HttpBodyNewSvc<N>
where
    N: NewService<Request=http::Request<HttpBody>>,
{
    pub fn new(new_service: N) -> Self {
        HttpBodyNewSvc {
            new_service,
        }
    }
}

impl<N> NewService for HttpBodyNewSvc<N>
where
    N: NewService<Request=http::Request<HttpBody>>,
{
    type Request = http::Request<tower_h2::RecvBody>;
    type Response = N::Response;
    type Error = N::Error;
    type Service = HttpBodySvc<N::Service>;
    type InitError = N::InitError;
    type Future = HttpBodyNewSvcFuture<N::Future>;

    fn new_service(&self) -> Self::Future {
        HttpBodyNewSvcFuture {
            inner: self.new_service.new_service(),
        }
    }
}

impl<F> Future for HttpBodyNewSvcFuture<F>
where
    F: Future,
{
    type Item = HttpBodySvc<F::Item>;
    type Error = F::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let s = try_ready!(self.inner.poll());
        Ok(Async::Ready(HttpBodySvc {
            service: s,
        }))
    }
}

// ===== impl HyperConnect =====

impl<C> HyperConnect<C>
where
    C: Connect,
    C::Future: 'static,
{
    pub fn new(connect: C) -> Self {
        HyperConnect {
            connect,
        }
    }
}

impl<C> hyper::client::Service for HyperConnect<C>
where
    C: Connect,
    C::Future: 'static,
{
    type Request = hyper::Uri;
    type Response = C::Connected;
    type Error = io::Error;
    type Future = HyperConnectFuture<C::Future>;

    fn call(&self, _uri: Self::Request) -> Self::Future {
        HyperConnectFuture {
            inner: self.connect.connect(),
        }
    }
}

impl<F> Future for HyperConnectFuture<F>
where
    F: Future,
{
    type Item = F::Item;
    type Error = io::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        self.inner.poll()
            .map_err(|_| io::ErrorKind::Other.into())
    }
}
