use bytes::{Buf, IntoBuf};
use futures::{future, Async, Future, Poll, Stream};
use h2;
use http;
use std::default::Default;
use std::marker::PhantomData;
use std::sync::Arc;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::time::Instant;
use tower_service::{NewService, Service};
use tower_h2::{client, Body};

use ctx;
use telemetry::event::{self, Event};

const GRPC_STATUS: &str = "grpc-status";

/// A `RequestOpen` timestamp.
///
/// This is added to a request's `Extensions` by the `TimestampRequestOpen`
/// middleware. It's a newtype in order to distinguish it from other
/// `Instant`s that may be added as request extensions.
#[derive(Copy, Clone, Debug)]
pub struct RequestOpen(pub Instant);

/// Middleware that adds a `RequestOpen` timestamp to requests.
///
/// This is a separate middleware from `sensor::Http`, because we want
/// to install it at the earliest point in the stack. This is in order
/// to ensure that request latency metrics cover the overhead added by
/// the proxy as accurately as possible.
#[derive(Copy, Clone, Debug)]
pub struct TimestampRequestOpen<S> {
    inner: S,
}

pub struct NewHttp<N, A, B> {
    next_id: Arc<AtomicUsize>,
    new_service: N,
    handle: super::Handle,
    client_ctx: Arc<ctx::transport::Client>,
    _p: PhantomData<(A, B)>,
}

pub struct Init<F, A, B> {
    next_id: Arc<AtomicUsize>,
    future: F,
    handle: super::Handle,
    client_ctx: Arc<ctx::transport::Client>,
    _p: PhantomData<(A, B)>,
}

/// Wraps a transport with telemetry.
#[derive(Debug)]
pub struct Http<S, A, B> {
    next_id: Arc<AtomicUsize>,
    service: S,
    handle: super::Handle,
    client_ctx: Arc<ctx::transport::Client>,
    _p: PhantomData<(A, B)>,
}

#[derive(Debug)]
pub struct Respond<F, B> {
    future: F,
    inner: Option<RespondInner>,
    _p: PhantomData<(B)>,
}

#[derive(Debug)]
struct RespondInner {
    handle: super::Handle,
    ctx: Arc<ctx::http::Request>,
    request_open_at: Instant,
}

pub type ResponseBody<B> = MeasuredBody<B, ResponseBodyInner>;
pub type RequestBody<B> = MeasuredBody<B, RequestBodyInner>;

#[derive(Debug)]
pub struct MeasuredBody<B, I: BodySensor> {
    body: B,
    inner: Option<I>,
    _p: PhantomData<(B)>,
}

/// The `inner` portion of a `MeasuredBody`, with differing implementations
/// for request and response streams.
pub trait BodySensor: Sized {
    fn frame(&mut self, bytes: usize);
    fn fail(self, reason: h2::Reason);
    fn end(self, grpc_status: Option<u32>);
}

#[derive(Debug)]
pub struct ResponseBodyInner {
    handle: super::Handle,
    ctx: Arc<ctx::http::Response>,
    bytes_sent: u64,
    frames_sent: u32,
    request_open_at: Instant,
    response_open_at: Instant,
    response_first_frame_at: Option<Instant>,
}


#[derive(Debug)]
pub struct RequestBodyInner {
    handle: super::Handle,
    ctx: Arc<ctx::http::Request>,
    bytes_sent: u64,
    frames_sent: u32,
    request_open_at: Instant,
}

// === NewHttp ===

impl<N, A, B> NewHttp<N, A, B>
where
    A: Body + 'static,
    B: Body + 'static,
    N: NewService<
        Request = http::Request<RequestBody<A>>,
        Response = http::Response<B>,
        Error = client::Error,
    >
        + 'static,
{
    pub(super) fn new(
        next_id: Arc<AtomicUsize>,
        new_service: N,
        handle: &super::Handle,
        client_ctx: &Arc<ctx::transport::Client>,
    ) -> Self {
        Self {
            next_id,
            new_service,
            handle: handle.clone(),
            client_ctx: Arc::clone(client_ctx),
            _p: PhantomData,
        }
    }
}

