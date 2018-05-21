use bytes::{Buf, IntoBuf};
use futures::{Async, Future, Poll, Stream};
use h2;
use http;
use std::default::Default;
use std::marker::PhantomData;
use std::sync::Arc;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::time::{Duration, Instant};
use tower_service::{NewService, Service};
use tower_h2::{client, Body};

use ctx;
use now::{Now, SystemNow};
use telemetry::event::{self, Event};

const GRPC_STATUS: &str = "grpc-status";

/// A `RequestOpen` timestamp.
///
/// This is added to a request's `Extensions` by the `TimestampRequestOpen`
/// middleware. It's a newtype in order to distinguish it from other
/// `Instant`s that may be added as request extensions.
#[derive(Copy, Clone, Debug, PartialEq)]
pub struct RequestOpen(pub Instant);

/// Middleware that adds a `RequestOpen` timestamp to requests.
///
/// This is a separate middleware from `sensor::Http`, because we want
/// to install it at the earliest point in the stack. This is in order
/// to ensure that request latency metrics cover the overhead added by
/// the proxy as accurately as possible.
#[derive(Copy, Clone, Debug)]
pub struct TimestampRequestOpen<S, T: Now = SystemNow> {
    inner: S,
    now: T,
}

pub struct NewHttp<N, A, B, T: Now = SystemNow> {
    next_id: Arc<AtomicUsize>,
    new_service: N,
    handle: super::Handle,
    client_ctx: Arc<ctx::transport::Client>,
    now: T,
    _p: PhantomData<(A, B)>,
}

pub struct Init<F, A, B, T: Now = SystemNow> {
    next_id: Arc<AtomicUsize>,
    future: F,
    handle: super::Handle,
    client_ctx: Arc<ctx::transport::Client>,
    now: T,
    _p: PhantomData<(A, B)>,
}

/// Wraps a transport with telemetry.
#[derive(Debug)]
pub struct Http<S, A, B, T: Now = SystemNow> {
    next_id: Arc<AtomicUsize>,
    service: S,
    handle: super::Handle,
    client_ctx: Arc<ctx::transport::Client>,
    now: T,
    _p: PhantomData<(A, B)>,
}

#[derive(Debug)]
pub struct Respond<F, B, T: Now = SystemNow> {
    future: F,
    inner: Option<RespondInner>,
    now: T,
    _p: PhantomData<(B)>,
}

#[derive(Debug)]
struct RespondInner {
    handle: super::Handle,
    ctx: Arc<ctx::http::Request>,
    request_open: Instant,
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
    fn fail(self, reason: h2::Reason);
    fn end(self, grpc_status: Option<u32>);
    fn frames_sent(&mut self) -> &mut u32;
    fn bytes_sent(&mut self) -> &mut u64;
}

#[derive(Debug)]
pub struct ResponseBodyInner {
    handle: super::Handle,
    ctx: Arc<ctx::http::Response>,
    bytes_sent: u64,
    frames_sent: u32,
    request_open: Instant,
    response_open: Instant,
}


#[derive(Debug)]
pub struct RequestBodyInner {
    handle: super::Handle,
    ctx: Arc<ctx::http::Request>,
    bytes_sent: u64,
    frames_sent: u32,
    request_open: Instant,
}

// === NewHttp ===

impl<N, A, B> NewHttp<N, A, B, SystemNow>
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
            now: SystemNow,
            _p: PhantomData,
        }
    }
}

impl<N, A, B, T> NewService for NewHttp<N, A, B, T>
where
    A: Body + 'static,
    B: Body + 'static,
    T: Now,
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
    type Future = Init<N::Future, A, B, T>;
    type Service = Http<N::Service, A, B, T>;

    fn new_service(&self) -> Self::Future {
        Init {
            next_id: self.next_id.clone(),
            future: self.new_service.new_service(),
            handle: self.handle.clone(),
            client_ctx: Arc::clone(&self.client_ctx),
            now: self.now.clone(),
            _p: PhantomData,
        }
    }
}

// === Init ===

impl<F, A, B, T> Future for Init<F, A, B, T>
where
    A: Body + 'static,
    B: Body + 'static,
    F: Future,
    T: Now,
    F::Item: Service<
        Request = http::Request<RequestBody<A>>,
        Response = http::Response<B>
    >,
{
    type Item = Http<F::Item, A, B, T>;
    type Error = F::Error;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        let service = try_ready!(self.future.poll());

        Ok(Async::Ready(Http {
            service,
            handle: self.handle.clone(),
            next_id: self.next_id.clone(),
            client_ctx: self.client_ctx.clone(),
            now: self.now.clone(),
            _p: PhantomData,
        }))
    }
}

