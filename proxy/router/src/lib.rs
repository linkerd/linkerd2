extern crate futures;
extern crate indexmap;
extern crate tower_service;

use futures::{Future, Poll};
use indexmap::IndexMap;
use tower_service::Service;

use std::{error, fmt, mem};
use std::convert::AsRef;
use std::hash::Hash;
use std::sync::{Arc, Mutex};

/// Routes requests based on a configurable `Key`.
pub struct Router<T>
where T: Recognize,
{
    inner: Arc<Mutex<Inner<T>>>,
}

/// Provides a strategy for routing a Request to a Service.
///
/// Implementors must provide a `Key` type that identifies each unique route. The
/// `recognize()` method is used to determine the key for a given request. This key is
/// used to look up a route in a cache (i.e. in `Router`), or can be passed to
/// `bind_service` to instantiate the identified route.
pub trait Recognize {
    /// Requests handled by the discovered services
    type Request;

    /// Responses given by the discovered services
    type Response;

    /// Errors produced by the discovered services
    type Error;

    /// Identifies a Route.
    type Key: Clone + Eq + Hash;

    /// Error produced by failed routing
    type RouteError;

    /// A route.
    type Service: Service<Request = Self::Request,
                         Response = Self::Response,
                            Error = Self::Error>;

    /// Determines the key for a route to handle the given request.
    fn recognize(&self, req: &Self::Request) -> Option<Reuse<Self::Key>>;

    /// Return a `Service` to handle requests.
    ///
    /// The returned service must always be in the ready state (i.e.
    /// `poll_ready` must always return `Ready` or `Err`).
    fn bind_service(&mut self, key: &Self::Key) -> Result<Self::Service, Self::RouteError>;
}

pub struct Single<S>(Option<S>);

/// Whether or not the service to a given key may be cached.
///
/// Some services may, for various reasons, may not be able to
/// be used to serve multiple requests. When this is the case,
/// implementors of `recognize` may use `Reuse::SingleUse` to
/// indicate that the service should not be cached.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Reuse<T> {
    Reusable(T),
    SingleUse(T),
}

#[derive(Debug, PartialEq)]
pub enum Error<T, U> {
    Inner(T),
    Route(U),
    NoCapacity(usize),
    NotRecognized,
}

pub struct ResponseFuture<T>
where T: Recognize,
{
    state: State<T>,
}

struct Inner<T>
where T: Recognize,
{
    routes: IndexMap<T::Key, T::Service>,
    recognize: T,
    capacity: usize,
}

enum State<T>
where T: Recognize,
{
    Inner(<T::Service as Service>::Future),
    RouteError(T::RouteError),
    NoCapacity(usize),
    NotRecognized,
    Invalid,
}

// ===== impl Router =====

impl<T> Router<T>
where T: Recognize
{
    pub fn new(recognize: T, capacity: usize) -> Self {
        Router {
            inner: Arc::new(Mutex::new(Inner {
                routes: IndexMap::default(),
                recognize,
                capacity,
            })),
        }
    }
}

macro_rules! try_bind_route {
    ( $bind:expr ) => {
        match $bind {
            Ok(svc) => svc,
            Err(e) => return ResponseFuture { state: State::RouteError(e) },
        }
    }
}