impl<N, A, B> NewService for NewHttp<N, A, B>
where
    A: Body + 'static,
    B: Body + 'static,
    N: NewService<
        Request = http::Request<RequestBody<A>>,
        Response = http::Response<B>,
        Error = client::Error,
    >
        + 'static,
{
    type Request = http::Request<A>;
    type Response = http::Response<ResponseBody<B>>;
    type Error = N::Error;
    type InitError = N::InitError;
    type Future = Init<N::Future, A, B>;
    type Service = Http<N::Service, A, B>;

    fn new_service(&self) -> Self::Future {
        Init {
            next_id: self.next_id.clone(),
            future: self.new_service.new_service(),
            handle: self.handle.clone(),
            client_ctx: Arc::clone(&self.client_ctx),
            _p: PhantomData,
        }
    }
}

// === Init ===

impl<F, A, B> Future for Init<F, A, B>
where
    A: Body + 'static,
    B: Body + 'static,
    F: Future,
    F::Item: Service<
        Request = http::Request<RequestBody<A>>,
        Response = http::Response<B>
    >,
{
    type Item = Http<F::Item, A, B>;
    type Error = F::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let service = try_ready!(self.future.poll());

        Ok(Async::Ready(Http {
            service,
            handle: self.handle.clone(),
            next_id: self.next_id.clone(),
            client_ctx: self.client_ctx.clone(),
            _p: PhantomData,
        }))
    }
}

// === Http ===

impl<S, A, B> Service for Http<S, A, B>
where
    A: Body + 'static,
    B: Body + 'static,
    S: Service<
        Request = http::Request<RequestBody<A>>,
        Response = http::Response<B>,
        Error = client::Error,
    >
        + 'static,
{
    type Request = http::Request<A>;
    type Response = http::Response<ResponseBody<B>>;
    type Error = S::Error;
    type Future = Respond<S::Future, B>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        self.service.poll_ready()
    }

    fn call(&mut self, mut req: Self::Request) -> Self::Future {
        let metadata = (
            req.extensions_mut().remove::<Arc<ctx::transport::Server>>(),
            req.extensions_mut().remove::<RequestOpen>()
        );
        let (inner, body_inner) = match metadata {
            (Some(ctx), Some(RequestOpen(request_open_at))) => {
                let id = self.next_id.fetch_add(1, Ordering::SeqCst);
                let ctx = ctx::http::Request::new(&req, &ctx, &self.client_ctx, id);

                self.handle
                    .send(|| Event::StreamRequestOpen(Arc::clone(&ctx)));

                let respond_inner = Some(RespondInner {
                    ctx: ctx.clone(),
                    handle: self.handle.clone(),
                    request_open_at,
                });
                let body_inner =
                    if req.body().is_end_stream() {
                        self.handle.send(|| {
                            Event::StreamRequestEnd(
                                Arc::clone(&ctx),
                                event::StreamRequestEnd {
                                    request_open_at,
                                    request_end_at: request_open_at,
                                },
                            )
                        });
                        None
                    } else {
                        Some(RequestBodyInner {
                            ctx,
                            handle: self.handle.clone(),
                            request_open_at,
                            frames_sent: 0,
                            bytes_sent: 0,
                        })
                    };
                (respond_inner, body_inner)
            },
            (ctx, request_open_at) => {
                warn!(
                    "missing metadata for a request to {:?}; ctx={:?}; request_open_at={:?};",
                    req.uri(), ctx, request_open_at
                );
                (None, None)
            },
        };
        let req = {
            let (parts, body) = req.into_parts();
            let body = MeasuredBody::new(body, body_inner);
            http::Request::from_parts(parts, body)
        };

        let future = self.service.call(req);

        Respond {
            future,
            inner,
            _p: PhantomData,
        }
    }
}

// === Measured ===

