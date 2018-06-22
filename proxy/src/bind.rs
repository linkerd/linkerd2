use std::error::Error;
use std::fmt;
use std::marker::PhantomData;
use std::net::SocketAddr;
use std::sync::Arc;

use futures::{Async, Future, Poll, future, task};
use http::{self, uri};
use tower_service as tower;
use tower_h2;
use tower_reconnect::{Reconnect, Error as ReconnectError};

use control;
use control::destination::Endpoint;
use ctx;
use telemetry::{self, sensor};
use transparency::{self, HttpBody, h1};
use transport;
use tls;
use ctx::transport::TlsStatus;
use conditional::Conditional;
use watch_service::{WatchService, Rebind};

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
    _p: PhantomData<fn() -> B>,
}

/// Binds a `Service` from a `SocketAddr` for a pre-determined protocol.
pub struct BindProtocol<C, B> {
    bind: Bind<C, B>,
    protocol: Protocol,
}

/// A bound service that can re-bind itself on demand.
///
/// Reasons this would need to re-bind:
///
/// - `BindsPerRequest` can only service 1 request, and then needs to bind a
///   new service.
/// - If there is an error in the inner service (such as a connect error), we
///   need to throw it away and bind a new service.
pub struct BoundService<B>
where
    B: tower_h2::Body + Send + 'static,
    <B::Data as ::bytes::IntoBuf>::Buf: Send,
{
    bind: Bind<Arc<ctx::Proxy>, B>,
    binding: Binding<B>,
    /// Prevents logging repeated connect errors.
    ///
    /// Set back to false after a connect succeeds, to log about future errors.
    debounce_connect_error_log: bool,
    endpoint: Endpoint,
    protocol: Protocol,
}

// `Bind` cannot use `ConditionalConnectionConfig` since it uses a
// `tls::Identity` and a `tls::ClientConfig` obtained from different sources.
pub type ConditionalTlsClientConfig = Conditional<tls::ClientConfig, tls::ReasonForNoTls>;

