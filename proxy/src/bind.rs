use std::error::Error;
use std::fmt;
use std::default::Default;
use std::marker::PhantomData;
use std::sync::Arc;
use std::sync::atomic::AtomicUsize;

use futures::{Future, Poll};
use futures::future::Map;
use http::{self, uri};
use tokio_core::reactor::Handle;
use tower;
use tower_h2;
use tower_reconnect::Reconnect;

use conduit_proxy_router::Reuse;

use control;
use control::discovery::Endpoint;
use ctx;
use telemetry::{self, sensor};
use transparency::{self, HttpBody, h1};
use transport;

/// Binds a `Service` from a `SocketAddr`.
///
/// The returned `Service` buffers request until a connection is established.
///
/// # TODO
///
/// Buffering is not bounded and no timeouts are applied.
pub struct Bind<C, B> {
    ctx: C,
    sensors: telemetry::Sensors,
    executor: Handle,
    req_ids: Arc<AtomicUsize>,
    _p: PhantomData<B>,
}

/// Binds a `Service` from a `SocketAddr` for a pre-determined protocol.
pub struct BindProtocol<C, B> {
    bind: Bind<C, B>,
    protocol: Protocol,
}

/// Protocol portion of the `Recognize` key for a request.
///
/// This marks whether to use HTTP/2 or HTTP/1.x for a request. In
/// the case of HTTP/1.x requests, it also stores a "host" key to ensure
/// that each host receives its own connection.
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub enum Protocol {
    Http1(Host),
    Http2
}

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub enum Host {
    Authority(uri::Authority),
    NoAuthority,
}

/// Rewrites HTTP/1.x requests so that their URIs are in a canonical form.
///
/// The following transformations are applied:
/// - If an absolute-form URI is received, it must replace
///   the host header (in accordance with RFC7230#section-5.4)
/// - If the request URI is not in absolute form, it is rewritten to contain
///   the authority given in the `Host:` header, or, failing that, from the
///   request's original destination according to `SO_ORIGINAL_DST`.
#[derive(Copy, Clone, Debug)]
pub struct NormalizeUri<S> {
    inner: S
}

pub type Service<B> = Reconnect<NormalizeUri<NewHttp<B>>>;

pub type NewHttp<B> = sensor::NewHttp<Client<B>, B, HttpBody>;

pub type HttpResponse = http::Response<sensor::http::ResponseBody<HttpBody>>;

pub type HttpRequest<B> = http::Request<sensor::http::RequestBody<B>>;

pub type Client<B> = transparency::Client<
    sensor::Connect<transport::Connect>,
    B,
>;

#[derive(Copy, Clone, Debug)]
pub enum BufferSpawnError {
    Inbound,
    Outbound,
}

impl fmt::Display for BufferSpawnError {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        f.pad(self.description())
    }
}

impl Error for BufferSpawnError {

    fn description(&self) -> &str {
        match *self {
            BufferSpawnError::Inbound =>
                "error spawning inbound buffer task",
            BufferSpawnError::Outbound =>
                "error spawning outbound buffer task",
        }
    }

    fn cause(&self) -> Option<&Error> { None }
}

impl<B> Bind<(), B> {
    pub fn new(executor: Handle) -> Self {
        Self {
            executor,
            ctx: (),
            sensors: telemetry::Sensors::null(),
            req_ids: Default::default(),
            _p: PhantomData,
        }
    }

    pub fn with_sensors(self, sensors: telemetry::Sensors) -> Self {
        Self {
            sensors,
            ..self
        }
    }

    pub fn with_ctx<C>(self, ctx: C) -> Bind<C, B> {
        Bind {
            ctx,
            sensors: self.sensors,
            executor: self.executor,
            req_ids: self.req_ids,
            _p: PhantomData,
        }
    }
}

impl<C: Clone, B> Clone for Bind<C, B> {
    fn clone(&self) -> Self {
        Self {
            ctx: self.ctx.clone(),
            sensors: self.sensors.clone(),
            executor: self.executor.clone(),
            req_ids: self.req_ids.clone(),
            _p: PhantomData,
        }
    }
}


impl<C, B> Bind<C, B> {

    // pub fn ctx(&self) -> &C {
    //     &self.ctx
    // }

    pub fn executor(&self) -> &Handle {
        &self.executor
    }

    // pub fn req_ids(&self) -> &Arc<AtomicUsize> {
    //     &self.req_ids
    // }

    // pub fn sensors(&self) -> &telemetry::Sensors {
    //     &self.sensors
    // }

}