impl<F, B> Future for Respond<F, B>
where
    F: Future<Item = http::Response<B>, Error=client::Error>,
    B: Body + 'static,
{
    type Item = http::Response<ResponseBody<B>>;
    type Error = F::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        match self.future.poll() {
            Ok(Async::NotReady) => Ok(Async::NotReady),

            Ok(Async::Ready(rsp)) => {
                let inner = self.inner.take().and_then(|i| {
                    let RespondInner {
                        ctx,
                        mut handle,
                        request_open_at,
                    } = i;

                    let ctx = ctx::http::Response::new(&rsp, &ctx);

                    let response_open_at = Instant::now();
                    handle.send(|| {
                        Event::StreamResponseOpen(
                            Arc::clone(&ctx),
                            event::StreamResponseOpen {
                                request_open_at,
                                response_open_at,
                            },
                        )
                    });

                    if rsp.body().is_end_stream() {
                        handle.send(|| {
                            let grpc_status = rsp.headers()
                                .get(GRPC_STATUS)
                                .and_then(|v| v.to_str().ok())
                                .and_then(|s| s.parse::<u32>().ok());

                            event::Event::StreamResponseEnd(
                                Arc::clone(&ctx),
                                event::StreamResponseEnd {
                                    grpc_status,
                                    request_open_at,
                                    response_open_at,
                                    response_first_frame_at: response_open_at,
                                    response_end_at: response_open_at,
                                    bytes_sent: 0,
                                    frames_sent: 0,
                                },
                            )
                        });

                        None
                    } else {
                        Some(ResponseBodyInner {
                            handle: handle,
                            ctx,
                            bytes_sent: 0,
                            frames_sent: 0,
                            request_open_at,
                            response_open_at,
                            response_first_frame_at: None,
                        })
                    }
                });

                let rsp = {
                    let (parts, body) = rsp.into_parts();
                    let body = ResponseBody::new(body, inner);
                    http::Response::from_parts(parts, body)
                };

                Ok(Async::Ready(rsp))
            }

            Err(e) => {
                if let Some(error) = e.reason() {
                    if let Some(i) = self.inner.take() {
                        let RespondInner {
                            ctx,
                            mut handle,
                            request_open_at,
                        } = i;

                        handle.send(|| {
                            Event::StreamRequestFail(
                                Arc::clone(&ctx),
                                event::StreamRequestFail {
                                    error,
                                    request_open_at,
                                    request_fail_at: Instant::now(),
                                },
                            )
                        });
                    }
                }

                Err(e)
            }
        }
    }
}

// === MeasuredBody ===

impl<B, I: BodySensor> MeasuredBody<B, I> {
    pub fn new(body: B, inner: Option<I>) -> Self {
        Self {
            body,
            inner,
            _p: PhantomData,
        }
    }

    /// Wraps an operation on the underlying transport with error telemetry.
    ///
    /// If the transport operation results in a non-recoverable error, a transport close
    /// event is emitted.
    fn sense_err<F, T>(&mut self, op: F) -> Result<T, h2::Error>
    where
        F: FnOnce(&mut B) -> Result<T, h2::Error>,
    {
        match op(&mut self.body) {
            Ok(v) => Ok(v),
            Err(e) => {
                if let Some(error) = e.reason() {
                    if let Some(i) = self.inner.take() {
                        i.fail(error);
                    }
                }

                Err(e)
            }
        }
    }
}

impl<B, I> Body for MeasuredBody<B, I>
where
    B: Body + 'static,
    I: BodySensor,
{
    /// The body chunk type
    type Data = <B::Data as IntoBuf>::Buf;

    fn is_end_stream(&self) -> bool {
        self.body.is_end_stream()
    }

    fn poll_data(&mut self) -> Poll<Option<Self::Data>, h2::Error> {
        let frame = try_ready!(self.sense_err(|b| b.poll_data()))
            .map(|f| f.into_buf());

        if let Some(ref f) = frame {
            if let Some(ref mut inner) = self.inner {
                inner.frame(f.remaining());
            }
        }

        // If the frame ended the stream, send the end of stream event now,
        // as we may not be polled again.
        if self.is_end_stream() {
            if let Some(inner) = self.inner.take() {
                inner.end(None);
            }
        }

        Ok(Async::Ready(frame))
    }

    fn poll_trailers(&mut self) -> Poll<Option<http::HeaderMap>, h2::Error> {
        match self.sense_err(|b| b.poll_trailers()) {
            Err(e) => Err(e),
            Ok(Async::NotReady) => Ok(Async::NotReady),
            Ok(Async::Ready(trls)) => {
                if let Some(i) = self.inner.take() {
                    let grpc_status = trls.as_ref()
                        .and_then(|t| t.get(GRPC_STATUS))
                        .and_then(|v| v.to_str().ok())
                        .and_then(|s| s.parse::<u32>().ok());
                    i.end(grpc_status);
                }

                Ok(Async::Ready(trls))
            }
        }
    }
}

