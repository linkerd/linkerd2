use std::error::Error;
use std::fmt;
use std::default::Default;
use std::marker::PhantomData;
use std::sync::Arc;
use std::sync::atomic::AtomicUsize;

use futures::{Future, Poll, future};
use http::{self, uri};
use tokio_core::reactor::Handle;
use tower_service as tower;
use tower_h2;
use tower_reconnect::Reconnect;

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

/// A type of service binding
///
/// Some services, for various reasons, may not be able to be used to serve multiple
/// requests. The `BindsPerRequest` binding ensures that a new stack is bound for each
/// request.
///
/// `Bound` serivces may be used to process an arbitrary number of requests.
pub enum Binding<B: tower_h2::Body + 'static> {
    Bound(Stack<B>),
    BindsPerRequest {
        endpoint: Endpoint,
        protocol: Protocol,
        bind: Bind<Arc<ctx::Proxy>, B>,
        // When `poll_ready` is called, the _next_ service to be used may be bound
        // ahead-of-time. This stack is used only to serve the next request to this
        // service.
        next: Option<Stack<B>>
    },
}

/// Protocol portion of the `Recognize` key for a request.
///
/// This marks whether to use HTTP/2 or HTTP/1.x for a request. In
/// the case of HTTP/1.x requests, it also stores a "host" key to ensure
/// that each host receives its own connection.
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub enum Protocol {
    Http1 {
        host: Host,
        /// Whether or not the request URI was in absolute form.
        ///
        /// This is used to configure Hyper's behaviour at the connection
        /// level, so it's necessary that requests with and without
        /// absolute URIs be bound to separate service stacks. It is also
        /// used to determine what URI normalization will be necessary.
        was_absolute_form: bool,
    },
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
    inner: S,
    was_absolute_form: bool,
}

pub type Service<B> = Binding<B>;

pub type Stack<B> = Reconnect<NormalizeUri<NewHttp<B>>>;

pub type NewHttp<B> = sensor::NewHttp<Client<B>, B, HttpBody>;

pub type HttpResponse = http::Response<sensor::http::ResponseBody<HttpBody>>;

pub type HttpRequest<B> = http::Request<sensor::http::RequestBody<B>>;

pub type Client<B> = transparency::Client<sensor::Connect<transport::Connect>, B>;

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
    pub fn executor(&self) -> &Handle {
        &self.executor
    }
}

impl<B> Bind<Arc<ctx::Proxy>, B>
where
    B: tower_h2::Body + 'static,
{
    fn bind_stack(&self, ep: &Endpoint, protocol: &Protocol) -> Stack<B> {
        debug!("bind_stack endpoint={:?}, protocol={:?}", ep, protocol);
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
            self.executor.clone()
        );

        let sensors = self.sensors.http(
            self.req_ids.clone(),
            client,
            &client_ctx
        );

        // Rewrite the HTTP/1 URI, if the authorities in the Host header
        // and request URI are not in agreement, or are not present.
        let proxy = NormalizeUri::new(sensors, protocol.was_absolute_form());

        // Automatically perform reconnects if the connection fails.
        //
        // TODO: Add some sort of backoff logic.
        Reconnect::new(proxy)
    }

    pub fn new_binding(&self, ep: &Endpoint, protocol: &Protocol) -> Binding<B> {
        if protocol.can_reuse_clients() {
            Binding::Bound(self.bind_stack(ep, protocol))
        } else {
            Binding::BindsPerRequest {
                endpoint: ep.clone(),
                protocol: protocol.clone(),
                bind: self.clone(),
                next: None
            }
        }
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
        Ok(self.bind.new_binding(ep, &self.protocol))
    }
}


// ===== impl NormalizeUri =====


impl<S> NormalizeUri<S> {
    fn new(inner: S, was_absolute_form: bool) -> Self {
        Self { inner, was_absolute_form }
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
    type Future = future::Map<
        S::Future,
        fn(S::Service) -> NormalizeUri<S::Service>
    >;
    fn new_service(&self) -> Self::Future {
        let s = self.inner.new_service();
        // This weird dance is so that the closure doesn't have to
        // capture `self` and can just be a `fn` (so the `Map`)
        // can be returned unboxed.
        if self.was_absolute_form {
            s.map(|inner| NormalizeUri::new(inner, true))
        } else {
            s.map(|inner| NormalizeUri::new(inner, false))
        }
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
            h1::normalize_our_view_of_uri(&mut request, self.was_absolute_form);
        }
        self.inner.call(request)
    }
}
// ===== impl Binding =====

impl<B: tower_h2::Body + 'static> tower::Service for Binding<B> {
    type Request = <Stack<B> as tower::Service>::Request;
    type Response = <Stack<B> as tower::Service>::Response;
    type Error = <Stack<B> as tower::Service>::Error;
    type Future = <Stack<B> as tower::Service>::Future;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        match *self {
            // A service is already bound, so poll its readiness.
            Binding::Bound(ref mut svc) |
            Binding::BindsPerRequest { next: Some(ref mut svc), .. } => svc.poll_ready(),

            // If no stack has been bound, bind it now so that its readiness can be
            // checked. Store it so it can be consumed to dispatch the next request.
            Binding::BindsPerRequest { ref endpoint, ref protocol, ref bind, ref mut next } => {
                let mut svc = bind.bind_stack(endpoint, protocol);
                let ready = svc.poll_ready()?;
                *next = Some(svc);
                Ok(ready)
            }
        }
    }

    fn call(&mut self, request: Self::Request) -> Self::Future {
        match *self {
            Binding::Bound(ref mut svc) => svc.call(request),
            Binding::BindsPerRequest { ref endpoint, ref protocol, ref bind, ref mut next } => {
                // If a service has already been bound in `poll_ready`, consume it.
                // Otherwise, bind a new service on-the-spot.
                let mut svc = next.take().unwrap_or_else(|| bind.bind_stack(endpoint, protocol));
                svc.call(request)
            }
        }
    }
}

// ===== impl Protocol =====


impl Protocol {

    pub fn detect<B>(req: &http::Request<B>) -> Self {
        if req.version() == http::Version::HTTP_2 {
            return Protocol::Http2
        }

        let authority_part = req.uri().authority_part();
        let was_absolute_form = authority_part.is_some();
        trace!(
            "Protocol::detect(); req.uri='{:?}'; was_absolute_form={:?};",
            req.uri(), was_absolute_form
        );
        // If the request has an authority part, use that as the host part of
        // the key for an HTTP/1.x request.
        let host = authority_part
            .cloned()
            .or_else(|| h1::authority_from_host(req))
            .map(Host::Authority)
            .unwrap_or_else(|| Host::NoAuthority);


        Protocol::Http1 { host, was_absolute_form }
    }

    /// Returns true if the request was originally received in absolute form.
    pub fn was_absolute_form(&self) -> bool {
        match self {
            &Protocol::Http1 { was_absolute_form, .. } => was_absolute_form,
            _ => false,
        }
    }

    pub fn can_reuse_clients(&self) -> bool {
        match *self {
            Protocol::Http2 | Protocol::Http1 { host: Host::Authority(_), .. } => true,
            _ => false,
        }
    }
}