impl<T> Service for Router<T>
where T: Recognize,
{
    type Request = T::Request;
    type Response = T::Response;
    type Error = Error<T::Error, T::RouteError>;
    type Future = ResponseFuture<T>;

    /// Always ready to serve.
    ///
    /// Graceful backpressure is **not** supported at this level, since each request may
    /// be routed to different resources. Instead, requests should be issued and each
    /// route should support a queue of requests.
    ///
    /// TODO Attempt to free capacity in the router.
    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        Ok(().into())
    }

    /// Routes the request through an underlying service.
    ///
    /// The response fails if the request cannot be routed.
    fn call(&mut self, request: Self::Request) -> Self::Future {
        let inner = &mut *self.inner.lock().expect("lock router cache");

        match inner.recognize.recognize(&request) {
            None => ResponseFuture::not_recognized(),

            Some(Reuse::SingleUse(key)) => {
                // TODO Keep SingleUse services in the cache as well, so that their
                // capacity is considered. To do this, we should move the Reuse logic into
                // the returned service (and not the key).
                let mut service = try_bind_route!(inner.recognize.bind_service(&key));
                ResponseFuture::new(service.call(request))
            }

            Some(Reuse::Reusable(key)) => {
                // First, try to load a cached route for `key`.
                if let Some(service) = inner.routes.get_mut(&key) {
                    return ResponseFuture::new(service.call(request));
                }

                // Since there wasn't a cached route, ensure that there is capacity for a
                // new one.
                if inner.routes.len() == inner.capacity {
                    // TODO If the cache is full, evict the oldest inactive route. If all
                    // routes are active, fail the request.
                    return ResponseFuture::no_capacity(inner.capacity);
                }

                // Bind a new route, send the request on the route, and cache the route.
                let mut service = try_bind_route!(inner.recognize.bind_service(&key));
                let response = service.call(request);
                inner.routes.insert(key, service);
                ResponseFuture::new(response)
            }
        }
    }
}

impl<T> Clone for Router<T>
where T: Recognize,
{
    fn clone(&self) -> Self {
        Router { inner: self.inner.clone() }
    }
}

// ===== impl Single =====

impl<S: Service> Single<S> {
    pub fn new(svc: S) -> Self {
        Single(Some(svc))
    }
}

impl<S: Service> Recognize for Single<S> {
    type Request = S::Request;
    type Response = S::Response;
    type Error = S::Error;
    type Key = ();
    type RouteError = ();
    type Service = S;

    fn recognize(&self, _: &Self::Request) -> Option<Reuse<Self::Key>> {
        Some(Reuse::Reusable(()))
    }

    fn bind_service(&mut self, _: &Self::Key) -> Result<S, Self::RouteError> {
        Ok(self.0.take().expect("static route bound twice"))
    }
}

// ===== impl ResponseFuture =====

impl<T> ResponseFuture<T>
where T: Recognize,
{
    fn new(inner: <T::Service as Service>::Future) -> Self {
        ResponseFuture { state: State::Inner(inner) }
    }

    fn not_recognized() -> Self {
        ResponseFuture { state: State::NotRecognized }
    }

    fn no_capacity(capacity: usize) -> Self {
        ResponseFuture { state: State::NoCapacity(capacity) }
    }
}

impl<T> Future for ResponseFuture<T>
where T: Recognize,
{
    type Item = T::Response;
    type Error = Error<T::Error, T::RouteError>;

    fn poll(&mut self) -> Poll<Self::Item, Self::Error> {
        use self::State::*;

        match self.state {
            Inner(ref mut fut) => fut.poll().map_err(Error::Inner),
            RouteError(..) => {
                match mem::replace(&mut self.state, Invalid) {
                    RouteError(e) => Err(Error::Route(e)),
                    _ => unreachable!(),
                }
            }
            NotRecognized => Err(Error::NotRecognized),
            NoCapacity(capacity) => Err(Error::NoCapacity(capacity)),
            Invalid => panic!(),
        }
    }
}

// ===== impl Error =====

impl<T, U> fmt::Display for Error<T, U>
where
    T: fmt::Display,
    U: fmt::Display,
{
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match *self {
            Error::Inner(ref why) => fmt::Display::fmt(why, f),
            Error::Route(ref why) =>
                write!(f, "route recognition failed: {}", why),
            Error::NotRecognized => f.pad("route not recognized"),
            Error::NoCapacity(capacity) => write!(f, "router capacity reached ({})", capacity),
        }
    }
}