impl<B> Bind<Arc<ctx::Proxy>, B>
where
    B: tower_h2::Body + 'static,
{
    pub fn bind_service(&self, ep: &Endpoint, protocol: &Protocol) -> Service<B> {
        trace!("bind_service endpoint={:?}, protocol={:?}", ep, protocol);
        let addr = ep.address();
        let client_ctx = ctx::transport::Client::new(
            &self.ctx,
            &addr,
            ep.dst_labels().cloned(),
        );

        // Map a socket address to a connection.
        let connect = self.sensors.connect(
            transport::Connect::new(addr, &self.executor),
            &client_ctx,
        );

        let client = transparency::Client::new(
            protocol,
            connect,
            self.executor.clone(),
        );

        let sensors = self.sensors.http(
            self.req_ids.clone(),
            client,
            &client_ctx
        );

        // Rewrite the HTTP/1 URI, if the authorities in the Host header
        // and request URI are not in agreement, or are not present.
        let proxy = NormalizeUri::new(sensors);

        // Automatically perform reconnects if the connection fails.
        //
        // TODO: Add some sort of backoff logic.
        Reconnect::new(proxy)
    }
}

// ===== impl BindProtocol =====


impl<C, B> Bind<C, B> {
    pub fn with_protocol(self, protocol: Protocol) -> BindProtocol<C, B> {
        BindProtocol {
            bind: self,
            protocol,
        }
    }
}

impl<B> control::discovery::Bind for BindProtocol<Arc<ctx::Proxy>, B>
where
    B: tower_h2::Body + 'static,
{
    type Endpoint = Endpoint;
    type Request = http::Request<B>;
    type Response = HttpResponse;
    type Error = <Service<B> as tower::Service>::Error;
    type Service = Service<B>;
    type BindError = ();

    fn bind(&self, ep: &Endpoint) -> Result<Self::Service, Self::BindError> {
        Ok(self.bind.bind_service(ep, &self.protocol))
    }
}


// ===== impl NormalizeUri =====


impl<S> NormalizeUri<S> {
    fn new (inner: S) -> Self {
        Self { inner }
    }
}

impl<S, B> tower::NewService for NormalizeUri<S>
where
    S: tower::NewService<
        Request=http::Request<B>,
        Response=HttpResponse,
    >,
    S::Service: tower::Service<
        Request=http::Request<B>,
        Response=HttpResponse,
    >,
    NormalizeUri<S::Service>: tower::Service,
    B: tower_h2::Body,
{
    type Request = <Self::Service as tower::Service>::Request;
    type Response = <Self::Service as tower::Service>::Response;
    type Error = <Self::Service as tower::Service>::Error;
    type Service = NormalizeUri<S::Service>;
    type InitError = S::InitError;
    type Future = Map<
        S::Future,
        fn(S::Service) -> NormalizeUri<S::Service>
    >;
    fn new_service(&self) -> Self::Future {
        self.inner.new_service().map(NormalizeUri::new)
    }
}

impl<S, B> tower::Service for NormalizeUri<S>
where
    S: tower::Service<
        Request=http::Request<B>,
        Response=HttpResponse,
    >,
    B: tower_h2::Body,
{
    type Request = S::Request;
    type Response = HttpResponse;
    type Error = S::Error;
    type Future = S::Future;

    fn poll_ready(&mut self) -> Poll<(), S::Error> {
        self.inner.poll_ready()
    }

    fn call(&mut self, mut request: S::Request) -> Self::Future {
        if request.version() != http::Version::HTTP_2 {
            h1::normalize_our_view_of_uri(&mut request);
        }
        self.inner.call(request)
    }
}

// ===== impl Protocol =====


impl Protocol {

    pub fn detect<B>(req: &http::Request<B>) -> Self {
        if req.version() == http::Version::HTTP_2 {
            return Protocol::Http2
        }

        // If the request has an authority part, use that as the host part of
        // the key for an HTTP/1.x request.
        let host = req.uri().authority_part()
            .cloned()
            .or_else(|| h1::authority_from_host(req))
            .map(Host::Authority)
            .unwrap_or_else(|| Host::NoAuthority);

        Protocol::Http1(host)
    }

    pub fn is_cachable(&self) -> bool {
        match *self {
            Protocol::Http2 | Protocol::Http1(Host::Authority(_)) => true,
            _ => false,
        }
    }

    pub fn into_key<T>(self, key: T) -> Reuse<(T, Protocol)> {
        if self.is_cachable() {
            Reuse::Reusable((key, self))
        } else {
            Reuse::SingleUse((key, self))
        }
    }
}