/// A type of service binding.
///
/// Some services, for various reasons, may not be able to be used to serve multiple
/// requests. The `BindsPerRequest` binding ensures that a new stack is bound for each
/// request.
///
/// `Bound` serivces may be used to process an arbitrary number of requests.
pub enum Binding<B>
where
    B: tower_h2::Body + Send + 'static,
    <B::Data as ::bytes::IntoBuf>::Buf: Send,
{
    Bound(WatchService<ConditionalTlsClientConfig, RebindTls<B>>),
    BindsPerRequest {
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

pub struct RebindTls<B> {
    bind: Bind<Arc<ctx::Proxy>, B>,
    protocol: Protocol,
    endpoint: Endpoint,
}

pub type Service<B> = BoundService<B>;

pub type Stack<B> = WatchService<ConditionalTlsClientConfig, RebindTls<B>>;

type StackInner<B> = Reconnect<NormalizeUri<NewHttp<B>>>;

pub type NewHttp<B> = sensor::NewHttp<Client<B>, B, HttpBody>;

pub type HttpResponse = http::Response<sensor::http::ResponseBody<HttpBody>>;

pub type HttpRequest<B> = http::Request<sensor::http::RequestBody<B>>;

pub type Client<B> = transparency::Client<
    sensor::Connect<transport::Connect>,
    ::logging::ClientExecutor<&'static str, SocketAddr>,
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
    pub fn new() -> Self {
        Self {
            ctx: (),
            sensors: telemetry::Sensors::null(),
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
            _p: PhantomData,
        }
    }
}

impl<C: Clone, B> Clone for Bind<C, B> {
    fn clone(&self) -> Self {
        Self {
            ctx: self.ctx.clone(),
            sensors: self.sensors.clone(),
            _p: PhantomData,
        }
    }
}

impl<B> Bind<Arc<ctx::Proxy>, B>
where
    B: tower_h2::Body + Send + 'static,
    <B::Data as ::bytes::IntoBuf>::Buf: Send,
{
    /// Binds the "inner" layers of the stack.
    ///
    /// This binds a service stack that comprises the client for that individual
    /// endpoint. It will have to be rebuilt if the TLS configuration changes.
    ///
    /// This includes:
    /// + Reconnects
    /// + URI normalization
    /// + HTTP sensors
    ///
    /// When the TLS client configuration is invalidated, this function will
    /// be called again to bind a new stack.
    fn bind_inner_stack(
        &self,
        ep: &Endpoint,
        protocol: &Protocol,
        tls_client_config: &ConditionalTlsClientConfig,
    )-> StackInner<B> {
        debug!("bind_inner_stack endpoint={:?}, protocol={:?}", ep, protocol);
        let addr = ep.address();

        // Like `tls::current_connection_config()`.
        let tls = ep.tls_identity().and_then(|identity| {
            tls_client_config.as_ref().map(|config| {
                tls::ConnectionConfig {
                    identity: identity.clone(),
                    config: config.clone(),
                }
            })
        });

        let client_ctx = ctx::transport::Client::new(
            &self.ctx,
            &addr,
            ep.metadata().clone(),
            TlsStatus::from(&tls),
        );

        // Map a socket address to a connection.
        let connect = self.sensors.connect(
            transport::Connect::new(addr, tls),
            &client_ctx,
        );

        let log = ::logging::Client::proxy(&self.ctx, addr)
            .with_protocol(protocol.clone());
        let client = transparency::Client::new(
            protocol,
            connect,
            log.executor(),
        );

        let sensors = self.sensors.http(
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

    /// Binds the endpoint stack used to construct a bound service.
    ///
    /// This will wrap the service stack returned by `bind_inner_stack`
    /// with a middleware layer that causes it to be re-constructed when
    /// the TLS client configuration changes.
    ///
    /// This function will itself be called again by `BoundService` if the
    /// service binds per request, or if the initial connection to the
    /// endpoint fails.
    fn bind_stack(&self, ep: &Endpoint, protocol: &Protocol) -> Stack<B> {
        debug!("bind_stack: endpoint={:?}, protocol={:?}", ep, protocol);
        // TODO: Since `BindsPerRequest` bindings are only used for a
        // single request, it seems somewhat unnecessary to wrap them in a
        // `WatchService` middleware so that they can be rebound when the TLS
        // config changes, since they _always_ get rebound regardless. For now,
        // we still add the `WatchService` layer so that the per-request and
        // bound service stacks have the same type.
        let rebind = RebindTls {
            bind: self.clone(),
            endpoint: ep.clone(),
            protocol: protocol.clone(),
        };
        // TODO: the watch should be an explicit field of `Bind`, rather
        // than passed in the context.
        let tls_client_config = self.ctx.tls_client_config_watch().clone();
        WatchService::new(tls_client_config, rebind)
    }

    pub fn bind_service(&self, ep: &Endpoint, protocol: &Protocol) -> BoundService<B> {
        let binding = if protocol.can_reuse_clients() {
            Binding::Bound(self.bind_stack(ep, protocol))
        } else {
            Binding::BindsPerRequest {
                next: None
            }
        };

        BoundService {
            bind: self.clone(),
            binding,
            debounce_connect_error_log: false,
            endpoint: ep.clone(),
            protocol: protocol.clone(),
        }
    }
}

// ===== impl BindProtocol =====


impl<C, B> Bind<C, B> {
    pub fn with_protocol(self, protocol: Protocol)
        -> BindProtocol<C, B>
    {
        BindProtocol {
            bind: self,
            protocol,
        }
    }
}

impl<B> control::destination::Bind for BindProtocol<Arc<ctx::Proxy>, B>
where
    B: tower_h2::Body + Send + 'static,
    <B::Data as ::bytes::IntoBuf>::Buf: Send,
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
        if request.version() != http::Version::HTTP_2 &&
            // Skip normalizing the URI if it was received in
            // absolute form.
            !self.was_absolute_form
        {
            h1::normalize_our_view_of_uri(&mut request);
        }
        self.inner.call(request)
    }
}
// ===== impl Binding =====

impl<B> tower::Service for BoundService<B>
where
    B: tower_h2::Body + Send + 'static,
    <B::Data as ::bytes::IntoBuf>::Buf: Send,
{
    type Request = <Stack<B> as tower::Service>::Request;
    type Response = <Stack<B> as tower::Service>::Response;
    type Error = <Stack<B> as tower::Service>::Error;
    type Future = <Stack<B> as tower::Service>::Future;

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        let ready = match self.binding {
            // A service is already bound, so poll its readiness.
            Binding::Bound(ref mut svc) |
            Binding::BindsPerRequest { next: Some(ref mut svc) } => {
                trace!("poll_ready: stack already bound");
                svc.poll_ready()
            }

            // If no stack has been bound, bind it now so that its readiness can be
            // checked. Store it so it can be consumed to dispatch the next request.
            Binding::BindsPerRequest { ref mut next } => {
                trace!("poll_ready: binding stack");
                let mut svc = self.bind.bind_stack(&self.endpoint, &self.protocol);
                let ready = svc.poll_ready();
                *next = Some(svc);
                ready
            }
        };

        // If there was a connect error, don't terminate this BoundService
        // completely. Instead, simply clean up the inner service, prepare to
        // make a new one, and tell our caller that we could maybe be ready
        // if they call `poll_ready` again.
        //
        // If they *don't* call `poll_ready` again, that's ok, we won't ever
        // try to connect again.
        match ready {
            Err(ReconnectError::Connect(err)) => {
                if !self.debounce_connect_error_log {
                    self.debounce_connect_error_log = true;
                    warn!("connect error to {:?}: {}", self.endpoint, err);
                } else {
                    debug!("connect error to {:?}: {}", self.endpoint, err);
                }
                match self.binding {
                    Binding::Bound(ref mut svc) => {
                        trace!("poll_ready: binding stack after error");
                        *svc = self.bind.bind_stack(&self.endpoint, &self.protocol);
                    },
                    Binding::BindsPerRequest { ref mut next } => {
                        trace!("poll_ready: dropping bound stack after error");
                        next.take();
                    }
                }

                // So, this service isn't "ready" yet. Instead of trying to make
                // it ready, schedule the task for notification so the caller can
                // determine whether readiness is still necessary (i.e. whether
                // there are still requests to be sent).
                //
                // But, to return NotReady, we must notify the task. So,
                // this notifies the task immediately, and figures that
                // whoever owns this service will call `poll_ready` if they
                // are still interested.
                task::current().notify();
                Ok(Async::NotReady)
            }
            // don't debounce on NotReady...
            Ok(Async::NotReady) => Ok(Async::NotReady),
            other => {
                trace!("poll_ready: ready for business");
                self.debounce_connect_error_log = false;
                other
            },
        }
    }

    fn call(&mut self, request: Self::Request) -> Self::Future {
        match self.binding {
            Binding::Bound(ref mut svc) => svc.call(request),
            Binding::BindsPerRequest { ref mut next } => {
                // If a service has already been bound in `poll_ready`, consume it.
                // Otherwise, bind a new service on-the-spot.
                let bind = &self.bind;
                let endpoint = &self.endpoint;
                let protocol = &self.protocol;
                let mut svc = next.take()
                    .unwrap_or_else(|| {
                        bind.bind_stack(endpoint, protocol)
                    });
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

// ===== impl RebindTls =====

impl<B> Rebind<ConditionalTlsClientConfig> for RebindTls<B>
where
    B: tower_h2::Body + Send + 'static,
    <B::Data as ::bytes::IntoBuf>::Buf: Send,
{
    type Service = StackInner<B>;
    fn rebind(&mut self, tls: &ConditionalTlsClientConfig) -> Self::Service {
        debug!(
            "rebinding endpoint stack for {:?}:{:?} on TLS config change",
            self.endpoint, self.protocol,
        );
        self.bind.bind_inner_stack(&self.endpoint, &self.protocol, tls)
    }
}
