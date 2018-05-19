extern crate futures;
extern crate indexmap;
extern crate tower_service;

use futures::{Future, Poll};
use tower_service::Service;

use std::{error, fmt, mem};
use std::hash::Hash;
use std::sync::{Arc, Mutex};
use std::time::Duration;

mod cache;

use self::cache::Cache;

/// Routes requests based on a configurable `Key`.
pub struct Router<T>
where T: Recognize,
{
    inner: Arc<Inner<T>>,
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
    fn recognize(&self, req: &Self::Request) -> Option<Self::Key>;

    /// Return a `Service` to handle requests.
    ///
    /// The returned service must always be in the ready state (i.e.
    /// `poll_ready` must always return `Ready` or `Err`).
    fn bind_service(&self, key: &Self::Key) -> Result<Self::Service, Self::RouteError>;
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
    recognize: T,
    cache: Mutex<Cache<T::Key, T::Service>>,
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
    pub fn new(recognize: T, capacity: usize, max_idle_age: Duration) -> Self {
        Router {
            inner: Arc::new(Inner {
                recognize,
                cache: Mutex::new(Cache::new(capacity, max_idle_age)),
            }),
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
    /// The response fails when the request cannot be routed.
    fn call(&mut self, request: Self::Request) -> Self::Future {
        let key = match self.inner.recognize.recognize(&request) {
            Some(key) => key,
            None => return ResponseFuture::not_recognized(),
        };

        let cache = &mut *self.inner.cache.lock().expect("lock router cache");

        // First, try to load a cached route for `key`.
        if let Some(mut service) = cache.access(&key) {
            return ResponseFuture::new(service.call(request));
        }

        // Since there wasn't a cached route, ensure that there is capacity for a
        // new one.
        let reserve = match cache.reserve() {
            Ok(r) => r,
            Err(cache::CapacityExhausted { capacity }) => {
                return ResponseFuture::no_capacity(capacity);
            }
        };

        // Bind a new route, send the request on the route, and cache the route.
        let mut service = match self.inner.recognize.bind_service(&key) {
            Ok(svc) => svc,
            Err(e) => return ResponseFuture { state: State::RouteError(e) },
        };

        let response = service.call(request);
        reserve.store(key, service);

        ResponseFuture::new(response)
    }
}

impl<T> Clone for Router<T>
where T: Recognize,
{
    fn clone(&self) -> Self {
        Router { inner: self.inner.clone() }
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

#[cfg(test)]
mod test_util {
    use futures::{Poll, future};
    use tower_service::Service;

    pub struct Recognize;

    #[derive(Debug)]
    pub struct MultiplyAndAssign(usize);

    #[derive(Debug)]
    pub enum Request {
        NotRecognized,
        Recognized(usize),
    }

    // ===== impl Recognize =====

    impl super::Recognize for Recognize {
        type Request = Request;
        type Response = usize;
        type Error = ();
        type Key = usize;
        type RouteError = ();
        type Service = MultiplyAndAssign;

        fn recognize(&self, req: &Self::Request) -> Option<Self::Key> {
            match *req {
                Request::NotRecognized => None,
                Request::Recognized(n) => Some(n),
            }
        }

        fn bind_service(&self, _: &Self::Key) -> Result<Self::Service, Self::RouteError> {
            Ok(MultiplyAndAssign(1))
        }
    }

    // ===== impl MultiplyAndAssign =====

    impl Default for MultiplyAndAssign {
        fn default() -> Self {
            MultiplyAndAssign(1)
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
                Request::Recognized(n) => n,
            };
            self.0 *= n;
            future::ok(self.0)
        }
    }

    impl From<usize> for Request {
        fn from(n: usize) -> Request {
            Request::Recognized(n)
        }
    }
}

#[cfg(test)]
mod tests {
    use futures::Future;
    use std::time::Duration;
    use test_util::*;
    use tower_service::Service;
    use super::{Error, Router};

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
        let mut router = Router::new(Recognize, 1, Duration::from_secs(0));

        let rsp = router.call_err(Request::NotRecognized);
        assert_eq!(rsp, Error::NotRecognized);
    }

    #[test]
    fn cache_limited_by_capacity() {
        let mut router = Router::new(Recognize, 1, Duration::from_secs(1));

        let rsp = router.call_ok(2.into());
        assert_eq!(rsp, 2);

        let rsp = router.call_err(3.into());
        assert_eq!(rsp, Error::NoCapacity(1));
    }

    #[test]
    fn services_cached() {
        let mut router = Router::new(Recognize, 1, Duration::from_secs(0));

        let rsp = router.call_ok(2.into());
        assert_eq!(rsp, 2);

        let rsp = router.call_ok(2.into());
        assert_eq!(rsp, 4);
    }
}
