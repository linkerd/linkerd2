use bytes::{Buf, IntoBuf};
use futures::{Async, Future, Poll};
use h2;
use http;
use std::marker::PhantomData;
use std::sync::Arc;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::time::{Duration, Instant};
use tower::{NewService, Service};
use tower_h2::{client, Body};

use ctx;
use telemetry::event::{self, Event};

const GRPC_STATUS: &str = "grpc-status";

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
    request_open: Instant,
}

#[derive(Default, Debug)]
pub struct ResponseBody<B> {
    body: B,
    inner: Option<ResponseBodyInner>,
    _p: PhantomData<(B)>,
}

#[derive(Debug)]
struct ResponseBodyInner {
    handle: super::Handle,
    ctx: Arc<ctx::http::Response>,
    bytes_sent: u64,
    frames_sent: u32,
    request_open: Instant,
    response_open: Instant,
}

// === NewHttp ===

impl<N, A, B> NewHttp<N, A, B>
where
    A: Body + 'static,
    B: Body + 'static,
    N: NewService<Request = http::Request<A>, Response = http::Response<B>, Error = client::Error>
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
    N: NewService<Request = http::Request<A>, Response = http::Response<B>, Error = client::Error>
        + 'static,
{
    type Request = N::Request;
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
    F::Item: Service<Request = http::Request<A>, Response = http::Response<B>>,
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
    S: Service<Request = http::Request<A>, Response = http::Response<B>, Error = client::Error>
        + 'static,
{
    type Request = S::Request;
    type Response = http::Response<ResponseBody<B>>;
    type Error = S::Error;
    type Future = Respond<S::Future, B>;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        self.service.poll_ready()
    }

    fn call(&mut self, mut req: Self::Request) -> Self::Future {
        let inner = match req.extensions_mut().remove::<Arc<ctx::transport::Server>>() {
            None => None,
            Some(ctx) => {
                let id = self.next_id.fetch_add(1, Ordering::SeqCst);
                let ctx = ctx::http::Request::new(&req, &ctx, &self.client_ctx, id);

                self.handle
                    .send(|| Event::StreamRequestOpen(Arc::clone(&ctx)));

                Some(RespondInner {
                    ctx,
                    handle: self.handle.clone(),
                    request_open: Instant::now(),
                })
            }
        };

        // TODO measure request lifetime.
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
    F: Future<Item = http::Response<B>, Error = client::Error>,
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
                            response_open: Instant::now(),
                        })
                    }
                });

                let rsp = {
                    let (parts, body) = rsp.into_parts();
                    let body = ResponseBody {
                        body,
                        inner,
                        _p: PhantomData,
                    };
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

// === ResponseBody ===

impl<B> ResponseBody<B> {
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
                        let ResponseBodyInner {
                            ctx,
                            mut handle,
                            request_open,
                            response_open,
                            bytes_sent,
                            frames_sent,
                            ..
                        } = i;

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
                }

                Err(e)
            }
        }
    }
}

impl<B> Body for ResponseBody<B>
where
    B: Body + 'static,
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
                inner.frames_sent += 1;
                inner.bytes_sent += frame.remaining() as u64;
            }
            frame
        });
        Ok(Async::Ready(frame))
    }

    fn poll_trailers(&mut self) -> Poll<Option<http::HeaderMap>, h2::Error> {
        match self.sense_err(|b| b.poll_trailers()) {
            Err(e) => Err(e),
            Ok(Async::NotReady) => Ok(Async::NotReady),
            Ok(Async::Ready(trls)) => {
                if let Some(i) = self.inner.take() {
                    let ResponseBodyInner {
                        ctx,
                        mut handle,
                        request_open,
                        response_open,
                        bytes_sent,
                        frames_sent,
                    } = i;

                    handle.send(|| {
                        let grpc_status = trls.as_ref()
                            .and_then(|t| t.get(GRPC_STATUS))
                            .and_then(|v| v.to_str().ok())
                            .and_then(|s| s.parse::<u32>().ok());

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
                    })
                }

                Ok(Async::Ready(trls))
            }
        }
    }
}
