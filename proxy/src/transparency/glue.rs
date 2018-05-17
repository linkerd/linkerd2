use std::cell::RefCell;
use std::fmt;
use std::io;
use std::sync::Arc;

use bytes::{Bytes, IntoBuf};
use futures::{future, Async, Future, Poll, Stream};
use futures::future::Either;
use h2;
use http;
use hyper;
use hyper::client::connect as hyper_connect;
use tokio_connect::Connect;
use tower_service::{Service, NewService};
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
pub(super) struct BodyStream<B> {
    body: B,
}

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
    absolute_form: bool,
}

/// Future returned by `HyperConnect`.
pub(super) struct HyperConnectFuture<F> {
    inner: F,
    absolute_form: bool,
}

// ===== impl HttpBody =====

impl tower_h2::Body for HttpBody {
    type Data = Bytes;

    fn is_end_stream(&self) -> bool {
        match *self {
            HttpBody::Http1(ref b) => b.is_empty(),
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

impl hyper::body::Payload for HttpBody {
    type Data = <Bytes as IntoBuf>::Buf;
    type Error = h2::Error;

    fn is_end_stream(&self) -> bool {
        tower_h2::Body::is_end_stream(self)
    }

    fn poll_data(&mut self) -> Poll<Option<Self::Data>, h2::Error> {
        tower_h2::Body::poll_data(self).map(|async| {
            async.map(|opt|
                opt.map(Bytes::into_buf)
            )
        })
    }

    fn poll_trailers(&mut self) -> Poll<Option<http::HeaderMap>, h2::Error> {
        tower_h2::Body::poll_trailers(self)
    }
}


impl Default for HttpBody {
    fn default() -> HttpBody {
        HttpBody::Http2(Default::default())
    }
}

// ===== impl BodyStream =====

impl<B> BodyStream<B> {
    /// Wrap a `tower_h2::Body` into a `Stream` hyper can understand.
    pub fn new(body: B) -> Self {
        BodyStream {
            body,
        }
    }
}

impl<B> hyper::body::Payload for BodyStream<B>
where
    B: tower_h2::Body + Send + 'static,
    <B::Data as IntoBuf>::Buf: Send,
{
    type Data = <B::Data as IntoBuf>::Buf;
    type Error = h2::Error;

    #[inline]
    fn poll_data(&mut self) -> Poll<Option<Self::Data>, Self::Error> {
        self.body.poll_data().map(|async| {
            async.map(|opt|
                opt.map(B::Data::into_buf)
            )
        })
    }

    #[inline]
    fn is_end_stream(&self) -> bool {
        self.body.is_end_stream()
    }

    #[inline]
    fn poll_trailers(&mut self) -> Poll<Option<http::HeaderMap>, Self::Error> {
        self.body.poll_trailers()
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

impl<S, B> hyper::service::Service for HyperServerSvc<S>
where
    S: Service<
        Request=http::Request<HttpBody>,
        Response=http::Response<B>,
    >,
    S::Error: fmt::Debug,
    B: tower_h2::Body + Default + Send + 'static,
    <B::Data as ::bytes::IntoBuf>::Buf: Send,
{
    type ReqBody = hyper::Body;
    type ResBody = BodyStream<B>;
    type Error = h2::Error;
    type Future = Either<
        HyperServerSvcFuture<S::Future>,
        future::FutureResult<http::Response<Self::ResBody>, Self::Error>,
    >;

    fn call(&mut self, req: http::Request<Self::ReqBody>) -> Self::Future {
        if let &hyper::Method::CONNECT = req.method() {
            debug!("HTTP/1.1 CONNECT not supported");
            let res = hyper::Response::builder()
                .status(hyper::StatusCode::BAD_GATEWAY)
                .body(BodyStream::new(Default::default()))
                .expect("building response with empty body should not error!");
            return Either::B(future::ok(res));
        }
        let mut req = req;
        req.extensions_mut().insert(self.srv_ctx.clone());

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
    type Item = hyper::Response<BodyStream<B>>;
    type Error = h2::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let mut res = try_ready!(self.inner.poll().map_err(|e| {
            debug!("h2 error: {:?}", e);
            h2::Error::from(io::Error::from(io::ErrorKind::Other))
        }));

        h1::strip_connection_headers(res.headers_mut());
        Ok(Async::Ready(res.map(BodyStream::new).into()))
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
    pub fn new(connect: C, absolute_form: bool) -> Self {
        HyperConnect {
            connect,
            absolute_form,
        }
    }
}

impl<C> hyper_connect::Connect for HyperConnect<C>
where
    C: Connect + Send + Sync,
    C::Future: Send + 'static,
    C::Connected: Send + 'static,
{
    type Transport = C::Connected;
    type Error = io::Error;
    type Future = HyperConnectFuture<C::Future>;

    fn connect(&self, _dst: hyper_connect::Destination) -> Self::Future {
        HyperConnectFuture {
            inner: self.connect.connect(),
            absolute_form: self.absolute_form,
        }
    }
}

impl<F> Future for HyperConnectFuture<F>
where
    F: Future,
{
    type Item = (F::Item, hyper_connect::Connected);
    type Error = io::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let transport = try_ready!(
            self.inner.poll()
                .map_err(|_| io::Error::from(io::ErrorKind::Other))
        );
        let connected = hyper_connect::Connected::new()
            .proxy(self.absolute_form);
        Ok(Async::Ready((transport, connected)))
    }
}