impl<B, I> Default for MeasuredBody<B, I>
where
    B: Default,
    I: BodySensor,
{
    fn default() -> Self {
        Self {
            body: B::default(),
            inner: None,
            _p: PhantomData,
        }
    }
}

impl<B, I> Stream for MeasuredBody<B, I>
where
    B: Stream,
    I: BodySensor,
{
    type Item = B::Item;
    type Error = B::Error;

    fn poll(&mut self) -> Poll<Option<Self::Item>, Self::Error> {
        self.body.poll()
    }

}

impl<B, I: BodySensor> Drop for MeasuredBody<B, I> {
    fn drop(&mut self) {
        if let Some(inner) = self.inner.take() {
            inner.end(None);
        }
    }
}

// ===== impl BodySensor =====

impl BodySensor for ResponseBodyInner {

    fn frame(&mut self, bytes: usize) {
        self.frames_sent += 1;
        self.bytes_sent += bytes as u64;
        if self.response_first_frame_at.is_none() {
            self.response_first_frame_at = Some(Instant::now());
        }
    }

    fn fail(self, error: h2::Reason) {
        let ResponseBodyInner {
            ctx,
            mut handle,
            request_open_at,
            response_open_at,
            response_first_frame_at,
            bytes_sent,
            frames_sent,
            ..
        } = self;

        handle.send(|| {
            event::Event::StreamResponseFail(
                Arc::clone(&ctx),
                event::StreamResponseFail {
                    error,
                    request_open_at,
                    response_open_at,
                    response_first_frame_at,
                    response_fail_at: Instant::now(),
                    bytes_sent,
                    frames_sent,
                },
            )
        });
    }

    fn end(self, grpc_status: Option<u32>) {
        let ResponseBodyInner {
            ctx,
            mut handle,
            request_open_at,
            response_open_at,
            response_first_frame_at,
            bytes_sent,
            frames_sent,
        } = self;
        let response_end_at =  Instant::now();

        handle.send(||
            event::Event::StreamResponseEnd(
                Arc::clone(&ctx),
                event::StreamResponseEnd {
                    grpc_status,
                    request_open_at,
                    response_open_at,
                    response_first_frame_at: response_first_frame_at.unwrap_or(response_end_at),
                    response_end_at,
                    bytes_sent,
                    frames_sent,
                },
            )
        )
    }
}

impl BodySensor for RequestBodyInner {

    fn frame(&mut self, bytes: usize) {
        self.frames_sent += 1;
        self.bytes_sent += bytes as u64;
    }

    fn fail(self, error: h2::Reason) {
        let RequestBodyInner {
            ctx,
            mut handle,
            request_open_at,
            ..
        } = self;

        handle.send(||
            event::Event::StreamRequestFail(
                Arc::clone(&ctx),
                event::StreamRequestFail {
                    error,
                    request_open_at,
                    request_fail_at: Instant::now(),
                },
            )
        )
    }

    fn end(self, _grpc_status: Option<u32>) {
        let RequestBodyInner {
            ctx,
            mut handle,
            request_open_at,
            ..
        } = self;

        handle.send(||
            event::Event::StreamRequestEnd(
                Arc::clone(&ctx),
                event::StreamRequestEnd {
                    request_open_at,
                    request_end_at: Instant::now(),
                },
            )
        )
    }
}

impl<S> TimestampRequestOpen<S> {
    pub fn new(inner: S) -> Self {
        Self { inner }
    }
}

impl<S, B> Service for TimestampRequestOpen<S>
where
    S: Service<Request = http::Request<B>>,
{
    type Request = http::Request<B>;
    type Response = S::Response;
    type Error = S::Error;
    type Future = S::Future;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        self.inner.poll_ready()
    }

    fn call(&mut self, mut req: Self::Request) -> Self::Future {
        req.extensions_mut().insert(RequestOpen(Instant::now()));
        self.inner.call(req)
    }
}

impl<S, B> NewService for TimestampRequestOpen<S>
where
    S: NewService<Request = http::Request<B>>,
{
    type Request = S::Request;
    type Response = S::Response;
    type Error = S::Error;
    type InitError = S::InitError;
    type Future = future::Map<
        S::Future,
        fn(S::Service) -> Self::Service
    >;
    type Service = TimestampRequestOpen<S::Service>;

    fn new_service(&self) -> Self::Future {
        self.inner.new_service().map(TimestampRequestOpen::new)
    }
}
