use std::fmt;
use std::io;
use std::sync::Arc;

use bytes::{Bytes, IntoBuf};
use futures::{future, Async, Future, Poll};
use futures::future::Either;
use h2;
use http;
use hyper::{self, body::Payload};
use hyper::client::connect as hyper_connect;
use tokio_connect::Connect;
use tower_service::{Service, NewService};
use tower_h2;

use ctx::transport::{Server as ServerCtx};
use drain;
use super::h1;
use super::upgrade::Http11Upgrade;
use task::{BoxSendFuture, ErasedExecutor, Executor};

/// Glue between `hyper::Body` and `tower_h2::RecvBody`.
#[derive(Debug)]
pub enum HttpBody {
    Http1 {
        /// In HttpBody::drop, if this was an HTTP upgrade, the body is taken
        /// to be inserted into the Http11Upgrade half.
        body: Option<hyper::Body>,
        upgrade: Option<Http11Upgrade>
    },
    Http2(tower_h2::RecvBody),
}

/// Glue for `tower_h2::Body`s to be used in hyper.
#[derive(Debug, Default)]
pub(super) struct BodyPayload<B> {
    body: B,
}

/// Glue for a `tower::Service` to used as a `hyper::server::Service`.
#[derive(Debug)]
pub(super) struct HyperServerSvc<S, E> {
    service: S,
    srv_ctx: Arc<ServerCtx>,
    /// Watch any spawned HTTP/1.1 upgrade tasks.
    upgrade_drain_signal: drain::Watch,
    /// Executor used to spawn HTTP/1.1 upgrade tasks, and TCP proxies
    /// after they succeed.
    upgrade_executor: E,
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
        match self {
            HttpBody::Http1 { body, .. } => {
                body
                    .as_ref()
                    .expect("only taken in drop")
                    .is_end_stream()
            },
            HttpBody::Http2(b) => b.is_end_stream(),
        }
    }

    fn poll_data(&mut self) -> Poll<Option<Self::Data>, h2::Error> {
        match self {
            HttpBody::Http1 { body, .. } => {
                match body.as_mut().expect("only taken in drop").poll_data() {
                    Ok(Async::Ready(Some(chunk))) => Ok(Async::Ready(Some(chunk.into()))),
                    Ok(Async::Ready(None)) => Ok(Async::Ready(None)),
                    Ok(Async::NotReady) => Ok(Async::NotReady),
                    Err(e) => {
                        debug!("http/1 body error: {}", e);
                        Err(h2::Reason::INTERNAL_ERROR.into())
                    }
                }
            },
            HttpBody::Http2(b) => b.poll_data()
                .map(|async| {
                    async.map(|opt| {
                        opt.map(Bytes::from)
                    })
                })
        }
    }

    fn poll_trailers(&mut self) -> Poll<Option<http::HeaderMap>, h2::Error> {
        match self {
            HttpBody::Http1 { .. } => Ok(Async::Ready(None)),
            HttpBody::Http2(b) => b.poll_trailers(),
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

impl Drop for HttpBody {
    fn drop(&mut self) {
        // If HTTP/1, and an upgrade was wanted, send the upgrade future.
        match self {
            HttpBody::Http1 { body, upgrade } => {
                if let Some(upgrade) = upgrade.take() {
                    let on_upgrade = body
                        .take()
                        .expect("take only on drop")
                        .on_upgrade();
                    upgrade.insert_half(on_upgrade);
                }
            },
            HttpBody::Http2(_) => (),
        }
    }
}

// ===== impl BodyPayload =====

impl<B> BodyPayload<B> {
    /// Wrap a `tower_h2::Body` into a `Stream` hyper can understand.
    pub fn new(body: B) -> Self {
        BodyPayload {
            body,
        }
    }
}

impl<B> hyper::body::Payload for BodyPayload<B>
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

impl<S, E> HyperServerSvc<S, E> {
    pub fn new(
        service: S,
        srv_ctx: Arc<ServerCtx>,
        upgrade_drain_signal: drain::Watch,
        upgrade_executor: E,
    ) -> Self {
        HyperServerSvc {
            service,
            srv_ctx,
            upgrade_drain_signal,
            upgrade_executor,
        }
    }
}

impl<S, E, B> hyper::service::Service for HyperServerSvc<S, E>
where
    S: Service<
        Request=http::Request<HttpBody>,
        Response=http::Response<B>,
    >,
    S::Error: fmt::Debug,
    B: tower_h2::Body + Default + Send + 'static,
    <B::Data as ::bytes::IntoBuf>::Buf: Send,
    E: Executor<BoxSendFuture> + Clone + Send + Sync + 'static,
{
    type ReqBody = hyper::Body;
    type ResBody = BodyPayload<B>;
    type Error = h2::Error;
    type Future = Either<
        HyperServerSvcFuture<S::Future>,
        future::FutureResult<http::Response<Self::ResBody>, Self::Error>,
    >;

    fn call(&mut self, mut req: http::Request<Self::ReqBody>) -> Self::Future {
        // Should this rejection happen later in the Service stack?
        //
        // Rejecting here means telemetry doesn't record anything about it...
        //
        // At the same time, this stuff is specifically HTTP1, so it feels
        // proper to not have the HTTP2 requests going through it...
        if h1::is_bad_request(&req) {
            let mut res = http::Response::default();
            *res.status_mut() = http::StatusCode::BAD_REQUEST;
            return Either::B(future::ok(res));
        }


        req.extensions_mut().insert(self.srv_ctx.clone());

        let upgrade = if h1::wants_upgrade(&req) {
            trace!("server request wants HTTP/1.1 upgrade");
            // Upgrade requests include several "connection" headers that
            // cannot be removed.

            // Setup HTTP Upgrade machinery.
            let halves = Http11Upgrade::new(
                self.upgrade_drain_signal.clone(),
                ErasedExecutor::erase(self.upgrade_executor.clone()),
            );
            req.extensions_mut().insert(halves.client);

            Some(halves.server)
        } else {
            h1::strip_connection_headers(req.headers_mut());
            None
        };


        let req = req.map(move |b| HttpBody::Http1 {
            body: Some(b),
            upgrade,
        });
        let f = HyperServerSvcFuture {
            inner: self.service.call(req),
        };
        Either::A(f)
    }
}

impl<F, B> Future for HyperServerSvcFuture<F>
where
    F: Future<Item=http::Response<B>>,
    F::Error: fmt::Debug,
{
    type Item = http::Response<BodyPayload<B>>;
    type Error = h2::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let mut res = try_ready!(self.inner.poll().map_err(|e| {
            debug!("h2 error: {:?}", e);
            h2::Error::from(io::Error::from(io::ErrorKind::Other))
        }));

        if h1::is_upgrade(&res) {
            trace!("client response is HTTP/1.1 upgrade");
        } else {
            h1::strip_connection_headers(res.headers_mut());
        }
        Ok(Async::Ready(res.map(BodyPayload::new)))
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