// === Http ===

impl<S, A, B, T> Service for Http<S, A, B, T>
where
    A: Body + 'static,
    B: Body + 'static,
    T: Now,
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
    type Future = Respond<S::Future, B, T>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        self.service.poll_ready()
    }

    fn call(&mut self, mut req: Self::Request) -> Self::Future {
        let metadata = (
            req.extensions_mut().remove::<Arc<ctx::transport::Server>>(),
            req.extensions_mut().remove::<RequestOpen>()
        );
        let (inner, body_inner) = match metadata {
            (Some(ctx), Some(RequestOpen(request_open))) => {
                let id = self.next_id.fetch_add(1, Ordering::SeqCst);
                let ctx = ctx::http::Request::new(&req, &ctx, &self.client_ctx, id);

                self.handle
                    .send(|| Event::StreamRequestOpen(Arc::clone(&ctx)));

                let respond_inner = Some(RespondInner {
                    ctx: ctx.clone(),
                    handle: self.handle.clone(),
                    request_open,
                });
                let body_inner =
                    if req.body().is_end_stream() {
                        self.handle.send(|| {
                            Event::StreamRequestEnd(
                                Arc::clone(&ctx),
                                event::StreamRequestEnd {
                                    since_request_open: request_open.elapsed(),
                                },
                            )
                        });
                        None
                    } else {
                        Some(RequestBodyInner {
                            ctx,
                            handle: self.handle.clone(),
                            request_open,
                            frames_sent: 0,
                            bytes_sent: 0,
                        })
                    };
                (respond_inner, body_inner)
            },
            (ctx, request_open) => {
                warn!(
                    "missing metadata for a request to {:?}; ctx={:?}; request_open={:?};",
                    req.uri(), ctx, request_open
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
            now: self.now.clone(),
            _p: PhantomData,
        }
    }
}

// === Measured ===

impl<F, B, T> Future for Respond<F, B, T>
where
    F: Future<Item = http::Response<B>, Error=client::Error>,
    B: Body + 'static,
    T: Now,
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
                        request_open,
                    } = i;

                    let ctx = ctx::http::Response::new(&rsp, &ctx);

                    handle.send(|| {
                        Event::StreamResponseOpen(
                            Arc::clone(&ctx),
                            event::StreamResponseOpen {
                                since_request_open: request_open.elapsed(),
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
                                    since_request_open: request_open.elapsed(),
                                    since_response_open: Duration::default(),
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
                            request_open,
                            response_open: self.now.now(),
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
                            request_open,
                        } = i;

                        handle.send(|| {
                            Event::StreamRequestFail(
                                Arc::clone(&ctx),
                                event::StreamRequestFail {
                                    error,
                                    since_request_open: request_open.elapsed(),
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
        let frame = try_ready!(self.sense_err(|b| b.poll_data()));
        let frame = frame.map(|frame| {
            let frame = frame.into_buf();
            if let Some(ref mut inner) = self.inner {
                *inner.frames_sent() += 1;
                *inner.bytes_sent() += frame.remaining() as u64;
            }
            frame
        });

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

// ===== impl BodySensor =====

impl BodySensor for ResponseBodyInner {

    fn fail(self, error: h2::Reason) {
        let ResponseBodyInner {
            ctx,
            mut handle,
            request_open,
            response_open,
            bytes_sent,
            frames_sent,
            ..
        } = self;

        handle.send(|| {
            event::Event::StreamResponseFail(
                Arc::clone(&ctx),
                event::StreamResponseFail {
                    error,
                    since_request_open: request_open.elapsed(),
                    since_response_open: response_open.elapsed(),
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
            request_open,
            response_open,
            bytes_sent,
            frames_sent,
        } = self;

        handle.send(||
            event::Event::StreamResponseEnd(
                Arc::clone(&ctx),
                event::StreamResponseEnd {
                    grpc_status,
                    since_request_open: request_open.elapsed(),
                    since_response_open: response_open.elapsed(),
                    bytes_sent,
                    frames_sent,
                },
            )
        )
    }

    fn frames_sent(&mut self) -> &mut u32 {
        &mut self.frames_sent
    }

    fn bytes_sent(&mut self) -> &mut u64 {
        &mut self.bytes_sent
    }
}

impl BodySensor for RequestBodyInner {

    fn fail(self, error: h2::Reason) {
        let RequestBodyInner {
            ctx,
            mut handle,
            request_open,
            ..
        } = self;

        handle.send(||
            event::Event::StreamRequestFail(
                Arc::clone(&ctx),
                event::StreamRequestFail {
                    error,
                    since_request_open: request_open.elapsed(),
                },
            )
        )
    }

    fn end(self, _grpc_status: Option<u32>) {
        let RequestBodyInner {
            ctx,
            mut handle,
            request_open,
            ..
        } = self;

        handle.send(||
            event::Event::StreamRequestEnd(
                Arc::clone(&ctx),
                event::StreamRequestEnd {
                    since_request_open: request_open.elapsed(),
                },
            )
        )
    }

    fn frames_sent(&mut self) -> &mut u32 {
        &mut self.frames_sent
    }

    fn bytes_sent(&mut self) -> &mut u64 {
        &mut self.bytes_sent
    }
}

// ===== impl TimestampRequestOpen =====

impl<S> TimestampRequestOpen<S, SystemNow> {
    pub fn new(inner: S) -> Self {
        Self { inner, now: SystemNow }
    }
}

impl<S> TimestampRequestOpen<S, SystemNow> {
    fn with_time<T: Now>(self, now: T) -> TimestampRequestOpen<S, T> {
        TimestampRequestOpen { now, inner: self.inner, }
    }
}

impl<S, B, T> Service for TimestampRequestOpen<S, T>
where
    S: Service<Request = http::Request<B>>,
    T: Now,
{
    type Request = http::Request<B>;
    type Response = S::Response;
    type Error = S::Error;
    type Future = S::Future;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        self.inner.poll_ready()
    }

    fn call(&mut self, mut req: Self::Request) -> Self::Future {
        let request_open = RequestOpen(self.now.now());
        req.extensions_mut().insert(request_open);
        self.inner.call(req)
    }
}

impl<S, B, T> NewService for TimestampRequestOpen<S, T>
where
    S: NewService<Request = http::Request<B>> + 'static,
    T: Now + 'static,
{
    type Request = S::Request;
    type Response = S::Response;
    type Error = S::Error;
    type InitError = S::InitError;
    type Future = Box<Future<Item = Self::Service, Error = S::InitError>>;
    type Service = TimestampRequestOpen<S::Service, T>;

    fn new_service(&self) -> Self::Future {
        let now = self.now.clone();
        let f = self.inner.new_service().map(|s| {
            TimestampRequestOpen::new(s).with_time(now)
        });
        Box::new(f)
    }
}

#[cfg(test)]
mod tests {
    use futures::{future, Poll};
    use futures_mpsc_lossy;
    use tower_service::Service;

    use now::test_util::Clock;
    use ctx::test_util::process as process_ctx;

    use super::*;

    struct SimulateLatency {
        clock: Clock,
        latency: Duration,
    }

    impl Service for SimulateLatency {
        type Request = http::Request<()>;
        type Response = http::Response<()>;
        type Error = ();
        type Future = future::FutureResult<Self::Response, ()>;

        fn poll_ready(&mut self) -> Poll<(), Self::Error> {
            Ok(().into())
        }

        fn call(&mut self, _: Self::Request) -> Self::Future {
            self.clock.advance(self.latency);
            future::ok(http::Response::new(()))
        }
    }

    impl<A, B> Http<SimulateLatency, A, B, Clock> {
        fn call_ok(&mut self, req: http::Request<A>) -> http::Response<B> {
            self.call(req).wait()
        }
    }

    macro_rules! event {
        ($evs:ident) => {
            match $evs.poll().expect("telemetry event") {
                Async::Ready(Some(ev)) => ev,
                _ => panic!("no telemetry events"),
            }
        };
    }

    quickcheck! {

        fn http_records_latency(latency: Duration) -> bool {
            let process = process_ctx();
            let proxy = ctx::Proxy::outbound(&process);

            let clock = Clock::default();

            let (tx, events) = futures_mpsc_lossy::channel(4);

            let mut http = {
                Http {
                    next_id: Default::default(),
                    service: SimulateLatency {
                        clock: clock.clone(),
                        latency,
                    },
                    handle: super::super::Handle(tx),
                    client_ctx: ctx::test_util::client(&proxy, None),
                    now: clock.clone(),
                    _p: PhantomData,
                }
            };

            let _ = http.call_ok(http::Request::new(()));
            match event!(events) {
                Event::StreamRequestOpen(_) => {}
                ev => panic!("unexpected event: {:?}", ev),
            }
            match event!(events) {
                Event::StreamRequestEnd(_, _) => {}
                ev => panic!("unexpected event: {:?}", ev),
            }
            match event!(events) {
                Event::StreamResponseOpen(_, _) => {}
                ev => panic!("unexpected event: {:?}", ev),
            }

            match event!(events) {
                Event::StreamResponseEnd(_, _) => {}
                ev => panic!("unexpected event: {:?}", ev),
            }


            true
        }

    }
}
