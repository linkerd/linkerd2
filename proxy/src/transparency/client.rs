use futures::{Async, Future, Poll};
use h2;
use http;
use hyper;
use tokio_connect::Connect;
use tokio::runtime::TaskExecutor;
use tower_service::{Service, NewService};
use tower_h2::{self, Body};

use bind;
use telemetry::sensor::http::RequestBody;
use super::glue::{BodyStream, HttpBody, HyperConnect};
use super::h1::UriIsAbsoluteForm;

type HyperClient<C, B> =
    hyper::Client<HyperConnect<C>, BodyStream<RequestBody<B>>>;

/// A `NewService` that can speak either HTTP/1 or HTTP/2.
pub struct Client<C, B>
where
    B: tower_h2::Body + 'static,
{
    inner: ClientInner<C, B>,
}

enum ClientInner<C, B>
where
    B: tower_h2::Body + 'static,
{
    Http1(HyperClient<C, B>),
    Http2(tower_h2::client::Connect<C, TaskExecutor, RequestBody<B>>),
}

/// A `Future` returned from `Client::new_service()`.
pub struct ClientNewServiceFuture<C, B>
where
    B: tower_h2::Body + 'static,
    C: Connect + 'static,
{
    inner: ClientNewServiceFutureInner<C, B>,
}

enum ClientNewServiceFutureInner<C, B>
where
    B: tower_h2::Body + 'static,
    C: Connect + 'static,
{
    Http1(Option<HyperClient<C, B>>),
    Http2(tower_h2::client::ConnectFuture<C, TaskExecutor, RequestBody<B>>),
}

/// The `Service` yielded by `Client::new_service()`.
pub struct ClientService<C, B>
where
    B: tower_h2::Body + 'static,
    C: Connect,
{
    inner: ClientServiceInner<C, B>,
}

enum ClientServiceInner<C, B>
where
    B: tower_h2::Body + 'static,
    C: Connect
{
    Http1(HyperClient<C, B>),
    Http2(tower_h2::client::Connection<
        <C as Connect>::Connected,
        TaskExecutor,
        RequestBody<B>,
    >),
}

impl<C, B> Client<C, B>
where
    C: Connect + Clone + 'static,
    C::Future: 'static,
    B: tower_h2::Body + 'static,
{
    /// Create a new `Client`, bound to a specific protocol (HTTP/1 or HTTP/2).
    pub fn new(protocol: &bind::Protocol,
               connect: C,
               executor: TaskExecutor)
               -> Self
    {
        match *protocol {
            bind::Protocol::Http1(_) => {
                let h1 = hyper::Client::configure()
                    .connector(HyperConnect::new(connect))
                    .body()
                    // hyper should never try to automatically set the Host
                    // header, instead always just passing whatever we received.
                    .set_host(false)
                    .build(&executor);
                Client {
                    inner: ClientInner::Http1(h1),
                }
            },
            bind::Protocol::Http2 => {
                let mut h2_builder = h2::client::Builder::default();
                // h2 currently doesn't handle PUSH_PROMISE that well, so we just
                // disable it for now.
                h2_builder.enable_push(false);
                let h2 = tower_h2::client::Connect::new(connect, h2_builder, executor);

                Client {
                    inner: ClientInner::Http2(h2),
                }
            }
        }
    }
}

impl<C, B> NewService for Client<C, B>
where
    C: Connect + Clone + 'static,
    C::Future: 'static,
    B: tower_h2::Body + 'static,
{
    type Request = bind::HttpRequest<B>;
    type Response = http::Response<HttpBody>;
    type Error = tower_h2::client::Error;
    type InitError = tower_h2::client::ConnectError<C::Error>;
    type Service = ClientService<C, B>;
    type Future = ClientNewServiceFuture<C, B>;

    fn new_service(&self) -> Self::Future {
        let inner = match self.inner {
            ClientInner::Http1(ref h1) => {
                ClientNewServiceFutureInner::Http1(Some(h1.clone()))
            },
            ClientInner::Http2(ref h2) => {
                ClientNewServiceFutureInner::Http2(h2.new_service())                        },
        };
        ClientNewServiceFuture {
            inner,
        }
    }
}