impl<T, U> error::Error for Error<T, U>
where
    T: error::Error,
    U: error::Error,
{
    fn cause(&self) -> Option<&error::Error> {
        match *self {
            Error::Inner(ref why) => Some(why),
            Error::Route(ref why) => Some(why),
            _ => None,
        }
    }

    fn description(&self) -> &str {
        match *self {
            Error::Inner(_) => "inner service error",
            Error::Route(_) => "route recognition failed",
            Error::NoCapacity(_) => "router capacity reached",
            Error::NotRecognized => "route not recognized",
        }
    }
}

// ===== impl Reuse =====

impl<T> AsRef<T> for Reuse<T> {
    fn as_ref(&self) -> &T {
        match *self {
            Reuse::Reusable(ref key) => key,
            Reuse::SingleUse(ref key) => key,
        }
    }
}

#[cfg(test)]
mod tests {
    use futures::{Poll, Future, future};
    use tower_service::Service;
    use super::{Error, Reuse, Router};

    struct Recognize;

    struct MultiplyAndAssign(usize);

    enum Request {
        NotRecognized,
        Reusable(usize),
        SingleUse(usize),
    }

    impl super::Recognize for Recognize {
        type Request = Request;
        type Response = usize;
        type Error = ();
        type Key = usize;
        type RouteError = ();
        type Service = MultiplyAndAssign;

        fn recognize(&self, req: &Self::Request) -> Option<Reuse<Self::Key>> {
            match *req {
                Request::NotRecognized => None,
                Request::Reusable(n) => Some(Reuse::Reusable(n)),
                Request::SingleUse(n) => Some(Reuse::SingleUse(n)),
            }
        }

        fn bind_service(&mut self, _: &Self::Key) -> Result<Self::Service, Self::RouteError> {
            Ok(MultiplyAndAssign(1))
        }
    }

    impl Service for MultiplyAndAssign {
        type Request = Request;
        type Response = usize;
        type Error = ();
        type Future = future::FutureResult<usize, ()>;

        fn poll_ready(&mut self) -> Poll<(), ()> {
            unimplemented!()
        }

        fn call(&mut self, req: Self::Request) -> Self::Future {
            let n = match req {
                Request::NotRecognized => unreachable!(),
                Request::Reusable(n) => n,
                Request::SingleUse(n) => n,
            };
            self.0 *= n;
            future::ok(self.0)
        }
    }

    impl Router<Recognize> {
        fn call_ok(&mut self, req: Request) -> usize {
            self.call(req).wait().expect("should route")
        }

        fn call_err(&mut self, req: Request) -> super::Error<(), ()> {
            self.call(req).wait().expect_err("should not route")
        }
    }

    #[test]
    fn invalid() {
        let mut router = Router::new(Recognize, 1);

        let rsp = router.call_err(Request::NotRecognized);
        assert_eq!(rsp, Error::NotRecognized);
    }

    #[test]
    fn reuse_limited_by_capacity() {
        let mut router = Router::new(Recognize, 1);

        let rsp = router.call_ok(Request::Reusable(2));
        assert_eq!(rsp, 2);

        let rsp = router.call_err(Request::Reusable(3));
        assert_eq!(rsp, Error::NoCapacity(1));
    }

    #[test]
    fn reuse_shares_service() {
        let mut router = Router::new(Recognize, 1);

        let rsp = router.call_ok(Request::Reusable(2));
        assert_eq!(rsp, 2);

        let rsp = router.call_ok(Request::Reusable(2));
        assert_eq!(rsp, 4);
    }

    #[test]
    fn single_use_does_not_share_service() {
        let mut router = Router::new(Recognize, 1);

        let rsp = router.call_ok(Request::SingleUse(2));
        assert_eq!(rsp, 2);

        let rsp = router.call_ok(Request::SingleUse(2));
        assert_eq!(rsp, 2);
    }

    #[test]
    fn single_use_not_cached_or_limited_by_capacity() {
        let mut router = Router::new(Recognize, 1);

        let rsp = router.call_ok(Request::Reusable(2));
        assert_eq!(rsp, 2);

        let rsp = router.call_ok(Request::SingleUse(2));
        assert_eq!(rsp, 2);
    }
}
