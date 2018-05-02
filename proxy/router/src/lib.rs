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

/// Route requests based on the request authority
pub struct Router<T>
where T: Recognize,
{
    inner: Arc<Mutex<Inner<T>>>,
}

/// Route a request based on an authority
pub trait Recognize {
    /// Requests handled by the discovered services
    type Request;

    /// Responses given by the discovered services
    type Response;

    /// Errors produced by the discovered services
    type Error;

    /// Key
    type Key: Clone + Eq + Hash;

    /// Error produced by failed routing
    type RouteError;

    /// The discovered `Service` instance.
    type Service: Service<Request = Self::Request,
                         Response = Self::Response,
                            Error = Self::Error>;

    /// Obtains a Key for a request.
    fn recognize(&self, req: &Self::Request) -> Option<Reuse<Self::Key>>;

    /// Return a `Service` to handle requests from the provided authority.
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

#[derive(Debug)]
pub enum Error<T, U> {
    Inner(T),
    Route(U),
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
}

enum State<T>
where T: Recognize,
{
    Inner(<T::Service as Service>::Future),
    RouteError(T::RouteError),
    NotRecognized,
    Invalid,
}

// ===== impl Router =====

impl<T> Router<T>
where T: Recognize
{
    pub fn new(recognize: T) -> Self {
        Router {
            inner: Arc::new(Mutex::new(Inner {
                routes: Default::default(),
                recognize,
            })),
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

    fn poll_ready(&mut self) -> Poll<(), Self::Error> {
        Ok(().into())
    }

    fn call(&mut self, request: Self::Request) -> Self::Future {
        let inner = &mut *self.inner.lock().expect("lock router cache");

        let key = match inner.recognize.recognize(&request) {
            Some(k) => k,
            None => {
                return ResponseFuture { state: State::NotRecognized };
            }
        };

        // Is the bound service for that key reusable? If `recognize`
        // returned `SingleUse`, that indicates that the service may
        // not be used to serve multiple requests.
        if let Reuse::Reusable(ref k) = key {
            if let Some(service) = inner.routes.get_mut(k) {
                let response = service.call(request);
                return ResponseFuture { state: State::Inner(response) };
            }
        };

        let mut service = match inner.recognize.bind_service(key.as_ref()) {
            Ok(s) => s,
            Err(e) => {
                return ResponseFuture { state: State::RouteError(e) };
            }
        };

        let response = service.call(request);
        if let Reuse::Reusable(k) = key {
            inner.routes.insert(k, service);
        }
        ResponseFuture { state: State::Inner(response) }
    }
}

impl<T> Clone for Router<T>
where T: Recognize,
{
    fn clone(&self) -> Self {
        Router { inner: self.inner.clone() }
    }
}

// ===== impl Recognize =====

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
            Invalid => panic!(),
        }
    }
}

// ===== impl RouteError =====

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