impl<C, B> Future for ClientNewServiceFuture<C, B>
where
    C: Connect + 'static,
    B: tower_h2::Body + 'static,
{
    type Item = ClientService<C, B>;
    type Error = tower_h2::client::ConnectError<C::Error>;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let inner = match self.inner {
            ClientNewServiceFutureInner::Http1(ref mut h1) => {
                ClientServiceInner::Http1(h1.take().expect("poll more than once"))
            },
            ClientNewServiceFutureInner::Http2(ref mut h2) => {
                let s = try_ready!(h2.poll());
                ClientServiceInner::Http2(s)
            },
        };
        Ok(Async::Ready(ClientService {
            inner,
        }))
    }
}

impl<C, B> Service for ClientService<C, B>
where
    C: Connect + 'static,
    C::Future: 'static,
    B: tower_h2::Body + 'static,
{
    type Request = bind::HttpRequest<B>;
    type Response = http::Response<HttpBody>;
    type Error = tower_h2::client::Error;
    type Future = ClientServiceFuture;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        match self.inner {
            ClientServiceInner::Http1(_) => Ok(Async::Ready(())),
            ClientServiceInner::Http2(ref mut h2) => h2.poll_ready(),
        }
    }

    fn call(&mut self, req: Self::Request) -> Self::Future {
        use http::header::CONTENT_LENGTH;

        match self.inner {
            ClientServiceInner::Http1(ref h1) => {
                // This sentinel may be set in h1::normalize_our_view_of_uri
                // when the original request-target was in absolute-form. In
                // that case, for hyper 0.11.x, we need to call `req.set_proxy`.
                let was_absolute_form = req.extensions()
                    .get::<UriIsAbsoluteForm>()
                    .is_some();
                // As of hyper 0.11.x, a set body implies a body must be sent.
                // If there is no content-length header, hyper adds a
                // transfer-encoding: chunked header, since it has no other way
                // of knowing if the body is empty or not.
                //
                // However, if we received a `Content-Length: 0` body ourselves,
                // the `body.is_end_stream()` would be true. While its true that
                // semantically a client request with no body headers and a request
                // with `Content-Length: 0` have the exact same body length, we
                // want to preserve the exact intent of the original request, as
                // we are trying to be a transparent proxy.
                //
                // So, if `Content-Length` is set at all, we need to send it. If
                // we unset the body, hyper assumes the RFC7230 semantics that
                // it can strip content body headers. Therefore, even if the
                // body is empty, it should still be passed to hyper so that
                // the `Content-Length: 0` header is not stripped.
                //
                // This does *NOT* check for `Transfer-Encoding` as future-proofing
                // measure. When we add the ability to proxy HTTP2 to HTTP1, this
                // body could have come from h2, where there is no `Transfer-Encoding`.
                // Instead, it has been replaced by `DATA` frames. The
                // `HttpBody::is_end_stream()` manages that distinction for us.
                let should_take_body = req.body().is_end_stream()
                    && !req.headers().contains_key(CONTENT_LENGTH);
                let mut req = hyper::Request::from(req.map(BodyStream::new));
                if should_take_body {
                    req.body_mut().take();
                }
                req.set_proxy(was_absolute_form);
                ClientServiceFuture::Http1(h1.request(req))
            },
            ClientServiceInner::Http2(ref mut h2) => {
                ClientServiceFuture::Http2(h2.call(req))
            },
        }
    }
}

pub enum ClientServiceFuture {
    Http1(hyper::client::FutureResponse),
    Http2(tower_h2::client::ResponseFuture),
}

impl Future for ClientServiceFuture {
    type Item = http::Response<HttpBody>;
    type Error = tower_h2::client::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        match *self {
            ClientServiceFuture::Http1(ref mut f) => {
                match f.poll() {
                    Ok(Async::Ready(res)) => {
                        let res = http::Response::from(res);
                        let res = res.map(HttpBody::Http1);
                        Ok(Async::Ready(res))
                    },
                    Ok(Async::NotReady) => Ok(Async::NotReady),
                    Err(e) => {
                        debug!("http/1 client error: {}", e);
                        Err(h2::Reason::INTERNAL_ERROR.into())
                    }
                }
            },
            ClientServiceFuture::Http2(ref mut f) => {
                let res = try_ready!(f.poll());
                let res = res.map(HttpBody::Http2);
                Ok(Async::Ready(res))
            }
        }
    }
}

